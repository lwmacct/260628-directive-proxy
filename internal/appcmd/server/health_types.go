package server

import "time"

type HealthResponse struct {
	Status      string              `json:"status" example:"ok"`
	Timestamp   time.Time           `json:"timestamp"`
	Modules     ModuleRuntimeHealth `json:"modules"`
	EventOutput EventOutputHealth   `json:"event_output"`
	BodyStore   BodyStoreHealth     `json:"body_store"`
}

type BodyStoreHealth struct {
	Status               string `json:"status"`
	MemoryUsedBytes      int64  `json:"memory_used_bytes"`
	MemoryAvailableBytes int64  `json:"memory_available_bytes"`
	QueuedRequests       int    `json:"queued_requests"`
	AdmittedTotal        uint64 `json:"admitted_total"`
	QueueFullTotal       uint64 `json:"queue_full_total"`
	QueueTimeoutTotal    uint64 `json:"queue_timeout_total"`
	CanceledTotal        uint64 `json:"canceled_total"`
	MaxQueueWaitMS       int64  `json:"max_queue_wait_ms"`
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
