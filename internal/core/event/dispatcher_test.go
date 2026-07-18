package event_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/lwmacct/260628-directive-proxy/internal/core/event"
	"github.com/lwmacct/260628-directive-proxy/internal/core/metadata"
	recordoutput "github.com/lwmacct/260628-directive-proxy/internal/testutil/recordoutput"
)

type blockingSink struct {
	started  chan struct{}
	allow    chan struct{}
	captured chan string
}

func (*blockingSink) Start(context.Context) error { return nil }
func (sink *blockingSink) Write(_ context.Context, _ int, record event.Record) error {
	close(sink.started)
	<-sink.allow
	if sink.captured != nil {
		sink.captured <- string(record.Data["data"].([]byte))
	}
	return nil
}
func (*blockingSink) Health() event.Status        { return event.Status{Status: "ok"} }
func (*blockingSink) Close(context.Context) error { return nil }

func TestDispatcherReleasesOwnedRecordAfterSinkReturns(t *testing.T) {
	released := &atomic.Bool{}
	sink := &blockingSink{started: make(chan struct{}), allow: make(chan struct{})}
	dispatcher, err := event.NewDispatcher(context.Background(), event.Config{Sink: sink, QueueMaxRecords: 1, QueueMaxBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	trace := dispatcher.Open("trace", eventMetadata(t))
	emitter := trace.Emitter("binding", 1)
	emitter.EmitOwned("owned.record", map[string]any{"data": []byte("owned")}, func() { released.Store(true) })
	<-sink.started
	if released.Load() {
		t.Fatal("owned record was released while sink was using it")
	}
	close(sink.allow)
	trace.Close()
	if err := dispatcher.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !released.Load() {
		t.Fatal("owned record was not released after sink returned")
	}
}

func TestDispatcherCopiesBorrowedDataOnlyForAcceptedRecords(t *testing.T) {
	source := []byte("original")
	sink := &blockingSink{started: make(chan struct{}), allow: make(chan struct{}), captured: make(chan string, 1)}
	dispatcher, err := event.NewDispatcher(context.Background(), event.Config{Sink: sink, QueueMaxRecords: 1, QueueMaxBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	first := dispatcher.Open("first", eventMetadata(t))
	if !first.Emitter("binding", 1).EmitBorrowed("borrowed.record", map[string]any{"data": source}) {
		t.Fatal("first borrowed record was rejected")
	}
	<-sink.started
	copy(source, "modified")
	second := dispatcher.Open("second", eventMetadata(t))
	if second.Emitter("binding", 1).EmitBorrowed("borrowed.record", map[string]any{"data": source}) {
		t.Fatal("record exceeding the global queue limit was accepted")
	}
	if health := dispatcher.EventOutputHealth(); health.Sink.DroppedRecords != 1 || health.Sink.QueuedRecords != 1 {
		t.Fatalf("unexpected bounded queue health: %#v", health.Sink)
	}
	close(sink.allow)
	if got := <-sink.captured; got != "original" {
		t.Fatalf("borrowed data changed after emission: %q", got)
	}
	first.Close()
	second.Close()
	_ = dispatcher.Close(context.Background())
}

func TestTraceAssignsRecordIdentityAndSequence(t *testing.T) {
	sink := recordoutput.New("memory")
	dispatcher, err := event.NewDispatcher(context.Background(), event.Config{Sink: sink, Workers: 2, QueueMaxRecords: 16, QueueMaxBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	trace := dispatcher.Open("trace", eventMetadata(t))
	emitter := trace.Emitter("binding", 2)
	emitter.Emit("test.record", map[string]any{"value": "one"})
	emitter.Emit("test.record", map[string]any{"value": "two"})
	trace.Close()
	if err := dispatcher.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	records := sink.Records()
	if len(records) != 2 || records[0].Sequence != 1 || records[1].Sequence != 2 || records[0].TraceID != "trace" || records[0].Producer != "binding" || records[0].RoundTrip != 2 {
		t.Fatalf("unexpected records: %#v", records)
	}
	if records[0].SchemaVersion != event.SchemaVersion || records[0].TraceID != "trace" || records[0].Metadata[metadata.KeyUserKey] != "uk_test" {
		t.Fatalf("record metadata was not attached: %#v", records[0])
	}
}

func eventMetadata(t *testing.T) metadata.Set {
	t.Helper()
	fields, err := metadata.Compile(map[string]string{metadata.KeyUserKey: "uk_test"})
	if err != nil {
		t.Fatal(err)
	}
	return fields
}
