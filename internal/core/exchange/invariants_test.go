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

func TestExchangeRejectsRoundTripAfterTerminalRoundTrip(t *testing.T) {
	manager := NewManager(ManagerOptions{MaxRoundTrips: 3}, nil)
	current := manager.Start(httptest.NewRequest(http.MethodGet, "http://proxy.local/", nil))
	prepareInlineExchange(t, current)
	roundTrip, err := current.BeginRoundTrip(func() {})
	if err != nil {
		t.Fatal(err)
	}
	roundTrip.FinishRoundTrip(false, errors.New("upstream failed"))
	if _, err := current.BeginRoundTrip(func() {}); !errors.Is(err, context.Canceled) {
		t.Fatalf("terminal exchange accepted another roundTrip: %v", err)
	}
	current.Complete()
}

func TestWrapResponseWriterUsesDPTraceHeader(t *testing.T) {
	manager := NewManager(ManagerOptions{MaxRoundTrips: 1}, nil)
	current := manager.Start(httptest.NewRequest(http.MethodGet, "http://proxy.local/", nil))
	recorder := httptest.NewRecorder()
	current.WrapResponseWriter(recorder)
	if recorder.Header().Get("X-Dp-Trace-ID") != current.TraceID() {
		t.Fatalf("missing X-Dp trace header: %#v", recorder.Header())
	}
	if len(recorder.Header()) != 1 {
		t.Fatalf("unexpected response headers: %#v", recorder.Header())
	}
	current.Complete()
}

func TestRoundTripScopeCanOnlyBeOpenedOnce(t *testing.T) {
	manager := NewManager(ManagerOptions{MaxRoundTrips: 2}, nil)
	current := manager.Start(httptest.NewRequest(http.MethodGet, "http://proxy.local/", nil))
	prepareInlineExchange(t, current)
	roundTrip, err := current.BeginRoundTrip(func() {})
	if err != nil {
		t.Fatal(err)
	}
	if err := roundTrip.OpenScope(); err != nil {
		t.Fatal(err)
	}
	if err := roundTrip.OpenScope(); !errors.Is(err, ErrRoundTripScopeOpened) {
		t.Fatalf("duplicate module program was accepted: %v", err)
	}
	current.Complete()
}

func TestExchangeEnforcesDirectivePreparationOrder(t *testing.T) {
	manager := NewManager(ManagerOptions{MaxRoundTrips: 2}, nil)
	current := manager.Start(httptest.NewRequest(http.MethodGet, "http://proxy.local/", nil))
	if _, err := current.BeginRoundTrip(func() {}); !errors.Is(err, ErrExchangeNotConfigured) {
		t.Fatalf("roundTrip started before exchange configuration: %v", err)
	}
	configuration := Configuration{
		Directive: DirectiveInfo{Mode: "inline", Target: mustURL(t, "https://upstream.example")},
		Metadata:  exchangeMetadata(t, map[string]string{"user_key": "uk_test"}),
	}
	if err := current.Configure(configuration); err != nil {
		t.Fatal(err)
	}
	if err := current.Configure(configuration); !errors.Is(err, ErrExchangeConfigured) {
		t.Fatalf("exchange was configured twice: %v", err)
	}
	current.Complete()
}

func TestExchangeFailsClosedWhenProgramRuntimeIsUnavailable(t *testing.T) {
	manager := NewManager(ManagerOptions{MaxRoundTrips: 2}, nil)
	current := manager.Start(httptest.NewRequest(http.MethodGet, "http://proxy.local/", nil))
	if err := current.Configure(Configuration{
		Directive: DirectiveInfo{Mode: "inline", Target: mustURL(t, "https://upstream.example")},
		Metadata:  exchangeMetadata(t, map[string]string{"user_key": "uk_test"}), Program: &program.Executable{},
	}); err == nil {
		t.Fatal("compiled directive program was silently skipped")
	}
	current.Complete()
}

func TestConcurrentRecoveryRetryIsIdempotent(t *testing.T) {
	manager := NewManager(ManagerOptions{MaxRoundTrips: 3}, nil)
	current := manager.Start(httptest.NewRequest(http.MethodGet, "http://proxy.local/", nil))
	prepareInlineExchange(t, current)
	var cancelCount atomic.Int32
	roundTrip, err := current.BeginRoundTrip(func() { cancelCount.Add(1) })
	if err != nil {
		t.Fatal(err)
	}
	if !roundTrip.BeginUpstream(nil) {
		t.Fatal("roundTrip did not enter upstream state")
	}
	if !roundTrip.BeginRecovery() {
		t.Fatal("roundTrip did not enter recovery state")
	}

	const callers = 32
	var wait sync.WaitGroup
	errorsSeen := make(chan error, callers)
	wait.Add(callers)
	for range callers {
		go func() {
			defer wait.Done()
			retryErr := roundTrip.RequestRecoveryRetry()
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
		t.Fatalf("roundTrip cancel ran %d times", cancelCount.Load())
	}
	current.Complete()
}

func TestCanceledExchangeDrainsAsyncModulesBeforeFinish(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	handled := make(chan struct{})
	runtime, err := program.NewRuntime(module.MustCatalog(drainDefinition{
		started: started,
		release: release,
		handled: handled,
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	executable, err := runtime.Compile(program.Program{{Module: "test.drain"}})
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(ManagerOptions{MaxRoundTrips: 2}, runtime)
	requestContext, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest(http.MethodGet, "http://proxy.local/", nil).WithContext(requestContext)
	current := manager.Start(request)
	if err := current.Configure(Configuration{
		Directive: DirectiveInfo{Mode: "inline", Target: mustURL(t, "https://upstream.example")},
		Metadata:  exchangeMetadata(t, map[string]string{"user_key": "uk_test"}), Program: executable,
	}); err != nil {
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

func (drainDefinition) Name() string              { return "test.drain" }
func (drainDefinition) Lifetime() module.Lifetime { return module.LifetimeExchange }

func (definition drainDefinition) CompileProgram(_ json.RawMessage) (module.Binding, error) {
	return drainBinding(definition), nil
}

type drainBinding drainDefinition

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
