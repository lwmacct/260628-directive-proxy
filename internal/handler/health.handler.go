package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

type healthHandler struct{}

func RegisterHealth(api huma.API) {
	handler := &healthHandler{}
	huma.Register(api, huma.Operation{
		OperationID: "get-health",
		Method:      http.MethodGet,
		Path:        "/health",
		Summary:     "Get service health",
	}, handler.get)
}

func (h *healthHandler) get(_ context.Context, _ *struct{}) (*HealthOutputDTO, error) {
	return &HealthOutputDTO{Body: HealthResponseDTO{Status: "ok", Timestamp: time.Now().UTC()}}, nil
}
