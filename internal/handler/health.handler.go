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
		Status: "ok", Plugins: map[string]PluginHealthDTO{}, Outputs: map[string]OutputHealthDTO{},
	}}
	if h != nil && h.observability != nil {
		health := h.observability.ObservabilityHealth()
		response.Observability.Status = health.Status
		if health.Status != "ok" {
			response.Status = "degraded"
		}
		for name, status := range health.Plugins {
			item := PluginHealthDTO{Status: status.Status}
			if !status.LastFailureAt.IsZero() {
				item.LastFailureAt = &status.LastFailureAt
			}
			response.Observability.Plugins[name] = item
		}
		for name, status := range health.Outputs {
			item := OutputHealthDTO{Status: status.Status, QueuedRecords: status.QueuedRecords, QueuedBytes: status.QueuedBytes, DroppedRecords: status.DroppedRecords}
			if !status.LastFailureAt.IsZero() {
				item.LastFailureAt = &status.LastFailureAt
			}
			response.Observability.Outputs[name] = item
		}
	}
	return &HealthOutputDTO{Body: response}, nil
}
