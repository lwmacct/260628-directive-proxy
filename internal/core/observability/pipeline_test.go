package observability_test

import (
	"context"
	"testing"

	"github.com/lwmacct/260628-directive-proxy/internal/core/observability"
	recordoutput "github.com/lwmacct/260628-directive-proxy/internal/testutil/recordoutput"
)

type emittingPlugin struct{}

func (emittingPlugin) Name() string { return "test.plugin" }
func (emittingPlugin) NewTrace(observability.TraceContext) observability.TraceObserver {
	return emittingTrace{}
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
