package capture

import "time"

const SchemaVersion = "dproxy.capture.v1"

type Event struct {
	SchemaVersion string         `msg:"schema_version"`
	RecordID      string         `msg:"record_id"`
	TraceID       string         `msg:"trace_id"`
	AttemptID     string         `msg:"attempt_id,omitempty"`
	InstanceID    string         `msg:"instance_id"`
	Sequence      uint64         `msg:"sequence"`
	Kind          string         `msg:"kind"`
	OccurredAt    string         `msg:"occurred_at"`
	Data          map[string]any `msg:"data"`
	Time          time.Time      `msg:"-"`
}

func (e Event) Record() map[string]any {
	record := map[string]any{
		"schema_version": e.SchemaVersion,
		"record_id":      e.RecordID,
		"trace_id":       e.TraceID,
		"instance_id":    e.InstanceID,
		"sequence":       e.Sequence,
		"kind":           e.Kind,
		"occurred_at":    e.OccurredAt,
		"data":           e.Data,
	}
	if e.AttemptID != "" {
		record["attempt_id"] = e.AttemptID
	}
	return record
}
