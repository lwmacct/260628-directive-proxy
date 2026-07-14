package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/lwmacct/260628-directive-proxy/internal/core/capture"
)

type healthHandler struct {
	capture capture.HealthProvider
}

func RegisterHealth(api huma.API, captureHealth capture.HealthProvider) {
	handler := &healthHandler{capture: captureHealth}
	huma.Register(api, huma.Operation{
		OperationID: "get-health",
		Method:      http.MethodGet,
		Path:        "/health",
		Summary:     "Get service health",
	}, handler.get)
}

func (h *healthHandler) get(_ context.Context, _ *struct{}) (*HealthOutputDTO, error) {
	response := HealthResponseDTO{Status: "ok", Timestamp: time.Now().UTC(), Capture: CaptureHealthDTO{Status: "unavailable"}}
	if h != nil && h.capture != nil {
		status := h.capture.CaptureHealth()
		response.Capture.Status = status.Status
		if !status.LastFailureAt.IsZero() {
			response.Capture.LastFailureAt = &status.LastFailureAt
		}
	}
	return &HealthOutputDTO{Body: response}, nil
}
