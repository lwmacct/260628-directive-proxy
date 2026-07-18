package event

import (
	"sync"
	"sync/atomic"
	"time"
)

const SchemaVersion = "dp.event.v6"

type Record struct {
	SchemaVersion string            `msg:"schema_version"`
	Producer      string            `msg:"producer"`
	Topic         string            `msg:"topic"`
	TraceID       string            `msg:"trace_id"`
	Metadata      map[string]string `msg:"metadata"`
	RoundTrip     int               `msg:"round_trip,omitempty"`
	Sequence      uint64            `msg:"sequence"`
	OccurredAt    string            `msg:"occurred_at"`
	Data          map[string]any    `msg:"data"`
	Time          time.Time         `msg:"-"`
	resource      *recordResource
}

type recordResource struct {
	refs    atomic.Int64
	once    sync.Once
	release func()
}

func newRecordResource(release func()) *recordResource {
	if release == nil {
		return nil
	}
	resource := &recordResource{release: release}
	resource.refs.Store(1)
	return resource
}

func (r Record) retain() {
	if r.resource != nil {
		r.resource.refs.Add(1)
	}
}

func (r Record) release() {
	if r.resource == nil || r.resource.refs.Add(-1) != 0 {
		return
	}
	r.resource.once.Do(r.resource.release)
}

func (r Record) Map() map[string]any {
	record := map[string]any{
		"schema_version": r.SchemaVersion,
		"producer":       r.Producer,
		"topic":          r.Topic,
		"trace_id":       r.TraceID,
		"metadata":       r.Metadata,
		"sequence":       r.Sequence,
		"occurred_at":    r.OccurredAt,
		"data":           r.Data,
	}
	if r.RoundTrip > 0 {
		record["round_trip"] = r.RoundTrip
	}
	return record
}
