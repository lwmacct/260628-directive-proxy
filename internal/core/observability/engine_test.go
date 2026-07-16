package observability_test

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
	"github.com/lwmacct/260628-directive-proxy/internal/core/observability"
	recordoutput "github.com/lwmacct/260628-directive-proxy/internal/testutil/recordoutput"
)

type testDefinition struct {
	name string
	open func() module.Instance
}

func (definition testDefinition) Name() string { return definition.name }
func (definition testDefinition) Compile(json.RawMessage) (module.Binding, error) {
	return testBinding{open: definition.open}, nil
}

type testBinding struct{ open func() module.Instance }

func (testBinding) Lifetime() module.Lifetime { return module.LifetimeRequest }
func (binding testBinding) Open(module.OpenContext) (module.Instance, error) {
	return binding.open(), nil
}

type testInstance struct {
	handle func(module.EventContext, module.RequestStarted) error
}

func (instance testInstance) Mount(binder *module.Binder) {
	binder.OnRequestStarted(module.SyncPolicy(), instance.handle)
}
func (testInstance) Finish(module.FinishContext) error { return nil }

type blockingOutput struct {
	started  chan struct{}
	allow    chan struct{}
	captured chan string
}

func (*blockingOutput) Start(context.Context) error { return nil }
func (output *blockingOutput) Write(_ context.Context, _ int, record observability.Record) error {
	close(output.started)
	<-output.allow
	if output.captured != nil {
		output.captured <- string(record.Data["data"].([]byte))
	}
	return nil
}
func (*blockingOutput) Health() observability.HealthStatus {
	return observability.HealthStatus{Status: "ok"}
}
func (*blockingOutput) Close(context.Context) error { return nil }

func TestEngineReleasesOwnedRecordAfterSinkReturns(t *testing.T) {
	released := &atomic.Bool{}
	sink := &blockingOutput{started: make(chan struct{}), allow: make(chan struct{})}
	definition := testDefinition{name: "owned.module", open: func() module.Instance {
		return testInstance{handle: func(ctx module.EventContext, _ module.RequestStarted) error {
			ctx.Output.EmitOwned("owned.record", map[string]any{"data": []byte("owned")}, func() { released.Store(true) })
			return nil
		}}
	}}
	engine, err := observability.NewEngine(context.Background(), []module.Definition{definition}, observability.SinkConfig{Sink: sink, QueueMaxRecords: 1, QueueMaxBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	run, scope := openRequestScope(t, engine, "owned.module")
	_ = scope.RequestStarted(t.Context(), module.RequestStarted{})
	<-sink.started
	if released.Load() {
		t.Fatal("owned record was released while sink was using it")
	}
	close(sink.allow)
	_ = scope.Finish(context.Background(), module.FinishCompleted)
	run.Close()
	if err := engine.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !released.Load() {
		t.Fatal("owned record was not released after sink returned")
	}
}

func TestEngineCopiesBorrowedDataOnlyForAcceptedRecords(t *testing.T) {
	source := []byte("original")
	sink := &blockingOutput{started: make(chan struct{}), allow: make(chan struct{}), captured: make(chan string, 1)}
	definition := testDefinition{name: "borrowed.module", open: func() module.Instance {
		return testInstance{handle: func(ctx module.EventContext, _ module.RequestStarted) error {
			ctx.Output.EmitBorrowed("borrowed.record", map[string]any{"data": source})
			return nil
		}}
	}}
	engine, err := observability.NewEngine(context.Background(), []module.Definition{definition}, observability.SinkConfig{Sink: sink, QueueMaxRecords: 1, QueueMaxBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	firstRun, first := openRequestScope(t, engine, "borrowed.module")
	_ = first.RequestStarted(t.Context(), module.RequestStarted{})
	<-sink.started
	copy(source, "modified")
	secondRun, second := openRequestScope(t, engine, "borrowed.module")
	_ = second.RequestStarted(t.Context(), module.RequestStarted{})
	if health := engine.ObservabilityHealth(); health.Sink.DroppedRecords != 1 || health.Sink.QueuedRecords != 1 {
		t.Fatalf("unexpected bounded queue health: %#v", health.Sink)
	}
	close(sink.allow)
	if got := <-sink.captured; got != "original" {
		t.Fatalf("borrowed data changed after emission: %q", got)
	}
	_ = first.Finish(context.Background(), module.FinishCompleted)
	_ = second.Finish(context.Background(), module.FinishCompleted)
	firstRun.Close()
	secondRun.Close()
	_ = engine.Close(context.Background())
}

func TestEngineWritesRecordsWithRunSequenceAndBindingProducer(t *testing.T) {
	sink := recordoutput.New("memory")
	definition := testDefinition{name: "test.module", open: func() module.Instance {
		return testInstance{handle: func(ctx module.EventContext, value module.RequestStarted) error {
			ctx.Output.Emit("test.record", map[string]any{"value": value.Method})
			return nil
		}}
	}}
	engine, err := observability.NewEngine(context.Background(), []module.Definition{definition}, observability.SinkConfig{Sink: sink, Workers: 2, QueueMaxRecords: 16, QueueMaxBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	run, scope := openRequestScope(t, engine, "test.module")
	_ = scope.RequestStarted(t.Context(), module.RequestStarted{Method: "one"})
	_ = scope.RequestStarted(t.Context(), module.RequestStarted{Method: "two"})
	_ = scope.Finish(context.Background(), module.FinishCompleted)
	run.Close()
	if err := engine.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	records := sink.Records()
	if len(records) != 2 || records[0].Sequence != 1 || records[1].Sequence != 2 || records[0].RecordID != "trace:00000001" || records[0].Producer != "binding" {
		t.Fatalf("unexpected records: %#v", records)
	}
	if health := engine.ObservabilityHealth(); health.Status != "ok" || health.Modules["test.module"].Status != "ok" {
		t.Fatalf("unexpected health: %#v", health)
	}
}

func TestEngineContainsModulePanics(t *testing.T) {
	definition := testDefinition{name: "panic.module", open: func() module.Instance {
		return testInstance{handle: func(module.EventContext, module.RequestStarted) error { panic("boom") }}
	}}
	engine, err := observability.NewEngine(context.Background(), []module.Definition{definition}, observability.SinkConfig{})
	if err != nil {
		t.Fatal(err)
	}
	_, scope := openRequestScope(t, engine, "panic.module")
	if err := scope.RequestStarted(t.Context(), module.RequestStarted{}); err == nil {
		t.Fatal("module panic did not fail the barrier")
	}
	health := engine.ObservabilityHealth()
	if health.Status != "degraded" || health.Modules["panic.module"].Status != "degraded" {
		t.Fatalf("module panic did not degrade health: %#v", health)
	}
}

func openRequestScope(t *testing.T, engine *observability.Engine, moduleName string) (*observability.Run, *module.Scope) {
	t.Helper()
	compiled, err := engine.Compile(module.LifetimeRequest, []module.Spec{{ID: "binding", Module: moduleName, Config: []byte(`{}`)}})
	if err != nil {
		t.Fatal(err)
	}
	run := engine.StartRun("trace")
	scope, err := run.OpenScope(module.OpenContext{}, compiled)
	if err != nil {
		t.Fatal(err)
	}
	return run, scope
}
