package handler

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lwmacct/260628-directive-proxy/internal/core/proxyrequest"
)

type requestRetryTrackerStub struct {
	requestID string
	digest    [32]byte
	attempt   int
	trigger   proxyrequest.RetryTrigger
	err       error
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
func (s *requestRetryTrackerStub) RetryByCapability(requestID string, digest [32]byte, attempt int, trigger proxyrequest.RetryTrigger) (proxyrequest.RetryResult, error) {
	s.requestID = requestID
	s.digest = digest
	s.attempt = attempt
	s.trigger = trigger
	if s.err != nil {
		return proxyrequest.RetryResult{}, s.err
	}
	return proxyrequest.RetryResult{
		Request:     proxyrequest.ActiveRequest{TraceID: "0123456789abcdef0123456789abcdef", RequestID: requestID, State: proxyrequest.StateRetryRequested},
		NextAttempt: attempt + 1,
	}, nil
}

func TestPublicRequestRetryEndpointUsesCapabilityWithoutControlAuthentication(t *testing.T) {
	tracker := &requestRetryTrackerStub{}
	handler := NewPublicEndpoint(Services{Requests: tracker}).Handler()
	requestID := base64.RawURLEncoding.EncodeToString(make([]byte, 16))
	capability := base64.RawURLEncoding.EncodeToString(bytesOfValue(1, 32))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/public/proxy-requests/"+requestID+"/attempts/2", nil)
	request.Header.Set("Authorization", "DProxy-Retry "+requestID+"."+capability)
	request.Header.Set("If-Match", `"attempt:1"`)
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("unexpected response: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if tracker.requestID != requestID || tracker.digest == [32]byte{} || tracker.attempt != 1 || tracker.trigger != proxyrequest.RetryTriggerRequesterAPI {
		t.Fatalf("unexpected retry command: request_id=%q attempt=%d trigger=%s", tracker.requestID, tracker.attempt, tracker.trigger)
	}
}

func TestPublicRequestRetryEndpointReturnsStableErrorEnvelope(t *testing.T) {
	tracker := &requestRetryTrackerStub{err: proxyrequest.ErrAttemptChanged}
	handler := NewPublicEndpoint(Services{Requests: tracker}).Handler()
	requestID := base64.RawURLEncoding.EncodeToString(make([]byte, 16))
	capability := base64.RawURLEncoding.EncodeToString(bytesOfValue(1, 32))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/public/proxy-requests/"+requestID+"/attempts/2", strings.NewReader(""))
	request.Header.Set("Authorization", "DProxy-Retry "+requestID+"."+capability)
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
	requestID := base64.RawURLEncoding.EncodeToString(make([]byte, 16))
	capability := base64.RawURLEncoding.EncodeToString(bytesOfValue(1, 32))
	tests := []struct {
		name          string
		authorization string
		tracker       *requestRetryTrackerStub
	}{
		{name: "invalid proof", authorization: "DProxy-Retry invalid", tracker: &requestRetryTrackerStub{}},
		{name: "unknown request", authorization: "DProxy-Retry " + requestID + "." + capability, tracker: &requestRetryTrackerStub{err: proxyrequest.ErrNotFound}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewPublicEndpoint(Services{Requests: tt.tracker}).Handler()
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPut, "/api/public/proxy-requests/"+requestID+"/attempts/2", nil)
			request.Header.Set("Authorization", tt.authorization)
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

func bytesOfValue(value byte, size int) []byte {
	data := make([]byte, size)
	for index := range data {
		data[index] = value
	}
	return data
}
