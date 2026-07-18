package recoveryhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	compiler := New(Options{MaxResponseBytes: 1024})
	defer func() { _ = compiler.Close() }()
	controller, err := compiler.Compile(recovery.ControllerSpec{
		URL: server.URL, Headers: map[string]string{"Authorization": "Bearer secret"}, Timeout: "1s",
	})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := controller.Decide(context.Background(), recovery.Event{
		Protocol: recovery.Protocol, EventID: "event-1", TraceID: "trace-1",
		RoundTrip: recovery.RoundTripInfo{Number: 1, MaxRoundTrips: 3},
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != recovery.ActionRetry || decision.AfterMS != 25 || received.TraceID != "trace-1" ||
		received.Protocol != "dproxy.recovery.v2" || received.RoundTrip.Number != 1 {
		t.Fatalf("unexpected callback result: decision=%#v event=%#v", decision, received)
	}
}

func TestCompilerValidatesSpecAndClampsTimeout(t *testing.T) {
	compiler := New(Options{MaxTimeout: 250 * time.Millisecond})
	defer func() { _ = compiler.Close() }()
	if _, err := compiler.Compile(recovery.ControllerSpec{URL: "redis://control.example.com"}); err == nil {
		t.Fatal("invalid controller URL was accepted")
	}
	compiled, err := compiler.Compile(recovery.ControllerSpec{
		URL: "https://user:secret@control.example.com/recovery?tenant=a", Timeout: "2s",
	})
	if err != nil {
		t.Fatal(err)
	}
	observation := compiled.(recovery.ObservableControllerBinding).Observation()
	if observation.Timeout != 250*time.Millisecond || observation.Endpoint != "https://user:secret@control.example.com/recovery?tenant=a" {
		t.Fatalf("unexpected normalized controller config: %#v", observation)
	}
}

func TestControllerRejectsInvalidDecision(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"action":"unknown"}`))
	}))
	defer server.Close()
	compiler := New(Options{MaxResponseBytes: 1024})
	defer func() { _ = compiler.Close() }()
	controller, err := compiler.Compile(recovery.ControllerSpec{URL: server.URL, Timeout: "1s"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Decide(context.Background(), recovery.Event{}); err == nil {
		t.Fatal("invalid recovery decision was accepted")
	}
}
