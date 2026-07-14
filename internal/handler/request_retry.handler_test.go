package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lwmacct/260628-directive-proxy/internal/core/proxyrequest"
	"github.com/lwmacct/260628-directive-proxy/internal/core/requestmeta"
)

type requestRetryTrackerStub struct {
	selector requestmeta.Selector
	attempt  int
	trigger  proxyrequest.RetryTrigger
	err      error
}

func (*requestRetryTrackerStub) Start(*http.Request) proxyrequest.Session { return nil }
func (*requestRetryTrackerStub) ListActive() []proxyrequest.ActiveRequest { return nil }
func (*requestRetryTrackerStub) GetActive(string) (proxyrequest.ActiveRequest, bool) {
	return proxyrequest.ActiveRequest{}, false
}
func (*requestRetryTrackerStub) RetryByTraceID(string, int, proxyrequest.RetryTrigger) (proxyrequest.RetryResult, error) {
	return proxyrequest.RetryResult{}, proxyrequest.ErrNotFound
}
func (s *requestRetryTrackerStub) RetryByMetadata(selector requestmeta.Selector, attempt int, trigger proxyrequest.RetryTrigger) (proxyrequest.RetryResult, error) {
	s.selector = selector
	s.attempt = attempt
	s.trigger = trigger
	if s.err != nil {
		return proxyrequest.RetryResult{}, s.err
	}
	return proxyrequest.RetryResult{
		Request:     proxyrequest.ActiveRequest{TraceID: "0123456789abcdef0123456789abcdef", State: proxyrequest.StateRetryRequested},
		NextAttempt: attempt + 1,
	}, nil
}

func TestPublicRequestRetryEndpointUsesMetadataWithoutAuthenticationContext(t *testing.T) {
	tracker := &requestRetryTrackerStub{}
	handler := NewPublicEndpoint(Services{Requests: tracker}).Handler()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/public/request-retries", strings.NewReader(`{"metadata":{"x-dproxy-request-id":"request-1"},"expected_attempt":1}`))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("unexpected response: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if tracker.selector["X-Dproxy-Request-Id"] != "request-1" || tracker.attempt != 1 || tracker.trigger != proxyrequest.RetryTriggerRequesterAPI {
		t.Fatalf("unexpected retry command: selector=%#v attempt=%d trigger=%s", tracker.selector, tracker.attempt, tracker.trigger)
	}
}

func TestPublicRequestRetryEndpointReturnsStableErrorEnvelope(t *testing.T) {
	tracker := &requestRetryTrackerStub{err: proxyrequest.ErrAmbiguous}
	handler := NewPublicEndpoint(Services{Requests: tracker}).Handler()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/public/request-retries", strings.NewReader(`{"metadata":{"X-Dproxy-Key":"shared"},"expected_attempt":1}`))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("unexpected response: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Error APIErrorBodyDTO `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "ambiguous_request" {
		t.Fatalf("unexpected error body: %s", recorder.Body.String())
	}
}
