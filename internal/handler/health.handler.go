package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/lwmacct/260628-directive-proxy/internal/core/observability"
)

type healthHandler struct {
	observability observability.HealthProvider
}

func RegisterHealth(api huma.API, health observability.HealthProvider) {
	handler := &healthHandler{observability: health}
	huma.Register(api, huma.Operation{
		OperationID: "get-health",
		Method:      http.MethodGet,
		Path:        "/health",
		Summary:     "Get service health",
	}, handler.get)
}

func (h *healthHandler) get(_ context.Context, _ *struct{}) (*HealthOutputDTO, error) {
	response := HealthResponseDTO{Status: "ok", Timestamp: time.Now().UTC(), Observability: ObservabilityHealthDTO{
		Status: "unavailable", Modules: map[string]ModuleHealthDTO{}, Sink: OutputHealthDTO{Type: "fluent", Status: "unavailable"},
	}}
	if h != nil && h.observability != nil {
		health := h.observability.ObservabilityHealth()
		response.Observability.Enabled = health.Enabled
		response.Observability.Status = health.Status
		if health.Status == "degraded" || health.Status == "unavailable" {
			response.Status = "degraded"
		}
		for name, status := range health.Modules {
			item := ModuleHealthDTO{Status: status.Status}
			if !status.LastFailureAt.IsZero() {
				item.LastFailureAt = &status.LastFailureAt
			}
			response.Observability.Modules[name] = item
		}
		status := health.Sink
		response.Observability.Sink = OutputHealthDTO{Type: "fluent", Status: status.Status, QueuedRecords: status.QueuedRecords, QueuedBytes: status.QueuedBytes, DroppedRecords: status.DroppedRecords}
		if !status.LastFailureAt.IsZero() {
			response.Observability.Sink.LastFailureAt = &status.LastFailureAt
		}
	}
	return &HealthOutputDTO{Body: response}, nil
}
