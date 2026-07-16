package handler

import (
	"context"
	"testing"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/observability"
)

type observabilityHealthStub struct{ snapshot observability.HealthSnapshot }

func (s observabilityHealthStub) ObservabilityHealth() observability.HealthSnapshot {
	return s.snapshot
}

func TestHealthReportsModuleAndSinkDegradation(t *testing.T) {
	failureAt := time.Now().UTC().Add(-time.Second)
	handler := &healthHandler{observability: observabilityHealthStub{snapshot: observability.HealthSnapshot{
		Enabled: true, Status: "degraded",
		Modules: map[string]observability.HealthStatus{
			"llmusage": {Status: "ok"},
		},
		Sink: observability.HealthStatus{Status: "degraded", LastFailureAt: failureAt, QueuedRecords: 3, QueuedBytes: 1024, DroppedRecords: 2},
	}}}
	response, err := handler.get(context.Background(), &struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if response.Body.Status != "degraded" || response.Body.Observability.Modules["llmusage"].Status != "ok" {
		t.Fatalf("unexpected health response: %#v", response.Body)
	}
	output := response.Body.Observability.Sink
	if output.Status != "degraded" || output.DroppedRecords != 2 || output.LastFailureAt == nil || !output.LastFailureAt.Equal(failureAt) {
		t.Fatalf("unexpected output health: %#v", output)
	}
}

func TestHealthReportsDisabledObservabilityWithoutDegradingService(t *testing.T) {
	handler := &healthHandler{observability: observabilityHealthStub{snapshot: observability.HealthSnapshot{
		Status: "disabled", Modules: map[string]observability.HealthStatus{}, Sink: observability.HealthStatus{Status: "disabled"},
	}}}
	response, err := handler.get(context.Background(), &struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if response.Body.Status != "ok" || response.Body.Observability.Enabled || response.Body.Observability.Status != "disabled" || response.Body.Observability.Sink.Status != "disabled" {
		t.Fatalf("unexpected disabled health response: %#v", response.Body)
	}
}
