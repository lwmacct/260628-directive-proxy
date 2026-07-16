package exchange

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/event"
	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
	"github.com/lwmacct/260628-directive-proxy/internal/core/requestmeta"
	"github.com/lwmacct/260628-directive-proxy/internal/core/retry"
	"github.com/lwmacct/260628-directive-proxy/internal/modules/capture"
	recordoutput "github.com/lwmacct/260628-directive-proxy/internal/testutil/recordoutput"
)

func TestExchangeLifecycleTracksRetryAndEmitsSSEEvents(t *testing.T) {
	runtime, dispatcher, output := newCaptureRuntime(t)
	manager := NewManager(ManagerOptions{MaxAttempts: 3}, runtime)
	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/chat?token=secret", nil)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Idempotency-Key", "lifecycle-test")
	current := manager.Start(req, retry.Identity{})
	captureSpec := []byte(`{"body-chunk-bytes":4,"redact-headers":["authorization"],"redact-query":["token"]}`)
	if err := current.ConfigureRequest([]module.Spec{{ID: "capture", Module: capture.Name, Config: captureSpec}}); err != nil {
		t.Fatal(err)
	}
	if _, err := retry.ParseID(current.TraceID()); err != nil {
		t.Fatalf("unexpected trace ID: %q", current.TraceID())
	}
	target := mustURL(t, "https://upstream.example/v1/chat?token=upstream-secret")
	current.RequestBodyEnd(5, "digest", true)

	canceled := false
	first, err := current.BeginAttempt(func() { canceled = true }, AttemptSource{Mode: "inline"})
	if err != nil {
		t.Fatal(err)
	}
	if err := first.ConfigureModules(nil); err != nil {
		t.Fatal(err)
	}
	first.BindMetadata(requestmeta.Metadata{"X-Dproxy-Request-Id": {"request-1"}})
	first.DirectiveResolved(target, time.Millisecond, "", false, false)
	if !first.BeginUpstream(req) {
		t.Fatal("first attempt did not enter upstream state")
	}
	active := manager.ListActive()
	if len(active) != 1 || active[0].Attempt != 1 || active[0].TargetURL != "https://upstream.example/v1/chat?token=%3Credacted%3E" {
		t.Fatalf("unexpected active exchange: %#v", active)
	}
	result, err := manager.RetryByTraceID(current.TraceID(), first.Number(), TriggerAdminAPI)
	if err != nil || result.NextAttempt != 2 || !canceled {
		t.Fatalf("retry was not accepted: result=%#v canceled=%t err=%v", result, canceled, err)
	}
	if decision := first.FinishRoundTrip(false, context.Canceled); decision != DecisionRetry {
		t.Fatalf("unexpected attempt decision: %v", decision)
	}
	second, err := current.BeginAttempt(func() {}, AttemptSource{Mode: "inline"})
	if err != nil {
		t.Fatal(err)
	}
	if err := second.ConfigureModules(nil); err != nil {
		t.Fatal(err)
	}
	if second.Number() != 2 {
		t.Fatalf("unexpected second attempt: %d", second.Number())
	}
	if !second.BindMetadata(requestmeta.Metadata{"X-Dproxy-Request-Id": {"changed"}}) {
		t.Fatal("metadata change was not detected")
	}
	second.DirectiveResolved(target, time.Millisecond, "", false, false)
	if !second.BeginUpstream(req) {
		t.Fatal("second attempt did not enter upstream state")
	}
	if decision := second.FinishRoundTrip(true, nil); decision != DecisionReturn {
		t.Fatalf("unexpected response decision: %v", decision)
	}
	if len(manager.ListActive()) != 0 {
		t.Fatal("exchange remained retryable after response headers")
	}

	recorder := httptest.NewRecorder()
	wrapped := current.WrapResponseWriter(recorder)
	wrapped.Header().Set("Content-Type", "text/event-stream")
	wrapped.WriteHeader(http.StatusOK)
	_, _ = wrapped.Write([]byte(": ping\n\nid: 9\nevent: delta\ndata: one\ndata: two\n\n"))
	current.Complete()
	if recorder.Header().Get("X-Dproxy-Trace-ID") != current.TraceID() {
		t.Fatalf("tracking response header missing: %#v", recorder.Header())
	}
	runtime.Close()
	if err := dispatcher.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	events := output.Records()
	var sawSSE, sawComment, sawResolveFinished, sawUpstreamStarted, sawMetadata bool
	var previous uint64
	for _, record := range events {
		if record.Sequence <= previous {
			t.Fatalf("capture sequence is not increasing: %#v", events)
		}
		previous = record.Sequence
		switch record.Topic {
		case "capture.directive.resolve.finished":
			sawResolveFinished = record.Data["target_url"] == "https://upstream.example/v1/chat?token=%3Credacted%3E"
		case "capture.attempt.upstream.started":
			sawUpstreamStarted = record.Attempt > 0
		case "capture.request.metadata.bound":
			sawMetadata = record.Data["metadata"] != nil
		case "capture.response.sse.event":
			sawSSE = record.Data["data"] == "one\ntwo" && record.Data["upstream_event_id"] == "9"
		case "capture.response.sse.comment":
			sawComment = true
		}
	}
	if !sawSSE || !sawComment || !sawResolveFinished || !sawUpstreamStarted || !sawMetadata {
		t.Fatalf("missing capture events: sse=%t comment=%t resolve=%t upstream=%t metadata=%t events=%#v", sawSSE, sawComment, sawResolveFinished, sawUpstreamStarted, sawMetadata, events)
	}
}

func TestManagerRetryByRetryIDUsesProofAndCAS(t *testing.T) {
	runtime, dispatcher, output := newCaptureRuntime(t)
	manager := NewManager(ManagerOptions{MaxAttempts: 3}, runtime)
	newActive := func(path string, seed byte, canceled *bool) (*Exchange, retry.Identity, [32]byte) {
		identity, digest := testIdentity(t, seed)
		current := manager.Start(httptest.NewRequest(http.MethodGet, "http://proxy.local/"+path, nil), identity)
		if err := current.ConfigureRequest([]module.Spec{{ID: "capture", Module: capture.Name, Config: []byte(`{}`)}}); err != nil {
			t.Fatal(err)
		}
		attempt, err := current.BeginAttempt(func() { *canceled = true }, AttemptSource{Mode: "inline"})
		if err != nil {
			t.Fatal(err)
		}
		_ = attempt.ConfigureModules(nil)
		attempt.DirectiveResolved(mustURL(t, "https://upstream.example"), 0, "", false, false)
		if !attempt.BeginUpstream(nil) {
			t.Fatal("attempt did not enter upstream state")
		}
		return current, identity, digest
	}
	var firstCanceled, secondCanceled bool
	first, _, _ := newActive("one", 1, &firstCanceled)
	second, _, secondDigest := newActive("two", 2, &secondCanceled)
	if _, err := manager.RetryByRetryID([32]byte{}, 1, TriggerRequesterAPI); err != ErrNotFound {
		t.Fatalf("invalid proof was accepted: %v", err)
	}
	result, err := manager.RetryByRetryID(secondDigest, 1, TriggerRequesterAPI)
	if err != nil || result.Exchange.TraceID != second.TraceID() || !secondCanceled || firstCanceled {
		t.Fatalf("unexpected retry ID result: result=%#v err=%v", result, err)
	}
	if repeated, err := manager.RetryByRetryID(secondDigest, 1, TriggerRequesterAPI); err != nil || repeated.NextAttempt != 2 {
		t.Fatalf("duplicate retry was not idempotent: result=%#v err=%v", repeated, err)
	}
	first.Complete()
	second.Complete()
	if repeated, err := manager.RetryByRetryID(secondDigest, 1, TriggerRequesterAPI); err != nil || repeated.NextAttempt != 2 {
		t.Fatalf("terminal retry tombstone was not idempotent: result=%#v err=%v", repeated, err)
	}
	runtime.Close()
	_ = dispatcher.Close(context.Background())
	sawRequesterRetry := false
	for _, record := range output.Records() {
		if record.Topic == "capture.retry.requested" && record.Data["trigger"] == string(TriggerRequesterAPI) {
			sawRequesterRetry = true
		}
	}
	if !sawRequesterRetry {
		t.Fatal("requester retry was not captured")
	}
}

func TestExchangeMetadataCaptureUsesHeaderRedactionPolicy(t *testing.T) {
	runtime, dispatcher, output := newCaptureRuntime(t)
	manager := NewManager(ManagerOptions{MaxAttempts: 2}, runtime)
	current := manager.Start(httptest.NewRequest(http.MethodGet, "http://proxy.local/", nil), retry.Identity{})
	if err := current.ConfigureRequest([]module.Spec{{ID: "capture", Module: capture.Name, Config: []byte(`{"redact-headers":["x-dproxy-secret-*"],"redact-query":[]}`)}}); err != nil {
		t.Fatal(err)
	}
	attempt, _ := current.BeginAttempt(func() {}, AttemptSource{Mode: "inline"})
	_ = attempt.ConfigureModules(nil)
	attempt.BindMetadata(requestmeta.Metadata{
		"X-Dproxy-Request-Id": {"request-1"}, "X-Dproxy-Secret-Key": {"secret"},
	})
	current.Complete()
	runtime.Close()
	_ = dispatcher.Close(context.Background())
	for _, record := range output.Records() {
		if record.Topic != "capture.request.metadata.bound" {
			continue
		}
		metadata := record.Data["metadata"].(map[string][]string)
		if metadata["X-Dproxy-Secret-Key"][0] != "<redacted>" || metadata["X-Dproxy-Request-Id"][0] != "request-1" {
			t.Fatalf("unexpected captured metadata: %#v", metadata)
		}
		return
	}
	t.Fatal("metadata capture event was not emitted")
}

func TestModuleRuntimeRejectsUnknownOrInvalidRequestModules(t *testing.T) {
	runtime, dispatcher, _ := newCaptureRuntime(t)
	manager := NewManager(ManagerOptions{MaxAttempts: 2}, runtime)
	for _, specs := range [][]module.Spec{
		{{ID: "missing", Module: "missing.module", Config: []byte(`{}`)}},
		{{ID: "capture", Module: capture.Name, Config: []byte(`{"body-chunk-bytes":-1}`)}},
	} {
		current := manager.Start(httptest.NewRequest(http.MethodGet, "http://proxy.local/", nil), retry.Identity{})
		if err := current.ConfigureRequest(specs); err == nil {
			t.Fatalf("invalid request module was accepted: %#v", specs)
		}
		current.Complete()
	}
	runtime.Close()
	_ = dispatcher.Close(context.Background())
}

func TestManagerRetryRejectsEarlyAndStaleAttempts(t *testing.T) {
	manager := NewManager(ManagerOptions{MaxAttempts: 2}, nil)
	current := manager.Start(httptest.NewRequest(http.MethodGet, "http://proxy.local/", nil), retry.Identity{})
	attempt, _ := current.BeginAttempt(func() {}, AttemptSource{Mode: "inline"})
	if active, ok := manager.GetActive(current.TraceID()); !ok || active.Phase != PhaseResolving {
		t.Fatalf("unexpected resolving phase: active=%#v ok=%t", active, ok)
	}
	if _, err := manager.RetryByTraceID(current.TraceID(), attempt.Number(), TriggerAdminAPI); err != ErrRetryNotReady {
		t.Fatalf("resolving attempt was retryable: %v", err)
	}
	attempt.DirectiveResolved(mustURL(t, "https://upstream.example"), 0, "", false, false)
	attempt.BeginUpstream(nil)
	if _, err := manager.RetryByTraceID(current.TraceID(), attempt.Number(), TriggerAdminAPI); err != nil {
		t.Fatalf("retry after upstream start failed: %v", err)
	}
	if _, err := manager.RetryByTraceID(current.TraceID(), attempt.Number()+1, TriggerAdminAPI); err != ErrAttemptChanged {
		t.Fatalf("unexpected stale attempt error: %v", err)
	}
	current.Complete()
}

func TestManagerRetryRequiresIdempotencyKeyForPostAndPatch(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			manager := NewManager(ManagerOptions{MaxAttempts: 2}, nil)
			current := manager.Start(httptest.NewRequest(method, "http://proxy.local/resource", nil), retry.Identity{})
			attempt, _ := current.BeginAttempt(func() {}, AttemptSource{Mode: "inline"})
			attempt.DirectiveResolved(mustURL(t, "https://upstream.example"), 0, "", false, false)
			if !attempt.BeginUpstream(nil) {
				t.Fatal("attempt did not enter upstream state")
			}
			if _, err := manager.RetryByTraceID(current.TraceID(), attempt.Number(), TriggerAdminAPI); err != ErrIdempotencyKeyRequired {
				t.Fatalf("unexpected retry result: %v", err)
			}
			current.Complete()
		})
	}
}

func TestManagerRejectsDuplicateRetryID(t *testing.T) {
	manager := NewManager(ManagerOptions{MaxAttempts: 2}, nil)
	identity, _ := testIdentity(t, 9)
	first := manager.Start(httptest.NewRequest(http.MethodGet, "http://proxy.local/one", nil), identity)
	second := manager.Start(httptest.NewRequest(http.MethodGet, "http://proxy.local/two", nil), identity)
	if first == nil || second != nil {
		t.Fatalf("duplicate retry ID was not rejected: first=%v second=%v", first, second)
	}
	first.Complete()
	if third := manager.Start(httptest.NewRequest(http.MethodGet, "http://proxy.local/three", nil), identity); third != nil {
		t.Fatal("recently terminal retry ID was accepted")
	}
}

func newCaptureRuntime(t *testing.T) (*module.Runtime, *event.Dispatcher, *recordoutput.Output) {
	t.Helper()
	output := recordoutput.New("memory")
	dispatcher, err := event.NewDispatcher(context.Background(), event.Config{Sink: output, QueueMaxRecords: 1024, QueueMaxBytes: 8 << 20})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := module.NewRuntime([]module.Definition{capture.New()}, dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	return runtime, dispatcher, output
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	value, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func testIdentity(t *testing.T, seed byte) (retry.Identity, [32]byte) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "http://proxy.local/", nil)
	retryID := fmt.Sprintf("01982d4f-7c2a-7%03x-8%03x-%012x", seed, seed, uint64(seed))
	request.Header.Set(retry.IDHeader, retryID)
	identity, err := retry.TakeIdentity(request)
	if err != nil {
		t.Fatal(err)
	}
	return identity, identity.Digest()
}
