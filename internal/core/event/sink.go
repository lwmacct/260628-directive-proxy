package event

import (
	"context"
	"time"
)

type Status struct {
	Status         string    `json:"status"`
	LastFailureAt  time.Time `json:"last_failure_at,omitempty"`
	QueuedRecords  int64     `json:"queued_records,omitempty"`
	QueuedBytes    int64     `json:"queued_bytes,omitempty"`
	DroppedRecords uint64    `json:"dropped_records,omitempty"`
}

type Sink interface {
	Start(context.Context) error
	// Write consumes Record synchronously and must not retain it after return.
	// shard identifies the worker assigned to the trace partition.
	Write(context.Context, int, Record) error
	Health() Status
	Close(context.Context) error
}

type Config struct {
	Sink            Sink
	Workers         int
	QueueMaxRecords int
	QueueMaxBytes   int64
}

type HealthSnapshot struct {
	Enabled bool   `json:"enabled"`
	Status  string `json:"status"`
	Sink    Status `json:"sink"`
}

type HealthProvider interface {
	EventOutputHealth() HealthSnapshot
}

func (dispatcher *Dispatcher) EventOutputHealth() HealthSnapshot {
	if dispatcher == nil {
		return HealthSnapshot{Status: "disabled", Sink: Status{Status: "disabled"}}
	}
	sink := dispatcher.sinkHealth()
	return HealthSnapshot{Enabled: true, Status: sink.Status, Sink: sink}
}
