package server

import "time"

type HealthResponse struct {
	Status      string              `json:"status" example:"ok"`
	Timestamp   time.Time           `json:"timestamp"`
	Modules     ModuleRuntimeHealth `json:"modules"`
	EventOutput EventOutputHealth   `json:"event_output"`
}

type ModuleRuntimeHealth struct {
	Status string                  `json:"status"`
	Items  map[string]ModuleHealth `json:"items"`
}

type ModuleHealth struct {
	Status        string     `json:"status"`
	LastFailureAt *time.Time `json:"last_failure_at,omitempty"`
}

type EventOutputHealth struct {
	Enabled bool         `json:"enabled"`
	Status  string       `json:"status"`
	Sink    OutputHealth `json:"sink"`
}

type OutputHealth struct {
	Type           string     `json:"type"`
	Status         string     `json:"status"`
	LastFailureAt  *time.Time `json:"last_failure_at,omitempty"`
	QueuedRecords  int64      `json:"queued_records,omitempty"`
	QueuedBytes    int64      `json:"queued_bytes,omitempty"`
	DroppedRecords uint64     `json:"dropped_records,omitempty"`
}
