package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lwmacct/260628-directive-proxy/internal/core/exchange"
)

type requestRetryCommandsStub struct {
	digest  [32]byte
	attempt int
	trigger exchange.Trigger
	err     error
}

func (*requestRetryCommandsStub) RetryByTraceID(string, int, exchange.Trigger) (exchange.RetryResult, error) {
	return exchange.RetryResult{}, exchange.ErrNotFound
}
func (s *requestRetryCommandsStub) RetryByRetryID(digest [32]byte, attempt int, trigger exchange.Trigger) (exchange.RetryResult, error) {
	s.digest = digest
	s.attempt = attempt
	s.trigger = trigger
	if s.err != nil {
		return exchange.RetryResult{}, s.err
	}
	return exchange.RetryResult{
		Exchange:    exchange.Snapshot{TraceID: "01982d4f-7c2a-7abc-9d43-1a2b3c4d5e6f", HasRetryID: true, Phase: exchange.PhaseRetryRequested},
		NextAttempt: attempt + 1,
	}, nil
}

func TestPublicRequestRetryEndpointUsesCapabilityWithoutAdminAuthentication(t *testing.T) {
	commands := &requestRetryCommandsStub{}
	handler := NewPublicEndpoint(Services{ExchangeCommands: commands}).Handler()
	retryID := "01982d4f-7c2a-7abc-9d43-1a2b3c4d5e6f"
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/public/retry", nil)
	request.Header.Set("Dproxy-Retry-ID", retryID)
	request.Header.Set("If-Match", `"attempt:1"`)
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("unexpected response: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if commands.digest == [32]byte{} || commands.attempt != 1 || commands.trigger != exchange.TriggerRequesterAPI {
		t.Fatalf("unexpected retry command: attempt=%d trigger=%s", commands.attempt, commands.trigger)
	}
}

func TestPublicRequestRetryEndpointReturnsStableErrorEnvelope(t *testing.T) {
	commands := &requestRetryCommandsStub{err: exchange.ErrAttemptChanged}
	handler := NewPublicEndpoint(Services{ExchangeCommands: commands}).Handler()
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
		commands      *requestRetryCommandsStub
	}{
		{name: "invalid proof", authorization: "", commands: &requestRetryCommandsStub{}},
		{name: "unknown request", authorization: retryID, commands: &requestRetryCommandsStub{err: exchange.ErrNotFound}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewPublicEndpoint(Services{ExchangeCommands: tt.commands}).Handler()
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
