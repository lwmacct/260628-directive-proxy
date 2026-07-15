package observability

import (
	"sync"
	"sync/atomic"
	"time"
)

const SchemaVersion = "dproxy.event.v1"

type Record struct {
	SchemaVersion string         `msg:"schema_version"`
	Plugin        string         `msg:"plugin"`
	Topic         string         `msg:"topic"`
	RecordID      string         `msg:"record_id"`
	TraceID       string         `msg:"trace_id"`
	Attempt       int            `msg:"attempt,omitempty"`
	Sequence      uint64         `msg:"sequence"`
	OccurredAt    string         `msg:"occurred_at"`
	Data          map[string]any `msg:"data"`
	Time          time.Time      `msg:"-"`
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
	return record
}
