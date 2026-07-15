package observability_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/lwmacct/260628-directive-proxy/internal/core/observability"
	recordoutput "github.com/lwmacct/260628-directive-proxy/internal/testutil/recordoutput"
)

type emittingPlugin struct{}

func (emittingPlugin) Name() string { return "test.plugin" }
func (emittingPlugin) NewTrace(observability.TraceContext) observability.TraceObserver {
	return emittingTrace{}
}

type ownedPlugin struct{ released *atomic.Bool }

func (p ownedPlugin) Name() string { return "owned.plugin" }
func (p ownedPlugin) NewTrace(observability.TraceContext) observability.TraceObserver {
	return ownedTrace{released: p.released}
}

type ownedTrace struct {
	observability.NopTraceObserver
	released *atomic.Bool
}

func (t ownedTrace) Observe(signal observability.Signal, emitter observability.Emitter) {
	emitter.EmitOwned("owned.record", signal.Attempt, map[string]any{"data": []byte("owned")}, func() { t.released.Store(true) })
}

type blockingOutput struct {
	started chan struct{}
	allow   chan struct{}
}

func (*blockingOutput) Name() string                { return "blocking" }
func (*blockingOutput) Start(context.Context) error { return nil }
func (o *blockingOutput) Write(context.Context, observability.Record) error {
	close(o.started)
	<-o.allow
	return nil
}
func (*blockingOutput) Health() observability.HealthStatus {
	return observability.HealthStatus{Status: "ok"}
}
func (*blockingOutput) Close(context.Context) error { return nil }

func TestPipelineReleasesOwnedRecordAfterOutputReturns(t *testing.T) {
	released := &atomic.Bool{}
	output := &blockingOutput{started: make(chan struct{}), allow: make(chan struct{})}
	pipeline, err := observability.NewPipeline(context.Background(), []observability.Plugin{ownedPlugin{released: released}}, []observability.OutputBinding{{
		Output: output, Routes: []string{"owned.**"}, QueueCapacity: 1, QueueMaxBytes: 1024,
	}})
	if err != nil {
		t.Fatal(err)
	}
	trace := pipeline.StartTrace(observability.TraceContext{TraceID: "owned"})
	trace.Observe(observability.Signal{Value: "emit"})
	<-output.started
	if released.Load() {
		t.Fatal("owned record was released while output was using it")
	}
	close(output.allow)
	trace.Close()
	if err := pipeline.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !released.Load() {
		t.Fatal("owned record was not released after output returned")
	}
}

type emittingTrace struct{ observability.NopTraceObserver }

func (emittingTrace) Observe(signal observability.Signal, emitter observability.Emitter) {
	emitter.Emit("test.record", signal.Attempt, map[string]any{"value": signal.Value})
}

func TestPipelineRoutesRecordsAndPreservesTraceSequence(t *testing.T) {
	output := recordoutput.New("memory")
	pipeline, err := observability.NewPipeline(context.Background(), []observability.Plugin{emittingPlugin{}}, []observability.OutputBinding{{
		Output: output, Routes: []string{"test.**"}, Workers: 2, QueueCapacity: 16, QueueMaxBytes: 1 << 20,
	}})
	if err != nil {
		t.Fatal(err)
	}
	trace := pipeline.StartTrace(observability.TraceContext{TraceID: "trace", InstanceID: "instance"})
	trace.Observe(observability.Signal{Attempt: 1, Value: "one"})
	trace.Observe(observability.Signal{Attempt: 1, Value: "two"})
	trace.Close()
	if err := pipeline.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	records := output.Records()
	if len(records) != 2 || records[0].Sequence != 1 || records[1].Sequence != 2 || records[0].RecordID != "trace:00000001" || records[1].RecordID != "trace:00000002" {
		t.Fatalf("unexpected records: %#v", records)
	}
	if health := pipeline.ObservabilityHealth(); health.Status != "ok" || health.Plugins["test.plugin"].Status != "ok" {
		t.Fatalf("unexpected health: %#v", health)
	}
}

type panicPlugin struct{}

func (panicPlugin) Name() string { return "panic.plugin" }
func (panicPlugin) NewTrace(observability.TraceContext) observability.TraceObserver {
	return panicTrace{}
}

type panicTrace struct{ observability.NopTraceObserver }

func (panicTrace) Observe(observability.Signal, observability.Emitter) { panic("boom") }

func TestPipelineContainsPluginPanics(t *testing.T) {
	pipeline, err := observability.NewPipeline(context.Background(), []observability.Plugin{panicPlugin{}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	trace := pipeline.StartTrace(observability.TraceContext{TraceID: "trace"})
	trace.Observe(observability.Signal{Value: "trigger"})
	health := pipeline.ObservabilityHealth()
	if health.Status != "degraded" || health.Plugins["panic.plugin"].Status != "degraded" {
		t.Fatalf("plugin panic did not degrade health: %#v", health)
	}
}
