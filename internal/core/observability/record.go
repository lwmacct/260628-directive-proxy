package observability

import "time"

const SchemaVersion = "dproxy.event.v1"

type Record struct {
	SchemaVersion string         `msg:"schema_version"`
	Plugin        string         `msg:"plugin"`
	Topic         string         `msg:"topic"`
	RecordID      string         `msg:"record_id"`
	TraceID       string         `msg:"trace_id"`
	Attempt       int            `msg:"attempt,omitempty"`
	InstanceID    string         `msg:"instance_id,omitempty"`
	Sequence      uint64         `msg:"sequence"`
	OccurredAt    string         `msg:"occurred_at"`
	Data          map[string]any `msg:"data"`
	Time          time.Time      `msg:"-"`
}

func (r Record) Map() map[string]any {
	record := map[string]any{
		"schema_version": r.SchemaVersion,
		"plugin":         r.Plugin,
		"topic":          r.Topic,
		"record_id":      r.RecordID,
		"trace_id":       r.TraceID,
		"sequence":       r.Sequence,
		"occurred_at":    r.OccurredAt,
		"data":           r.Data,
	}
	if r.Attempt > 0 {
		record["attempt"] = r.Attempt
	}
	if r.InstanceID != "" {
		record["instance_id"] = r.InstanceID
	}
	return record
}
