package handler

import "time"

type HealthResponseDTO struct {
	Status        string                 `json:"status" example:"ok"`
	Timestamp     time.Time              `json:"timestamp"`
	Observability ObservabilityHealthDTO `json:"observability"`
}

type ObservabilityHealthDTO struct {
	Enabled bool                       `json:"enabled"`
	Status  string                     `json:"status"`
	Plugins map[string]PluginHealthDTO `json:"plugins"`
	Sink    OutputHealthDTO            `json:"sink"`
}

type PluginHealthDTO struct {
	Status        string     `json:"status"`
	LastFailureAt *time.Time `json:"last_failure_at,omitempty"`
}

type OutputHealthDTO struct {
	Type           string     `json:"type"`
	Status         string     `json:"status"`
	LastFailureAt  *time.Time `json:"last_failure_at,omitempty"`
	QueuedRecords  int64      `json:"queued_records,omitempty"`
	QueuedBytes    int64      `json:"queued_bytes,omitempty"`
	DroppedRecords uint64     `json:"dropped_records,omitempty"`
}

type HealthOutputDTO struct {
	Body HealthResponseDTO
}
