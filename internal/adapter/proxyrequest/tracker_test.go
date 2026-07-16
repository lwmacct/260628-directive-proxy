package proxyrequestadapter

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
	"github.com/lwmacct/260628-directive-proxy/internal/core/observability"
	"github.com/lwmacct/260628-directive-proxy/internal/core/proxyrequest"
	"github.com/lwmacct/260628-directive-proxy/internal/core/requestmeta"
	"github.com/lwmacct/260628-directive-proxy/internal/modules/capture"
	recordoutput "github.com/lwmacct/260628-directive-proxy/internal/testutil/recordoutput"
)

func TestProxyRequestLifecycleTracksRetryAndEmitsSSEEvents(t *testing.T) {
	engine, output := newCaptureEngine(t)
	captureSpec := []byte(`{"body-chunk-bytes":4,"redact-headers":["authorization"],"redact-query":["token"]}`)
	tracker := NewProxyRequestService(ProxyRequestOptions{
		MaxAttempts: 3,
	}, engine)
	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/chat?token=secret", nil)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Idempotency-Key", "lifecycle-test")
	session := tracker.Start(req, proxyrequest.Identity{})
	if err := session.ConfigureRequest([]module.Spec{{ID: "capture", Module: capture.Name, Config: captureSpec}}); err != nil {
		t.Fatal(err)
	}
	if _, err := proxyrequest.ParseRetryID(session.TraceID()); err != nil {
		t.Fatalf("unexpected trace ID: %q", session.TraceID())
	}
	target, _ := url.Parse("https://upstream.example/v1/chat?token=upstream-secret")
	session.RequestBodyEnd(5, "digest", true)

	canceled := false
	attempt := session.BeginAttempt(func() { canceled = true }, "inline", "", "", "")
	if err := session.ConfigureAttempt(attempt, nil); err != nil {
		t.Fatal(err)
	}
	session.BindMetadata(attempt, requestmeta.Metadata{"X-Dproxy-Request-Id": {"request-1"}})
	session.DirectiveResolved(attempt, target, time.Millisecond, "", false, false)
	if !session.BeginUpstream(attempt, req) {
		t.Fatal("first attempt did not enter upstream state")
	}
	active := tracker.ListActive()
	if len(active) != 1 || active[0].Attempt != 1 || active[0].TargetURL != "https://upstream.example/v1/chat?token=%3Credacted%3E" {
		t.Fatalf("unexpected active request: %#v", active)
	}
	result, err := tracker.RetryByTraceID(session.TraceID(), attempt, proxyrequest.RetryTriggerAdminAPI)
	if err != nil || result.NextAttempt != 2 || !canceled {
		t.Fatalf("retry was not accepted: result=%#v canceled=%t err=%v", result, canceled, err)
	}
	if action := session.FinishAttempt(attempt, false, context.Canceled); action != proxyrequest.AttemptRetry {
		t.Fatalf("unexpected attempt action: %v", action)
	}
	attempt = session.BeginAttempt(func() {}, "inline", "", "", "")
	if err := session.ConfigureAttempt(attempt, nil); err != nil {
		t.Fatal(err)
	}
	if attempt != 2 {
		t.Fatalf("unexpected second attempt: %d", attempt)
	}
	if !session.BindMetadata(attempt, requestmeta.Metadata{"X-Dproxy-Request-Id": {"changed"}}) {
		t.Fatal("metadata change was not detected")
	}
	session.DirectiveResolved(attempt, target, time.Millisecond, "", false, false)
	if !session.BeginUpstream(attempt, req) {
		t.Fatal("second attempt did not enter upstream state")
	}
	if action := session.FinishAttempt(attempt, true, nil); action != proxyrequest.AttemptReturn {
		t.Fatalf("unexpected response attempt action: %v", action)
	}
	if len(tracker.ListActive()) != 0 {
		t.Fatal("request remained retryable after response headers")
	}

	recorder := httptest.NewRecorder()
	wrapped := session.WrapResponseWriter(recorder)
	wrapped.Header().Set("Content-Type", "text/event-stream")
	wrapped.WriteHeader(http.StatusOK)
	_, _ = wrapped.Write([]byte(": ping\n\nid: 9\nevent: delta\ndata: one\ndata: two\n\n"))
	session.Complete()

	if recorder.Header().Get("X-Dproxy-Trace-ID") != session.TraceID() {
		t.Fatalf("tracking response header missing: %#v", recorder.Header())
	}
	if err := engine.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	events := output.Records()
	var sawSSE, sawComment, sawResolveStarted, sawResolveFinished, sawUpstreamStarted, sawMetadata bool
	var previous uint64
	for _, event := range events {
		if event.Sequence <= previous {
			t.Fatalf("capture sequence is not increasing: %#v", events)
		}
		previous = event.Sequence
		switch event.Topic {
		case "capture.directive.resolve.started":
			sawResolveStarted = event.Attempt > 0
		case "capture.directive.resolve.finished":
			sawResolveFinished = event.Data["target_url"] == "https://upstream.example/v1/chat?token=%3Credacted%3E"
		case "capture.attempt.upstream.started":
			sawUpstreamStarted = event.Attempt > 0
		case "capture.request.metadata.bound":
			sawMetadata = event.Data["metadata"] != nil
		case "capture.response.sse.event":
			sawSSE = event.Data["data"] == "one\ntwo" && event.Data["upstream_event_id"] == "9"
		case "capture.response.sse.comment":
			sawComment = true
		}
	}
	if !sawSSE || !sawComment || !sawResolveFinished || !sawUpstreamStarted || !sawMetadata {
		t.Fatalf("missing capture events: sse=%t comment=%t resolve_started=%t resolve_finished=%t upstream_started=%t metadata=%t events=%#v", sawSSE, sawComment, sawResolveStarted, sawResolveFinished, sawUpstreamStarted, sawMetadata, events)
	}
}

func TestProxyRequestRetryByRetryIDUsesProofAndCAS(t *testing.T) {
	engine, output := newCaptureEngine(t)
	tracker := NewProxyRequestService(ProxyRequestOptions{MaxAttempts: 3}, engine)
	newActive := func(path string, seed byte, canceled *bool) (proxyrequest.Session, proxyrequest.Identity, [32]byte) {
		identity, digest := testIdentity(t, seed)
		session := tracker.Start(httptest.NewRequest(http.MethodGet, "http://proxy.local/"+path, nil), identity)
		if err := session.ConfigureRequest([]module.Spec{{ID: "capture", Module: capture.Name, Config: []byte(`{}`)}}); err != nil {
			t.Fatal(err)
		}
		attempt := session.BeginAttempt(func() { *canceled = true }, "inline", "", "", "")
		if err := session.ConfigureAttempt(attempt, nil); err != nil {
			t.Fatal(err)
		}
		session.DirectiveResolved(attempt, mustURL(t, "https://upstream.example"), 0, "", false, false)
		if !session.BeginUpstream(attempt, nil) {
			t.Fatal("attempt did not enter upstream state")
		}
		return session, identity, digest
	}
	var firstCanceled, secondCanceled bool
	first, _, _ := newActive("one", 1, &firstCanceled)
	second, _, secondDigest := newActive("two", 2, &secondCanceled)
	if _, err := tracker.RetryByRetryID([32]byte{}, 1, proxyrequest.RetryTriggerRequesterAPI); err != proxyrequest.ErrNotFound {
		t.Fatalf("invalid proof was accepted: %v", err)
	}
	result, err := tracker.RetryByRetryID(secondDigest, 1, proxyrequest.RetryTriggerRequesterAPI)
	if err != nil || result.Request.TraceID != second.TraceID() || !secondCanceled || firstCanceled {
		t.Fatalf("unexpected retry ID retry: result=%#v err=%v first_canceled=%t second_canceled=%t", result, err, firstCanceled, secondCanceled)
	}
	if repeated, err := tracker.RetryByRetryID(secondDigest, 1, proxyrequest.RetryTriggerRequesterAPI); err != nil || repeated.NextAttempt != 2 {
		t.Fatalf("duplicate retry was not idempotent: result=%#v err=%v", repeated, err)
	}
	first.Complete()
	second.Complete()
	repeatedAfterCompletion, err := tracker.RetryByRetryID(secondDigest, 1, proxyrequest.RetryTriggerRequesterAPI)
	if err != nil || repeatedAfterCompletion.NextAttempt != 2 {
		t.Fatalf("completed retry command tombstone was not idempotent: result=%#v err=%v", repeatedAfterCompletion, err)
	}
	if err := engine.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	sawRequesterRetry := false
	for _, event := range output.Records() {
		if event.Topic == "capture.retry.requested" && event.Data["trigger"] == string(proxyrequest.RetryTriggerRequesterAPI) {
			sawRequesterRetry = true
		}
	}
	if !sawRequesterRetry {
		t.Fatal("requester retry was not captured")
	}
}

func TestProxyRequestMetadataCaptureUsesHeaderRedactionPolicy(t *testing.T) {
	engine, output := newCaptureEngine(t)
	tracker := NewProxyRequestService(ProxyRequestOptions{
		MaxAttempts: 2,
	}, engine)
	session := tracker.Start(httptest.NewRequest(http.MethodGet, "http://proxy.local/", nil), proxyrequest.Identity{})
	if err := session.ConfigureRequest([]module.Spec{{ID: "capture", Module: capture.Name, Config: []byte(`{"redact-headers":["x-dproxy-secret-*"],"redact-query":[]}`)}}); err != nil {
		t.Fatal(err)
	}
	attempt := session.BeginAttempt(func() {}, "inline", "", "", "")
	if err := session.ConfigureAttempt(attempt, nil); err != nil {
		t.Fatal(err)
	}
	session.BindMetadata(attempt, requestmeta.Metadata{
		"X-Dproxy-Request-Id": {"request-1"},
		"X-Dproxy-Secret-Key": {"secret"},
	})
	session.Complete()
	if err := engine.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, event := range output.Records() {
		if event.Topic != "capture.request.metadata.bound" {
			continue
		}
		metadata := event.Data["metadata"].(map[string][]string)
		if metadata["X-Dproxy-Secret-Key"][0] != "<redacted>" || metadata["X-Dproxy-Request-Id"][0] != "request-1" {
			t.Fatalf("unexpected captured metadata: %#v", metadata)
		}
		return
	}
	t.Fatal("metadata capture event was not emitted")
}

func TestModuleEngineRejectsUnknownOrInvalidRequestModules(t *testing.T) {
	engine, _ := newCaptureEngine(t)
	tracker := NewProxyRequestService(ProxyRequestOptions{MaxAttempts: 2}, engine)
	for _, specs := range [][]module.Spec{
		{{ID: "missing", Module: "missing.module", Config: []byte(`{}`)}},
		{{ID: "capture", Module: capture.Name, Config: []byte(`{"body-chunk-bytes":-1}`)}},
	} {
		session := tracker.Start(httptest.NewRequest(http.MethodGet, "http://proxy.local/", nil), proxyrequest.Identity{})
		if err := session.ConfigureRequest(specs); err == nil {
			t.Fatalf("invalid request module was accepted: %#v", specs)
		}
		session.Complete()
	}
	if err := engine.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestProxyRequestRetryRejectsEarlyAndStaleAttempts(t *testing.T) {
	tracker := NewProxyRequestService(ProxyRequestOptions{MaxAttempts: 2}, nil)
	session := tracker.Start(httptest.NewRequest(http.MethodGet, "http://proxy.local/", nil), proxyrequest.Identity{})
	attempt := session.BeginAttempt(func() {}, "inline", "", "", "")
	if active, ok := tracker.GetActive(session.TraceID()); !ok || active.State != proxyrequest.StateResolvingDirective {
		t.Fatalf("unexpected resolving state: active=%#v ok=%t", active, ok)
	}
	if _, err := tracker.RetryByTraceID(session.TraceID(), attempt, proxyrequest.RetryTriggerAdminAPI); err != proxyrequest.ErrRetryNotReady {
		t.Fatalf("resolving attempt was retryable: %v", err)
	}
	session.DirectiveResolved(attempt, mustURL(t, "https://upstream.example"), 0, "", false, false)
	session.BeginUpstream(attempt, nil)
	if _, err := tracker.RetryByTraceID(session.TraceID(), attempt, proxyrequest.RetryTriggerAdminAPI); err != nil {
		t.Fatalf("retry after upstream start failed: %v", err)
	}
	if _, err := tracker.RetryByTraceID(session.TraceID(), attempt+1, proxyrequest.RetryTriggerAdminAPI); err != proxyrequest.ErrAttemptChanged {
		t.Fatalf("unexpected stale attempt error: %v", err)
	}
	session.Complete()
}

func TestProxyRequestRetryRequiresIdempotencyKeyForPostAndPatch(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			tracker := NewProxyRequestService(ProxyRequestOptions{MaxAttempts: 2}, nil)
			session := tracker.Start(httptest.NewRequest(method, "http://proxy.local/resource", nil), proxyrequest.Identity{})
			attempt := session.BeginAttempt(func() {}, "inline", "", "", "")
			session.DirectiveResolved(attempt, mustURL(t, "https://upstream.example"), 0, "", false, false)
			if !session.BeginUpstream(attempt, nil) {
				t.Fatal("attempt did not enter upstream state")
			}
			if _, err := tracker.RetryByTraceID(session.TraceID(), attempt, proxyrequest.RetryTriggerAdminAPI); err != proxyrequest.ErrIdempotencyKeyRequired {
				t.Fatalf("unexpected retry result: %v", err)
			}
			session.Complete()
		})
	}
}

func TestProxyRequestTrackerRejectsDuplicateRetryID(t *testing.T) {
	tracker := NewProxyRequestService(ProxyRequestOptions{MaxAttempts: 2}, nil)
	identity, _ := testIdentity(t, 9)
	first := tracker.Start(httptest.NewRequest(http.MethodGet, "http://proxy.local/one", nil), identity)
	second := tracker.Start(httptest.NewRequest(http.MethodGet, "http://proxy.local/two", nil), identity)
	if first == nil || second != nil {
		t.Fatalf("duplicate retry ID was not rejected: first=%v second=%v", first, second)
	}
	first.Complete()
	third := tracker.Start(httptest.NewRequest(http.MethodGet, "http://proxy.local/three", nil), identity)
	if third != nil {
		t.Fatal("recently terminal retry ID was accepted")
	}
}

func newCaptureEngine(t *testing.T) (*observability.Engine, *recordoutput.Output) {
	t.Helper()
	output := recordoutput.New("memory")
	engine, err := observability.NewEngine(context.Background(), []module.Definition{capture.New()}, observability.SinkConfig{Sink: output, QueueMaxRecords: 1024, QueueMaxBytes: 8 << 20})
	if err != nil {
		t.Fatal(err)
	}
	return engine, output
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	value, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func testIdentity(t *testing.T, seed byte) (proxyrequest.Identity, [32]byte) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "http://proxy.local/", nil)
	retryID := fmt.Sprintf("01982d4f-7c2a-7%03x-8%03x-%012x", seed, seed, uint64(seed))
	request.Header.Set(proxyrequest.RetryIDHeader, retryID)
	identity, err := proxyrequest.TakeIdentity(request)
	if err != nil {
		t.Fatal(err)
	}
	digest := identity.Digest()
	return identity, digest
}
