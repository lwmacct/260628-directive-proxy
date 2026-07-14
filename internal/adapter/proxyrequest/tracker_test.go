package proxyrequestadapter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/observability"
	"github.com/lwmacct/260628-directive-proxy/internal/core/proxyrequest"
	"github.com/lwmacct/260628-directive-proxy/internal/core/requestmeta"
	captureplugin "github.com/lwmacct/260628-directive-proxy/internal/plugin/capture"
	recordoutput "github.com/lwmacct/260628-directive-proxy/internal/testutil/recordoutput"
)

func TestProxyRequestLifecycleTracksRetryAndEmitsSSEEvents(t *testing.T) {
	pipeline, output := newCapturePipeline(t, captureplugin.Config{
		BodyChunkBytes: 4, MaxSSEEventBytes: 1024, RedactHeaders: []string{"authorization"}, RedactQuery: []string{"token"},
	})
	tracker := NewProxyRequestService(ProxyRequestOptions{
		RetryAfter: 0, MaxAttempts: 3, InstanceID: "test-instance",
	}, pipeline)
	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/chat?token=secret", nil)
	req.Header.Set("Authorization", "Bearer secret")
	session := tracker.Start(req)
	if len(session.TraceID()) != 32 {
		t.Fatalf("unexpected trace ID: %q", session.TraceID())
	}
	target, _ := url.Parse("https://upstream.example/v1/chat?token=upstream-secret")
	session.RequestBodyChunk([]byte("hello"), 0)
	session.RequestBodyEnd(5, "digest", true)

	canceled := false
	attempt := session.BeginAttempt(func() { canceled = true }, "inline", "", "", "")
	session.BindMetadata(attempt, requestmeta.Metadata{"X-Dproxy-Request-Id": {"request-1"}})
	session.DirectiveResolved(attempt, target, time.Millisecond, "", false, false)
	if !session.BeginUpstream(attempt, req) {
		t.Fatal("first attempt did not enter upstream state")
	}
	active := tracker.ListActive()
	if len(active) != 1 || active[0].Attempt != 1 || active[0].TargetURL != "https://upstream.example/v1/chat?token=%3Credacted%3E" {
		t.Fatalf("unexpected active request: %#v", active)
	}
	result, err := tracker.RetryByTraceID(session.TraceID(), attempt, proxyrequest.RetryTriggerControlAPI)
	if err != nil || result.NextAttempt != 2 || !canceled {
		t.Fatalf("retry was not accepted: result=%#v canceled=%t err=%v", result, canceled, err)
	}
	if action := session.FinishAttempt(attempt, false, context.Canceled); action != proxyrequest.AttemptRetry {
		t.Fatalf("unexpected attempt action: %v", action)
	}
	attempt = session.BeginAttempt(func() {}, "inline", "", "", "")
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
	if err := pipeline.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	events := output.Records()
	var sawSSE, sawComment, sawRedacted, sawResolveStarted, sawResolveFinished, sawUpstreamStarted, sawMetadata bool
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
		case "capture.request.headers":
			headers := event.Data["headers"].(map[string][]string)
			sawRedacted = headers["Authorization"][0] == "<redacted>"
		case "capture.response.sse.event":
			sawSSE = event.Data["data"] == "one\ntwo" && event.Data["upstream_event_id"] == "9"
		case "capture.response.sse.comment":
			sawComment = true
		}
	}
	if !sawSSE || !sawComment || !sawRedacted || !sawResolveStarted || !sawResolveFinished || !sawUpstreamStarted || !sawMetadata {
		t.Fatalf("missing capture events: sse=%t comment=%t redacted=%t resolve_started=%t resolve_finished=%t upstream_started=%t metadata=%t events=%#v", sawSSE, sawComment, sawRedacted, sawResolveStarted, sawResolveFinished, sawUpstreamStarted, sawMetadata, events)
	}
}

func TestProxyRequestRetryByMetadataRequiresUniqueMatchAndUsesCAS(t *testing.T) {
	pipeline, output := newCapturePipeline(t, captureplugin.Config{})
	tracker := NewProxyRequestService(ProxyRequestOptions{RetryAfter: 0, MaxAttempts: 3}, pipeline)
	newActive := func(path, requestID, tenant string, canceled *bool) proxyrequest.Session {
		session := tracker.Start(httptest.NewRequest(http.MethodGet, "http://proxy.local/"+path, nil))
		attempt := session.BeginAttempt(func() { *canceled = true }, "inline", "", "", "")
		session.BindMetadata(attempt, requestmeta.Metadata{
			"X-Dproxy-Request-Id": {requestID},
			"X-Dproxy-Tenant":     {tenant},
		})
		session.DirectiveResolved(attempt, mustURL(t, "https://upstream.example"), 0, "", false, false)
		if !session.BeginUpstream(attempt, nil) {
			t.Fatal("attempt did not enter upstream state")
		}
		return session
	}
	var firstCanceled, secondCanceled bool
	first := newActive("one", "shared", "one", &firstCanceled)
	second := newActive("two", "shared", "two", &secondCanceled)
	shared, _ := requestmeta.NormalizeSelector(map[string]string{"X-Dproxy-Request-ID": "shared"})
	if _, err := tracker.RetryByMetadata(shared, 1, proxyrequest.RetryTriggerRequesterAPI); err != proxyrequest.ErrAmbiguous {
		t.Fatalf("unexpected ambiguous retry error: %v", err)
	}
	unique, _ := requestmeta.NormalizeSelector(map[string]string{
		"X-Dproxy-Request-ID": "shared",
		"X-Dproxy-Tenant":     "two",
	})
	result, err := tracker.RetryByMetadata(unique, 1, proxyrequest.RetryTriggerRequesterAPI)
	if err != nil || result.Request.TraceID != second.TraceID() || !secondCanceled || firstCanceled {
		t.Fatalf("unexpected metadata retry: result=%#v err=%v first_canceled=%t second_canceled=%t", result, err, firstCanceled, secondCanceled)
	}
	if _, err := tracker.RetryByMetadata(unique, 1, proxyrequest.RetryTriggerRequesterAPI); err != proxyrequest.ErrRetryInProgress {
		t.Fatalf("duplicate retry was not rejected: %v", err)
	}
	first.Complete()
	second.Complete()
	if err := pipeline.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	sawSelectorCapture := false
	for _, event := range output.Records() {
		if event.Topic != "capture.retry.requested" || event.Data["trigger"] != string(proxyrequest.RetryTriggerRequesterAPI) {
			continue
		}
		selector := event.Data["selector_metadata"].(map[string][]string)
		sawSelectorCapture = selector["X-Dproxy-Request-Id"][0] == "shared" && selector["X-Dproxy-Tenant"][0] == "two"
	}
	if !sawSelectorCapture {
		t.Fatal("requester retry selector was not captured")
	}
}

func TestProxyRequestMetadataCaptureUsesHeaderRedactionPolicy(t *testing.T) {
	pipeline, output := newCapturePipeline(t, captureplugin.Config{RedactHeaders: []string{"x-dproxy-secret-*"}})
	tracker := NewProxyRequestService(ProxyRequestOptions{
		MaxAttempts: 2,
	}, pipeline)
	session := tracker.Start(httptest.NewRequest(http.MethodGet, "http://proxy.local/", nil))
	attempt := session.BeginAttempt(func() {}, "inline", "", "", "")
	session.BindMetadata(attempt, requestmeta.Metadata{
		"X-Dproxy-Request-Id": {"request-1"},
		"X-Dproxy-Secret-Key": {"secret"},
	})
	session.Complete()
	if err := pipeline.Close(context.Background()); err != nil {
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

func TestProxyRequestRetryRejectsEarlyAndStaleAttempts(t *testing.T) {
	tracker := NewProxyRequestService(ProxyRequestOptions{RetryAfter: time.Hour, MaxAttempts: 2}, nil)
	session := tracker.Start(httptest.NewRequest(http.MethodGet, "http://proxy.local/", nil))
	attempt := session.BeginAttempt(func() {}, "inline", "", "", "")
	if active, ok := tracker.GetActive(session.TraceID()); !ok || active.State != proxyrequest.StateResolvingDirective || !active.RetryableAt.IsZero() {
		t.Fatalf("unexpected resolving state: active=%#v ok=%t", active, ok)
	}
	if _, err := tracker.RetryByTraceID(session.TraceID(), attempt, proxyrequest.RetryTriggerControlAPI); err != proxyrequest.ErrRetryNotReady {
		t.Fatalf("resolving attempt was retryable: %v", err)
	}
	session.DirectiveResolved(attempt, mustURL(t, "https://upstream.example"), 0, "", false, false)
	session.BeginUpstream(attempt, nil)
	if _, err := tracker.RetryByTraceID(session.TraceID(), attempt, proxyrequest.RetryTriggerControlAPI); err != proxyrequest.ErrRetryNotReady {
		t.Fatalf("unexpected early retry error: %v", err)
	}
	if _, err := tracker.RetryByTraceID(session.TraceID(), attempt+1, proxyrequest.RetryTriggerControlAPI); err != proxyrequest.ErrAttemptChanged {
		t.Fatalf("unexpected stale attempt error: %v", err)
	}
	session.Complete()
}

func TestProxyRequestTrackerBoundsActiveRequests(t *testing.T) {
	tracker := NewProxyRequestService(ProxyRequestOptions{MaxAttempts: 2, MaxActiveRequests: 1}, nil)
	first := tracker.Start(httptest.NewRequest(http.MethodGet, "http://proxy.local/one", nil))
	second := tracker.Start(httptest.NewRequest(http.MethodGet, "http://proxy.local/two", nil))
	if attempt := first.BeginAttempt(func() {}, "inline", "", "", ""); attempt != 1 {
		t.Fatalf("unexpected first attempt: %d", attempt)
	}
	if attempt := second.BeginAttempt(func() {}, "inline", "", "", ""); attempt != 0 {
		t.Fatalf("active request capacity was not enforced: %d", attempt)
	}
	first.Complete()
	second.Complete()
}

func newCapturePipeline(t *testing.T, config captureplugin.Config) (*observability.Pipeline, *recordoutput.Output) {
	t.Helper()
	output := recordoutput.New("memory")
	pipeline, err := observability.NewPipeline(context.Background(), []observability.Plugin{captureplugin.New(config)}, []observability.OutputBinding{{
		Output: output, Routes: []string{"**"}, QueueCapacity: 1024, QueueMaxBytes: 8 << 20,
	}})
	if err != nil {
		t.Fatal(err)
	}
	return pipeline, output
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	value, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
