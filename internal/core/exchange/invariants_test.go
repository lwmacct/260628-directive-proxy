package exchange

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/lifecycle"
	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
	"github.com/lwmacct/260628-directive-proxy/internal/core/program"
)

func TestExchangeRejectsAttemptAfterTerminalRoundTrip(t *testing.T) {
	manager := NewManager(ManagerOptions{MaxAttempts: 3}, nil)
	current := manager.Start(httptest.NewRequest(http.MethodGet, "http://proxy.local/", nil))
	prepareInlineExchange(t, current)
	attempt, err := current.BeginAttempt(func() {})
	if err != nil {
		t.Fatal(err)
	}
	attempt.FinishRoundTrip(false, errors.New("upstream failed"))
	if _, err := current.BeginAttempt(func() {}); !errors.Is(err, context.Canceled) {
		t.Fatalf("terminal exchange accepted another attempt: %v", err)
	}
	current.Complete()
}

func TestAttemptScopeCanOnlyBeOpenedOnce(t *testing.T) {
	manager := NewManager(ManagerOptions{MaxAttempts: 2}, nil)
	current := manager.Start(httptest.NewRequest(http.MethodGet, "http://proxy.local/", nil))
	prepareInlineExchange(t, current)
	attempt, err := current.BeginAttempt(func() {})
	if err != nil {
		t.Fatal(err)
	}
	if err := attempt.OpenScope(); err != nil {
		t.Fatal(err)
	}
	if err := attempt.OpenScope(); !errors.Is(err, ErrAttemptScopeOpened) {
		t.Fatalf("duplicate module program was accepted: %v", err)
	}
	current.Complete()
}

func TestExchangeEnforcesDirectivePreparationOrder(t *testing.T) {
	manager := NewManager(ManagerOptions{MaxAttempts: 2}, nil)
	current := manager.Start(httptest.NewRequest(http.MethodGet, "http://proxy.local/", nil))
	info := DirectiveInfo{Mode: "inline", Target: mustURL(t, "https://upstream.example")}
	if err := current.PrepareDirective(info); !errors.Is(err, ErrProgramNotConfigured) {
		t.Fatalf("directive was prepared before program configuration: %v", err)
	}
	if err := current.ConfigureProgram(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := current.BeginAttempt(func() {}); !errors.Is(err, ErrDirectiveNotPrepared) {
		t.Fatalf("attempt started before directive preparation: %v", err)
	}
	if err := current.PrepareDirective(info); err != nil {
		t.Fatal(err)
	}
	if err := current.PrepareDirective(info); !errors.Is(err, ErrDirectiveAlreadySet) {
		t.Fatalf("directive was prepared twice: %v", err)
	}
	current.Complete()
}

func TestExchangeFailsClosedWhenProgramRuntimeIsUnavailable(t *testing.T) {
	manager := NewManager(ManagerOptions{MaxAttempts: 2}, nil)
	current := manager.Start(httptest.NewRequest(http.MethodGet, "http://proxy.local/", nil))
	if err := current.ConfigureProgram(&program.Executable{}); err == nil {
		t.Fatal("compiled directive program was silently skipped")
	}
	current.Complete()
}

func TestConcurrentRecoveryRetryIsIdempotent(t *testing.T) {
	manager := NewManager(ManagerOptions{MaxAttempts: 3}, nil)
	current := manager.Start(httptest.NewRequest(http.MethodGet, "http://proxy.local/", nil))
	prepareInlineExchange(t, current)
	var cancelCount atomic.Int32
	attempt, err := current.BeginAttempt(func() { cancelCount.Add(1) })
	if err != nil {
		t.Fatal(err)
	}
	if !attempt.BeginUpstream(nil) {
		t.Fatal("attempt did not enter upstream state")
	}
	if !attempt.BeginRecovery() {
		t.Fatal("attempt did not enter recovery state")
	}

	const callers = 32
	var wait sync.WaitGroup
	errorsSeen := make(chan error, callers)
	wait.Add(callers)
	for range callers {
		go func() {
			defer wait.Done()
			retryErr := attempt.RequestRecoveryRetry()
			if retryErr != nil {
				errorsSeen <- retryErr
				return
			}
		}()
	}
	wait.Wait()
	close(errorsSeen)
	for retryErr := range errorsSeen {
		t.Error(retryErr)
	}
	if cancelCount.Load() != 1 {
		t.Fatalf("attempt cancel ran %d times", cancelCount.Load())
	}
	current.Complete()
}

func TestCanceledExchangeDrainsAsyncModulesBeforeFinish(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	handled := make(chan struct{})
	runtime, err := program.NewRuntime([]module.Definition{drainDefinition{
		started: started,
		release: release,
		handled: handled,
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	executable, err := runtime.Compile(program.Program{Request: []program.Spec{{ID: "drain", Module: "test.drain"}}})
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(ManagerOptions{MaxAttempts: 2}, runtime)
	requestContext, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest(http.MethodGet, "http://proxy.local/", nil).WithContext(requestContext)
	current := manager.Start(request)
	if err := current.ConfigureProgram(executable); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("async module did not start")
	}

	cancel()
	completed := make(chan struct{})
	go func() {
		current.Complete()
		close(completed)
	}()
	select {
	case <-completed:
		close(release)
		<-handled
		t.Fatal("exchange completed before its async module lane drained")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	select {
	case <-completed:
	case <-time.After(time.Second):
		t.Fatal("exchange did not complete after async module drained")
	}
	select {
	case <-handled:
	default:
		t.Fatal("async module was not handled before completion")
	}
}

type drainDefinition struct {
	started chan struct{}
	release chan struct{}
	handled chan struct{}
}

func (drainDefinition) Name() string { return "test.drain" }

func (definition drainDefinition) Compile(json.RawMessage) (module.Binding, error) {
	return drainBinding(definition), nil
}

type drainBinding drainDefinition

func (drainBinding) Scope() module.ScopeKind { return module.ScopeRequest }

func (binding drainBinding) Open(module.OpenContext) (module.Instance, error) {
	return drainInstance(binding), nil
}

type drainInstance drainBinding

func (instance drainInstance) Bind(binder module.Registrar) {
	binder.OnRequestStarted(module.AsyncPolicy(module.OverflowBlock), func(module.Context, lifecycle.RequestStarted) error {
		close(instance.started)
		<-instance.release
		close(instance.handled)
		return nil
	})
}

func (drainInstance) Finish(module.FinishContext) error { return nil }
