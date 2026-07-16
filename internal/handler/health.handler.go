package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/lwmacct/260628-directive-proxy/internal/core/event"
	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
)

type healthHandler struct {
	modules     module.HealthProvider
	eventOutput event.HealthProvider
}

func RegisterHealth(api huma.API, modules module.HealthProvider, eventOutput event.HealthProvider) {
	handler := &healthHandler{modules: modules, eventOutput: eventOutput}
	huma.Register(api, huma.Operation{
		OperationID: "get-health",
		Method:      http.MethodGet,
		Path:        "/health",
		Summary:     "Get service health",
	}, handler.get)
}

func (handler *healthHandler) get(_ context.Context, _ *struct{}) (*HealthOutputDTO, error) {
	response := HealthResponseDTO{
		Status:    "ok",
		Timestamp: time.Now().UTC(),
		Modules: ModuleRuntimeHealthDTO{
			Status: "unavailable", Items: map[string]ModuleHealthDTO{},
		},
		EventOutput: EventOutputHealthDTO{
			Status: "disabled", Sink: OutputHealthDTO{Type: "fluent", Status: "disabled"},
		},
	}
	if handler != nil && handler.modules != nil {
		health := handler.modules.ModuleHealth()
		response.Modules.Status = health.Status
		if health.Status == "degraded" || health.Status == "unavailable" {
			response.Status = "degraded"
		}
		for name, status := range health.Modules {
			item := ModuleHealthDTO{Status: status.Status}
			if !status.LastFailureAt.IsZero() {
				item.LastFailureAt = &status.LastFailureAt
			}
			response.Modules.Items[name] = item
		}
	}
	if handler != nil && handler.eventOutput != nil {
		health := handler.eventOutput.EventOutputHealth()
		response.EventOutput.Enabled = health.Enabled
		response.EventOutput.Status = health.Status
		if health.Status == "degraded" || health.Status == "unavailable" {
			response.Status = "degraded"
		}
		status := health.Sink
		response.EventOutput.Sink = OutputHealthDTO{
			Type: "fluent", Status: status.Status, QueuedRecords: status.QueuedRecords,
			QueuedBytes: status.QueuedBytes, DroppedRecords: status.DroppedRecords,
		}
		if !status.LastFailureAt.IsZero() {
			response.EventOutput.Sink.LastFailureAt = &status.LastFailureAt
		}
	}
	return &HealthOutputDTO{Body: response}, nil
}
