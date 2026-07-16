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

	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
	"github.com/lwmacct/260628-directive-proxy/internal/core/retry"
)

func TestExchangeRejectsAttemptAfterTerminalRoundTrip(t *testing.T) {
	manager := NewManager(ManagerOptions{MaxAttempts: 3}, nil)
	current := manager.Start(httptest.NewRequest(http.MethodGet, "http://proxy.local/", nil), retry.Identity{})
	attempt, err := current.BeginAttempt(func() {}, AttemptSource{Mode: "inline"})
	if err != nil {
		t.Fatal(err)
	}
	attempt.FinishRoundTrip(false, errors.New("upstream failed"))
	if _, err := current.BeginAttempt(func() {}, AttemptSource{Mode: "inline"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("terminal exchange accepted another attempt: %v", err)
	}
	current.Complete()
}

func TestAttemptModuleProgramCanOnlyBeConfiguredOnce(t *testing.T) {
	manager := NewManager(ManagerOptions{MaxAttempts: 2}, nil)
	current := manager.Start(httptest.NewRequest(http.MethodGet, "http://proxy.local/", nil), retry.Identity{})
	attempt, err := current.BeginAttempt(func() {}, AttemptSource{Mode: "inline"})
	if err != nil {
		t.Fatal(err)
	}
	if err := attempt.ConfigureModules(nil); err != nil {
		t.Fatal(err)
	}
	if err := attempt.ConfigureModules(nil); !errors.Is(err, ErrAttemptConfigured) {
		t.Fatalf("duplicate module program was accepted: %v", err)
	}
	current.Complete()
}

func TestConcurrentRetryCommandsAreIdempotent(t *testing.T) {
	manager := NewManager(ManagerOptions{MaxAttempts: 3}, nil)
	current := manager.Start(httptest.NewRequest(http.MethodGet, "http://proxy.local/", nil), retry.Identity{})
	var cancelCount atomic.Int32
	attempt, err := current.BeginAttempt(func() { cancelCount.Add(1) }, AttemptSource{Mode: "inline"})
	if err != nil {
		t.Fatal(err)
	}
	if !attempt.BeginUpstream(nil) {
		t.Fatal("attempt did not enter upstream state")
	}

	const callers = 32
	var wait sync.WaitGroup
	errorsSeen := make(chan error, callers)
	wait.Add(callers)
	for range callers {
		go func() {
			defer wait.Done()
			result, retryErr := manager.RetryByTraceID(current.TraceID(), attempt.Number(), TriggerAdminAPI)
			if retryErr != nil {
				errorsSeen <- retryErr
				return
			}
			if result.NextAttempt != 2 || result.Exchange.Phase != PhaseRetryRequested {
				errorsSeen <- errors.New("retry command returned an inconsistent result")
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
	runtime, err := module.NewRuntime([]module.Definition{drainDefinition{
		started: started,
		release: release,
		handled: handled,
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(ManagerOptions{MaxAttempts: 2}, runtime)
	requestContext, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest(http.MethodGet, "http://proxy.local/", nil).WithContext(requestContext)
	current := manager.Start(request, retry.Identity{})
	if err := current.ConfigureRequest([]module.Spec{{ID: "drain", Module: "test.drain"}}); err != nil {
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

func (drainBinding) Lifetime() module.Lifetime { return module.LifetimeRequest }

func (binding drainBinding) Open(module.OpenContext) (module.Instance, error) {
	return drainInstance(binding), nil
}

type drainInstance drainBinding

func (instance drainInstance) Mount(binder *module.Binder) {
	binder.OnRequestStarted(module.AsyncPolicy(module.OverflowBlock), func(module.EventContext, module.RequestStarted) error {
		close(instance.started)
		<-instance.release
		close(instance.handled)
		return nil
	})
}

func (drainInstance) Finish(module.FinishContext) error { return nil }
