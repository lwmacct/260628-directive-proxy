package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/event"
	"github.com/lwmacct/260628-directive-proxy/internal/core/program"
)

type healthHandler struct {
	modules     program.HealthProvider
	eventOutput event.HealthProvider
}

func newHealthHandler(modules program.HealthProvider, eventOutput event.HealthProvider) http.Handler {
	return &healthHandler{modules: modules, eventOutput: eventOutput}
}

func (handler *healthHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request == nil || request.Method != http.MethodGet {
		writer.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(writer).Encode(handler.snapshot())
}

func (handler *healthHandler) snapshot() HealthResponse {
	response := HealthResponse{
		Status:    "ok",
		Timestamp: time.Now().UTC(),
		Modules: ModuleRuntimeHealth{
			Status: "unavailable", Items: map[string]ModuleHealth{},
		},
		EventOutput: EventOutputHealth{
			Status: "disabled", Sink: OutputHealth{Type: "fluent", Status: "disabled"},
		},
	}
	if handler != nil && handler.modules != nil {
		health := handler.modules.ModuleHealth()
		response.Modules.Status = health.Status
		if health.Status == "degraded" || health.Status == "unavailable" {
			response.Status = "degraded"
		}
		for name, status := range health.Modules {
			item := ModuleHealth{Status: status.Status}
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
		response.EventOutput.Sink = OutputHealth{
			Type: "fluent", Status: status.Status, QueuedRecords: status.QueuedRecords,
			QueuedBytes: status.QueuedBytes, DroppedRecords: status.DroppedRecords,
		}
		if !status.LastFailureAt.IsZero() {
			response.EventOutput.Sink.LastFailureAt = &status.LastFailureAt
		}
	}
	return response
}
