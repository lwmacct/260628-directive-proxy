package event

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/metadata"
)

type Trace struct {
	dispatcher *Dispatcher
	traceID    string
	metadata   metadata.Set
	mu         sync.Mutex
	sequence   uint64
	closed     atomic.Bool
}

type traceEmitter struct {
	trace     *Trace
	producer  string
	roundTrip int
}

func (dispatcher *Dispatcher) Open(traceID string, fields metadata.Set) Session {
	if dispatcher == nil || dispatcher.closed.Load() || strings.TrimSpace(traceID) == "" || fields.TraceID() != traceID {
		return nil
	}
	return &Trace{dispatcher: dispatcher, traceID: traceID, metadata: fields}
}

func (trace *Trace) Emitter(producer string, roundTrip int) Emitter {
	return traceEmitter{trace: trace, producer: producer, roundTrip: roundTrip}
}

func (trace *Trace) Close() {
	if trace != nil {
		trace.closed.Store(true)
	}
}

func (emitter traceEmitter) Emit(topic string, data map[string]any) bool {
	return emitter.trace.emit(emitter.producer, topic, emitter.roundTrip, data, nil, false)
}

func (emitter traceEmitter) EmitOwned(topic string, data map[string]any, release func()) bool {
	return emitter.trace.emit(emitter.producer, topic, emitter.roundTrip, data, release, false)
}

func (emitter traceEmitter) EmitBorrowed(topic string, data map[string]any) bool {
	return emitter.trace.emit(emitter.producer, topic, emitter.roundTrip, data, nil, true)
}

func (trace *Trace) emit(producer, topic string, roundTrip int, data map[string]any, release func(), copyBorrowed bool) bool {
	if trace == nil || trace.dispatcher == nil || trace.closed.Load() {
		if release != nil {
			release()
		}
		return false
	}
	trace.mu.Lock()
	defer trace.mu.Unlock()
	trace.sequence++
	now := time.Now().UTC()
	record := Record{
		SchemaVersion: SchemaVersion,
		Producer:      producer,
		Topic:         topic,
		RecordID:      fmt.Sprintf("%s:%08d", trace.traceID, trace.sequence),
		TraceID:       trace.traceID,
		Metadata:      trace.metadata.Map(),
		RoundTrip:     roundTrip,
		Sequence:      trace.sequence,
		OccurredAt:    now.Format(time.RFC3339Nano),
		Data:          data,
		Time:          now,
		resource:      newRecordResource(release),
	}
	accepted := trace.dispatcher.enqueue(record, copyBorrowed)
	record.release()
	return accepted
}
