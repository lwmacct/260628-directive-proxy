package observability

import (
	"context"
	"time"
)

type HealthStatus struct {
	Status         string    `json:"status"`
	LastFailureAt  time.Time `json:"last_failure_at,omitempty"`
	QueuedRecords  int64     `json:"queued_records,omitempty"`
	QueuedBytes    int64     `json:"queued_bytes,omitempty"`
	DroppedRecords uint64    `json:"dropped_records,omitempty"`
}

type Sink interface {
	Start(context.Context) error
	// Write must consume Record synchronously and must not retain it after return.
	// shard identifies the worker and lets a partitioned sink bind it to one connection.
	Write(context.Context, int, Record) error
	Health() HealthStatus
	Close(context.Context) error
}

type SinkConfig struct {
	Sink            Sink
	Workers         int
	QueueMaxRecords int
	QueueMaxBytes   int64
}

type HealthProvider interface {
	ObservabilityHealth() HealthSnapshot
}

type HealthSnapshot struct {
	Enabled bool                    `json:"enabled"`
	Status  string                  `json:"status"`
	Modules map[string]HealthStatus `json:"modules"`
	Sink    HealthStatus            `json:"sink"`
}
