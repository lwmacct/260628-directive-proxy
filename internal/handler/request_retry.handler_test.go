package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lwmacct/260628-directive-proxy/internal/core/proxyrequest"
)

type requestRetryTrackerStub struct {
	digest  [32]byte
	attempt int
	trigger proxyrequest.RetryTrigger
	err     error
}

func (*requestRetryTrackerStub) Start(*http.Request, proxyrequest.Identity) proxyrequest.Session {
	return nil
}
func (*requestRetryTrackerStub) ListActive() []proxyrequest.ActiveRequest { return nil }
func (*requestRetryTrackerStub) GetActive(string) (proxyrequest.ActiveRequest, bool) {
	return proxyrequest.ActiveRequest{}, false
}
func (*requestRetryTrackerStub) RetryByTraceID(string, int, proxyrequest.RetryTrigger) (proxyrequest.RetryResult, error) {
	return proxyrequest.RetryResult{}, proxyrequest.ErrNotFound
}
func (s *requestRetryTrackerStub) RetryByRetryID(digest [32]byte, attempt int, trigger proxyrequest.RetryTrigger) (proxyrequest.RetryResult, error) {
	s.digest = digest
	s.attempt = attempt
	s.trigger = trigger
	if s.err != nil {
		return proxyrequest.RetryResult{}, s.err
	}
	return proxyrequest.RetryResult{
		Request:     proxyrequest.ActiveRequest{TraceID: "01982d4f-7c2a-7abc-9d43-1a2b3c4d5e6f", HasRetryID: true, State: proxyrequest.StateRetryRequested},
		NextAttempt: attempt + 1,
	}, nil
}

func TestPublicRequestRetryEndpointUsesCapabilityWithoutAdminAuthentication(t *testing.T) {
	tracker := &requestRetryTrackerStub{}
	handler := NewPublicEndpoint(Services{Requests: tracker}).Handler()
	retryID := "01982d4f-7c2a-7abc-9d43-1a2b3c4d5e6f"
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/public/retry", nil)
	request.Header.Set("Dproxy-Retry-ID", retryID)
	request.Header.Set("If-Match", `"attempt:1"`)
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("unexpected response: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if tracker.digest == [32]byte{} || tracker.attempt != 1 || tracker.trigger != proxyrequest.RetryTriggerRequesterAPI {
		t.Fatalf("unexpected retry command: attempt=%d trigger=%s", tracker.attempt, tracker.trigger)
	}
}

func TestPublicRequestRetryEndpointReturnsStableErrorEnvelope(t *testing.T) {
	tracker := &requestRetryTrackerStub{err: proxyrequest.ErrAttemptChanged}
	handler := NewPublicEndpoint(Services{Requests: tracker}).Handler()
	retryID := "01982d4f-7c2a-7abc-9d43-1a2b3c4d5e6f"
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/public/retry", nil)
	request.Header.Set("Dproxy-Retry-ID", retryID)
	request.Header.Set("If-Match", `"attempt:1"`)
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
	if body.Error.Code != "attempt_changed" {
		t.Fatalf("unexpected error body: %s", recorder.Body.String())
	}
}

func TestPublicRequestRetryHidesInvalidProofAndUnknownRequest(t *testing.T) {
	retryID := "01982d4f-7c2a-7abc-9d43-1a2b3c4d5e6f"
	tests := []struct {
		name          string
		authorization string
		tracker       *requestRetryTrackerStub
	}{
		{name: "invalid proof", authorization: "", tracker: &requestRetryTrackerStub{}},
		{name: "unknown request", authorization: retryID, tracker: &requestRetryTrackerStub{err: proxyrequest.ErrNotFound}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewPublicEndpoint(Services{Requests: tt.tracker}).Handler()
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPut, "/api/public/retry", nil)
			request.Header.Set("Dproxy-Retry-ID", tt.authorization)
			request.Header.Set("If-Match", `"attempt:1"`)
			handler.ServeHTTP(recorder, request)
			var body struct {
				Error APIErrorBodyDTO `json:"error"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if recorder.Code != http.StatusNotFound || body.Error.Code != "proxy_request_not_found" {
				t.Fatalf("unexpected hidden lookup response: status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}
