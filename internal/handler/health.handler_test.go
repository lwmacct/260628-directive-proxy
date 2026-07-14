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

func TestHealthReportsPluginAndOutputDegradation(t *testing.T) {
	failureAt := time.Now().UTC().Add(-time.Second)
	handler := &healthHandler{observability: observabilityHealthStub{snapshot: observability.HealthSnapshot{
		Status: "degraded",
		Plugins: map[string]observability.HealthStatus{
			"llmusage": {Status: "ok"},
		},
		Outputs: map[string]observability.HealthStatus{
			"fluent-primary": {Status: "degraded", LastFailureAt: failureAt, QueuedRecords: 3, QueuedBytes: 1024, DroppedRecords: 2},
		},
	}}}
	response, err := handler.get(context.Background(), &struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if response.Body.Status != "degraded" || response.Body.Observability.Plugins["llmusage"].Status != "ok" {
		t.Fatalf("unexpected health response: %#v", response.Body)
	}
	output := response.Body.Observability.Outputs["fluent-primary"]
	if output.Status != "degraded" || output.DroppedRecords != 2 || output.LastFailureAt == nil || !output.LastFailureAt.Equal(failureAt) {
		t.Fatalf("unexpected output health: %#v", output)
	}
}
