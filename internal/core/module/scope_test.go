package module

import (
	"context"
	"net/http"
	"reflect"
	"sync"
	"testing"
	"time"
)

type testBinding struct {
	lifetime Lifetime
	open     func() Instance
}

func (binding testBinding) Lifetime() Lifetime { return binding.lifetime }
func (binding testBinding) Open(OpenContext) (Instance, error) {
	return binding.open(), nil
}

type testInstance struct {
	mount  func(*Binder)
	finish func(FinishContext) error
}

func (instance testInstance) Mount(binder *Binder) { instance.mount(binder) }
func (instance testInstance) Finish(ctx FinishContext) error {
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
	instance := testInstance{mount: func(binder *Binder) {
		binder.OnRequestStarted(AsyncPolicy(OverflowBlock), func(EventContext, RequestStarted) error {
			appendEvent("started")
			return nil
		})
		binder.OnRequestBodyEnded(AsyncBarrierPolicy(OverflowBlock), func(EventContext, RequestBodyEnded) error {
			appendEvent("body-ended")
			return nil
		})
	}}
	scope := openTestScope(t, []Compiled{{Spec: Spec{ID: "ordered", Module: "test.ordered"}, Binding: testBinding{lifetime: LifetimeRequest, open: func() Instance { return instance }}}})
	if err := scope.RequestStarted(t.Context(), RequestStarted{}); err != nil {
		t.Fatal(err)
	}
	if err := scope.RequestBodyEnded(t.Context(), RequestBodyEnded{}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got := append([]string(nil), events...)
	mu.Unlock()
	if !reflect.DeepEqual(got, []string{"started", "body-ended"}) {
		t.Fatalf("barrier did not preserve order: %#v", got)
	}
	_ = scope.Finish(context.Background(), FinishCompleted)
}

func TestMutatorsRunInDirectiveProgramOrder(t *testing.T) {
	compiled := []Compiled{
		mutationModule("second", "2"),
		mutationModule("first", "1"),
	}
	scope := openTestScope(t, compiled)
	request, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)
	if err := scope.MutateOutboundRequest(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if got := request.Header.Values("X-Module-Order"); !reflect.DeepEqual(got, []string{"2", "1"}) {
		t.Fatalf("mutators ignored directive order: %#v", got)
	}
}

func TestProjectionCreatesOnlySubscribedSSEView(t *testing.T) {
	var rawChunks int
	var events []SSEData
	instance := testInstance{mount: func(binder *Binder) {
		binder.OnUpstreamBodyChunk(SyncPolicy(), func(EventContext, BodyChunk) error {
			rawChunks++
			return nil
		})
		binder.OnUpstreamSSEData(SyncPolicy(), func(_ EventContext, event SSEData) error {
			events = append(events, event)
			return nil
		})
	}}
	scope := openTestScope(t, []Compiled{{Spec: Spec{ID: "projection", Module: "test.projection"}, Binding: testBinding{lifetime: LifetimeAttempt, open: func() Instance { return instance }}}})
	projection := NewProjection(ProjectionUpstream, "text/event-stream; charset=utf-8", 1024, scope)
	if err := projection.Feed(t.Context(), time.Now(), []byte("event: delta\ndata: hello\n\n")); err != nil {
		t.Fatal(err)
	}
	if err := projection.Close(t.Context(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if rawChunks != 0 || len(events) != 1 || events[0].Event != "delta" || string(events[0].Data) != "hello" {
		t.Fatalf("unexpected projection delivery: raw=%d events=%#v", rawChunks, events)
	}
}

func TestAsyncBodyChunkOwnsItsQueuedView(t *testing.T) {
	allow := make(chan struct{})
	seen := make(chan string, 1)
	instance := testInstance{mount: func(binder *Binder) {
		binder.OnUpstreamBodyChunk(AsyncPolicy(OverflowBlock), func(_ EventContext, chunk BodyChunk) error {
			<-allow
			seen <- string(chunk.Data)
			return nil
		})
	}}
	scope := openTestScope(t, []Compiled{{Spec: Spec{ID: "copy", Module: "test.copy"}, Binding: testBinding{lifetime: LifetimeAttempt, open: func() Instance { return instance }}}})
	data := []byte("original")
	if err := scope.UpstreamBodyChunk(t.Context(), BodyChunk{Data: data}); err != nil {
		t.Fatal(err)
	}
	copy(data, "modified")
	close(allow)
	if got := <-seen; got != "original" {
		t.Fatalf("queued body view was borrowed: %q", got)
	}
	_ = scope.Finish(context.Background(), FinishCompleted)
}

func mutationModule(id, value string) Compiled {
	instance := testInstance{mount: func(binder *Binder) {
		binder.MutateOutboundRequest(SyncPolicy(), func(_ EventContext, request *http.Request) error {
			request.Header.Add("X-Module-Order", value)
			return nil
		})
	}}
	return Compiled{Spec: Spec{ID: id, Module: "test.mutation"}, Binding: testBinding{
		lifetime: LifetimeAttempt, open: func() Instance { return instance },
	}}
}

func openTestScope(t *testing.T, compiled []Compiled) *Scope {
	t.Helper()
	scope, err := OpenScope(OpenContext{TraceID: "trace", Attempt: 1}, compiled, nil)
	if err != nil {
		t.Fatal(err)
	}
	return scope
}
