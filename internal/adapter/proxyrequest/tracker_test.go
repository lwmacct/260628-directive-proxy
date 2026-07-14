package proxyrequestadapter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/capture"
	"github.com/lwmacct/260628-directive-proxy/internal/core/proxyrequest"
)

type recordingSink struct {
	mu     sync.Mutex
	tags   []string
	events []capture.Event
}

func (s *recordingSink) Emit(tag string, event capture.Event) error {
	s.mu.Lock()
	s.tags = append(s.tags, tag)
	s.events = append(s.events, event)
	s.mu.Unlock()
	return nil
}

func (s *recordingSink) Close() error { return nil }
func (s *recordingSink) CaptureHealth() capture.HealthStatus {
	return capture.HealthStatus{Status: "ok"}
}

func TestProxyRequestLifecycleTracksRetryAndEmitsSSEEvents(t *testing.T) {
	sink := &recordingSink{}
	tracker := NewProxyRequestService(ProxyRequestOptions{
		RetryAfter:       0,
		MaxAttempts:      3,
		InstanceID:       "test-instance",
		BodyChunkBytes:   4,
		MaxSSEEventBytes: 1024,
		RedactHeaders:    []string{"authorization"},
		RedactQuery:      []string{"token"},
	}, sink)
	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/chat?token=secret", nil)
	req.Header.Set("Authorization", "Bearer secret")
	session := tracker.Start(req)
	if len(session.TraceID()) != 32 {
		t.Fatalf("unexpected trace ID: %q", session.TraceID())
	}
	target, _ := url.Parse("https://upstream.example/v1/chat?token=upstream-secret")
	session.SetTargetURL(target)
	session.SetDirective("inline", "", "", "", time.Millisecond)
	session.RequestBodyChunk([]byte("hello"), 0)
	session.RequestBodyEnd(5, "digest", true)

	canceled := false
	attempt := session.BeginAttempt(req, func() { canceled = true })
	active := tracker.ListActive()
	if len(active) != 1 || active[0].Attempt != 1 || active[0].TargetURL != "https://upstream.example/v1/chat?token=%3Credacted%3E" {
		t.Fatalf("unexpected active request: %#v", active)
	}
	result, err := tracker.Retry(session.TraceID(), attempt)
	if err != nil || result.NextAttempt != 2 || !canceled {
		t.Fatalf("retry was not accepted: result=%#v canceled=%t err=%v", result, canceled, err)
	}
	if action := session.FinishAttempt(attempt, false, context.Canceled); action != proxyrequest.AttemptRetry {
		t.Fatalf("unexpected attempt action: %v", action)
	}
	attempt = session.BeginAttempt(req, func() {})
	if attempt != 2 {
		t.Fatalf("unexpected second attempt: %d", attempt)
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
	sink.mu.Lock()
	defer sink.mu.Unlock()
	var sawSSE, sawComment, sawRedacted bool
	var previous uint64
	for _, event := range sink.events {
		if event.Sequence <= previous {
			t.Fatalf("capture sequence is not increasing: %#v", sink.events)
		}
		previous = event.Sequence
		switch event.Kind {
		case "request.headers":
			headers := event.Data["headers"].(map[string][]string)
			sawRedacted = headers["Authorization"][0] == "<redacted>"
		case "response.sse.event":
			sawSSE = event.Data["data"] == "one\ntwo" && event.Data["upstream_event_id"] == "9"
		case "response.sse.comment":
			sawComment = true
		}
	}
	if !sawSSE || !sawComment || !sawRedacted {
		t.Fatalf("missing capture events: sse=%t comment=%t redacted=%t events=%#v", sawSSE, sawComment, sawRedacted, sink.events)
	}
}

func TestProxyRequestRetryRejectsEarlyAndStaleAttempts(t *testing.T) {
	tracker := NewProxyRequestService(ProxyRequestOptions{RetryAfter: time.Hour, MaxAttempts: 2}, capture.DiscardSink{})
	session := tracker.Start(httptest.NewRequest(http.MethodGet, "http://proxy.local/", nil))
	attempt := session.BeginAttempt(nil, func() {})
	if _, err := tracker.Retry(session.TraceID(), attempt); err != proxyrequest.ErrRetryNotReady {
		t.Fatalf("unexpected early retry error: %v", err)
	}
	if _, err := tracker.Retry(session.TraceID(), attempt+1); err != proxyrequest.ErrAttemptChanged {
		t.Fatalf("unexpected stale attempt error: %v", err)
	}
	session.Complete()
}

func TestProxyRequestTrackerBoundsActiveRequests(t *testing.T) {
	tracker := NewProxyRequestService(ProxyRequestOptions{MaxAttempts: 2, MaxActiveRequests: 1}, capture.DiscardSink{})
	first := tracker.Start(httptest.NewRequest(http.MethodGet, "http://proxy.local/one", nil))
	second := tracker.Start(httptest.NewRequest(http.MethodGet, "http://proxy.local/two", nil))
	if attempt := first.BeginAttempt(nil, func() {}); attempt != 1 {
		t.Fatalf("unexpected first attempt: %d", attempt)
	}
	if attempt := second.BeginAttempt(nil, func() {}); attempt != 0 {
		t.Fatalf("active request capacity was not enforced: %d", attempt)
	}
	first.Complete()
	second.Complete()
}
