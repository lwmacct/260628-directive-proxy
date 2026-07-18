package program

import (
	"context"
	"net/http"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/lifecycle"
	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
)

type testBinding struct {
	scope module.ScopeKind
	open  func() module.Instance
}

func (binding testBinding) Scope() module.ScopeKind { return binding.scope }
func (binding testBinding) Open(module.OpenContext) (module.Instance, error) {
	return binding.open(), nil
}

type testInstance struct {
	bind   func(module.Registrar)
	finish func(module.FinishContext) error
}

func (instance testInstance) Bind(registrar module.Registrar) {
	if instance.bind != nil {
		instance.bind(registrar)
	}
}
func (instance testInstance) Finish(ctx module.FinishContext) error {
	if instance.finish != nil {
		return instance.finish(ctx)
	}
	return nil
}

func TestAsyncBeforeCommitBarrierPreservesModuleOrder(t *testing.T) {
	var mu sync.Mutex
	var events []string
	appendEvent := func(value string) {
		mu.Lock()
		events = append(events, value)
		mu.Unlock()
	}
	instance := testInstance{bind: func(registrar module.Registrar) {
		registrar.OnRequestStarted(module.AsyncPolicy(module.OverflowBlock), func(module.Context, lifecycle.RequestStarted) error {
			appendEvent("started")
			return nil
		})
		registrar.OnRequestBodyEnded(module.AsyncBarrierPolicy(module.OverflowBlock), func(module.Context, lifecycle.RequestBodyEnded) error {
			appendEvent("body-ended")
			return nil
		})
	}}
	scope := openTestScope(t, []compiled{{id: "ordered", moduleName: "test.ordered", binding: testBinding{scope: module.ScopeRequest, open: func() module.Instance { return instance }}}})
	if err := scope.RequestStarted(t.Context(), lifecycle.RequestStarted{}); err != nil {
		t.Fatal(err)
	}
	if err := scope.RequestBodyEnded(t.Context(), lifecycle.RequestBodyEnded{}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got := append([]string(nil), events...)
	mu.Unlock()
	if !reflect.DeepEqual(got, []string{"started", "body-ended"}) {
		t.Fatalf("barrier did not preserve order: %#v", got)
	}
	_ = scope.Finish(context.Background(), module.FinishCompleted)
}

func TestMutatorsRunInDirectiveProgramOrder(t *testing.T) {
	entries := []compiled{
		mutationModule("second", "2"),
		mutationModule("first", "1"),
	}
	scope := openTestScope(t, entries)
	request, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)
	if err := scope.MutateOutboundRequest(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if got := request.Header.Values("X-Module-Order"); !reflect.DeepEqual(got, []string{"2", "1"}) {
		t.Fatalf("mutators ignored directive order: %#v", got)
	}
}

func TestStreamObserverCreatesOnlySubscribedSSEView(t *testing.T) {
	var rawChunks int
	var events []lifecycle.SSEData
	instance := testInstance{bind: func(registrar module.Registrar) {
		registrar.OnUpstreamBodyChunk(module.SyncPolicy(), func(module.Context, lifecycle.BodyChunk) error {
			rawChunks++
			return nil
		})
		registrar.OnUpstreamSSEData(module.SyncPolicy(), func(_ module.Context, event lifecycle.SSEData) error {
			events = append(events, event)
			return nil
		})
	}}
	scope := openTestScope(t, []compiled{{id: "projection", moduleName: "test.projection", binding: testBinding{scope: module.ScopeAttempt, open: func() module.Instance { return instance }}}})
	observer := NewUpstreamObserver("text/event-stream; charset=utf-8", 1024, scope)
	if err := observer.Observe(t.Context(), time.Now(), []byte("event: delta\ndata: hello\n\n")); err != nil {
		t.Fatal(err)
	}
	if err := observer.Finish(t.Context(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if rawChunks != 0 || len(events) != 1 || events[0].Event != "delta" || string(events[0].Data) != "hello" {
		t.Fatalf("unexpected projection delivery: raw=%d events=%#v", rawChunks, events)
	}
}

func TestAsyncBodyChunkOwnsItsQueuedView(t *testing.T) {
	allow := make(chan struct{})
	seen := make(chan string, 1)
	instance := testInstance{bind: func(registrar module.Registrar) {
		registrar.OnUpstreamBodyChunk(module.AsyncPolicy(module.OverflowBlock), func(_ module.Context, chunk lifecycle.BodyChunk) error {
			<-allow
			seen <- string(chunk.Data)
			return nil
		})
	}}
	scope := openTestScope(t, []compiled{{id: "copy", moduleName: "test.copy", binding: testBinding{scope: module.ScopeAttempt, open: func() module.Instance { return instance }}}})
	data := []byte("original")
	if err := scope.UpstreamBodyChunk(t.Context(), lifecycle.BodyChunk{Data: data}); err != nil {
		t.Fatal(err)
	}
	copy(data, "modified")
	close(allow)
	if got := <-seen; got != "original" {
		t.Fatalf("queued body view was borrowed: %q", got)
	}
	_ = scope.Finish(context.Background(), module.FinishCompleted)
}

func TestDroppedBeforeCommitHandlerDoesNotWait(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	instance := testInstance{bind: func(registrar module.Registrar) {
		registrar.OnRequestStarted(module.Policy{
			Executor: module.ExecutorOrderedLane,
			Barrier:  module.BarrierScopeEnd,
			Overflow: module.OverflowBlock,
			Capacity: 1,
		}, func(module.Context, lifecycle.RequestStarted) error {
			select {
			case <-started:
			default:
				close(started)
			}
			<-release
			return nil
		})
		registrar.OnRequestBodyEnded(module.Policy{
			Executor: module.ExecutorOrderedLane,
			Barrier:  module.BarrierBeforeCommit,
			Overflow: module.OverflowDrop,
			Capacity: 1,
		}, func(module.Context, lifecycle.RequestBodyEnded) error {
			return nil
		})
	}}
	scope := openTestScope(t, []compiled{{id: "drop", moduleName: "test.drop", binding: testBinding{
		scope: module.ScopeRequest,
		open:  func() module.Instance { return instance },
	}}})
	if err := scope.RequestStarted(t.Context(), lifecycle.RequestStarted{}); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := scope.RequestStarted(t.Context(), lifecycle.RequestStarted{}); err != nil {
		t.Fatal(err)
	}
	if err := scope.RequestBodyEnded(t.Context(), lifecycle.RequestBodyEnded{}); err != nil {
		t.Fatalf("dropped barrier handler blocked or failed: %v", err)
	}
	close(release)
	if err := scope.Finish(t.Context(), module.FinishCompleted); err != nil {
		t.Fatal(err)
	}
}

func mutationModule(id, value string) compiled {
	instance := testInstance{bind: func(registrar module.Registrar) {
		registrar.MutateOutboundRequest(module.SyncPolicy(), func(_ module.Context, request *http.Request) error {
			request.Header.Add("X-Module-Order", value)
			return nil
		})
	}}
	return compiled{id: id, moduleName: "test.mutation", binding: testBinding{
		scope: module.ScopeAttempt,
		open:  func() module.Instance { return instance },
	}}
}

func openTestScope(t *testing.T, entries []compiled) *Scope {
	t.Helper()
	scope, err := openScope(module.OpenContext{TraceID: "trace", Attempt: 1}, entries, nil)
	if err != nil {
		t.Fatal(err)
	}
	return scope
}
