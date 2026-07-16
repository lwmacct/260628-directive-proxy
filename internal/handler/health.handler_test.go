package handler

import (
	"context"
	"testing"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/event"
	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
)

type moduleHealthStub struct{ snapshot module.HealthSnapshot }

func (stub moduleHealthStub) ModuleHealth() module.HealthSnapshot { return stub.snapshot }

type eventHealthStub struct{ snapshot event.HealthSnapshot }

func (stub eventHealthStub) EventOutputHealth() event.HealthSnapshot { return stub.snapshot }

func TestHealthReportsModuleAndEventOutputDegradation(t *testing.T) {
	failureAt := time.Now().UTC().Add(-time.Second)
	handler := &healthHandler{
		modules: moduleHealthStub{snapshot: module.HealthSnapshot{
			Status: "ok", Modules: map[string]module.HealthStatus{"builtin.llmusage": {Status: "ok"}},
		}},
		eventOutput: eventHealthStub{snapshot: event.HealthSnapshot{
			Enabled: true,
			Status:  "degraded",
			Sink: event.Status{
				Status: "degraded", LastFailureAt: failureAt, QueuedRecords: 3, QueuedBytes: 1024, DroppedRecords: 2,
			},
		}},
	}
	response, err := handler.get(context.Background(), &struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if response.Body.Status != "degraded" || response.Body.Modules.Items["builtin.llmusage"].Status != "ok" {
		t.Fatalf("unexpected health response: %#v", response.Body)
	}
	output := response.Body.EventOutput.Sink
	if output.Status != "degraded" || output.DroppedRecords != 2 || output.LastFailureAt == nil || !output.LastFailureAt.Equal(failureAt) {
		t.Fatalf("unexpected output health: %#v", output)
	}
}

func TestHealthReportsDisabledEventOutputWithoutDegradingService(t *testing.T) {
	handler := &healthHandler{
		modules: moduleHealthStub{snapshot: module.HealthSnapshot{Status: "ok", Modules: map[string]module.HealthStatus{}}},
		eventOutput: eventHealthStub{snapshot: event.HealthSnapshot{
			Status: "disabled", Sink: event.Status{Status: "disabled"},
		}},
	}
	response, err := handler.get(context.Background(), &struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if response.Body.Status != "ok" || response.Body.EventOutput.Enabled || response.Body.EventOutput.Status != "disabled" || response.Body.EventOutput.Sink.Status != "disabled" {
		t.Fatalf("unexpected disabled output health: %#v", response.Body)
	}
}
