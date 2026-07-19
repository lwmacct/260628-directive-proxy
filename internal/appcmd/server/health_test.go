package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/bodystore"
	"github.com/lwmacct/260628-directive-proxy/internal/core/event"
	"github.com/lwmacct/260628-directive-proxy/internal/core/program"
)

type moduleHealthStub struct{ snapshot program.HealthSnapshot }

func (stub moduleHealthStub) ModuleHealth() program.HealthSnapshot { return stub.snapshot }

type eventHealthStub struct{ snapshot event.HealthSnapshot }

func (stub eventHealthStub) EventOutputHealth() event.HealthSnapshot { return stub.snapshot }

func TestHealthReportsModuleAndEventOutputDegradation(t *testing.T) {
	failureAt := time.Now().UTC().Add(-time.Second)
	handler := &healthHandler{
		modules: moduleHealthStub{snapshot: program.HealthSnapshot{
			Status: "ok", Modules: map[string]program.HealthStatus{"builtin.llmusage": {Status: "ok"}},
		}},
		eventOutput: eventHealthStub{snapshot: event.HealthSnapshot{
			Enabled: true,
			Status:  "degraded",
			Sink: event.Status{
				Status: "degraded", LastFailureAt: failureAt, QueuedRecords: 3, QueuedBytes: 1024, DroppedRecords: 2,
			},
		}},
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))
	var response HealthResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Status != "degraded" || response.Modules.Items["builtin.llmusage"].Status != "ok" {
		t.Fatalf("unexpected health response: %#v", response)
	}
	output := response.EventOutput.Sink
	if output.Status != "degraded" || output.DroppedRecords != 2 || output.LastFailureAt == nil || !output.LastFailureAt.Equal(failureAt) {
		t.Fatalf("unexpected output health: %#v", output)
	}
}

func TestHealthReportsDisabledEventOutputWithoutDegradingService(t *testing.T) {
	handler := &healthHandler{
		modules: moduleHealthStub{snapshot: program.HealthSnapshot{Status: "ok", Modules: map[string]program.HealthStatus{}}},
		eventOutput: eventHealthStub{snapshot: event.HealthSnapshot{
			Status: "disabled", Sink: event.Status{Status: "disabled"},
		}},
	}
	response := handler.snapshot()
	if response.Status != "ok" || response.EventOutput.Enabled || response.EventOutput.Status != "disabled" || response.EventOutput.Sink.Status != "disabled" {
		t.Fatalf("unexpected disabled output health: %#v", response)
	}
}

func TestHealthReportsBodyStoreAdmissionCounters(t *testing.T) {
	store := bodystore.New(bodystore.Config{MemoryMaxBytes: 8, MaxBodyBytes: 4, ChunkBytes: 4, QueueMaxRequests: 1})
	first, err := store.Stream(t.Context(), io.NopCloser(strings.NewReader("1234")), 4, bodystore.Observer{})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Retire()
	_, _ = store.Stream(t.Context(), io.NopCloser(strings.NewReader("5678")), 4, bodystore.Observer{}, bodystore.StreamOptions{QueueWait: time.Millisecond})
	handler := &healthHandler{bodyStore: store}
	response := handler.snapshot()
	if response.BodyStore.QueueTimeoutTotal != 1 || response.BodyStore.MaxQueueWaitMS < 0 {
		t.Fatalf("unexpected body store health: %#v", response.BodyStore)
	}
}
