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
	definition := New(Options{MaxResponseBytes: 1024})
	defer func() { _ = definition.Close() }()
	controller, err := definition.CompileController(json.RawMessage(`{"url":"` + server.URL + `","headers":{"Authorization":"Bearer secret"},"timeout":"1s"}`))
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

func TestDefinitionValidatesConfigAndClampsTimeout(t *testing.T) {
	definition := New(Options{MaxTimeout: 250 * time.Millisecond})
	defer func() { _ = definition.Close() }()
	if definition.Name() != "builtin.recovery" {
		t.Fatalf("unexpected recovery module name: %q", definition.Name())
	}
	if _, err := definition.CompileController(json.RawMessage(`{"url":"https://control.example.com","unknown":true}`)); err == nil {
		t.Fatal("unknown controller config field was accepted")
	}
	compiled, err := definition.CompileController(json.RawMessage(`{"url":"https://user:secret@control.example.com/recovery?tenant=a","timeout":"2s"}`))
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
	definition := New(Options{MaxResponseBytes: 1024})
	defer func() { _ = definition.Close() }()
	controller, err := definition.CompileController(json.RawMessage(`{"url":"` + server.URL + `","timeout":"1s"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Decide(context.Background(), recovery.Event{}); err == nil {
		t.Fatal("invalid recovery decision was accepted")
	}
}
