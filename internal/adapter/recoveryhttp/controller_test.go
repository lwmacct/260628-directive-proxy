package recoveryhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/recovery"
)

func TestControllerSendsRecoveryEventAndReadsDecision(t *testing.T) {
	var received recovery.Event
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.Header.Get("Authorization") != "Bearer secret" ||
			request.Header.Get("Idempotency-Key") != "event-1" {
			t.Errorf("unexpected callback request: method=%s headers=%#v", request.Method, request.Header)
		}
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"action":"retry","after_ms":25}`))
	}))
	defer server.Close()
	controller := New(Options{MaxResponseBytes: 1024})
	defer func() { _ = controller.Close() }()
	callbackURL, _ := url.Parse(server.URL)
	decision, err := controller.Decide(context.Background(), recovery.ControllerSpec{
		URL: callbackURL, Headers: http.Header{"Authorization": {"Bearer secret"}}, Timeout: time.Second,
	}, recovery.Event{Protocol: recovery.Protocol, EventID: "event-1", TraceID: "trace-1"})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != recovery.ActionRetry || decision.AfterMS != 25 || received.TraceID != "trace-1" {
		t.Fatalf("unexpected callback result: decision=%#v event=%#v", decision, received)
	}
}

func TestControllerRejectsInvalidDecision(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"action":"unknown"}`))
	}))
	defer server.Close()
	controller := New(Options{MaxResponseBytes: 1024})
	defer func() { _ = controller.Close() }()
	callbackURL, _ := url.Parse(server.URL)
	if _, err := controller.Decide(context.Background(), recovery.ControllerSpec{URL: callbackURL, Timeout: time.Second}, recovery.Event{}); err == nil {
		t.Fatal("invalid recovery decision was accepted")
	}
}
