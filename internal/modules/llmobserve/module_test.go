package llmobserve

import (
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/lwmacct/260628-directive-proxy/internal/core/event"
	"github.com/lwmacct/260628-directive-proxy/internal/core/lifecycle"
	"github.com/lwmacct/260628-directive-proxy/internal/core/metadata"
	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
	"github.com/lwmacct/260628-directive-proxy/internal/core/program"
)

type emittedRecord struct {
	topic     string
	roundTrip int
	data      map[string]any
}

type recordingFactory struct{ records []emittedRecord }
type recordingOutput struct {
	factory   *recordingFactory
	roundTrip int
}

func (factory *recordingFactory) Open(string, metadata.Set) event.Session { return factory }
func (factory *recordingFactory) Emitter(_ string, roundTrip int) event.Emitter {
	return recordingOutput{factory: factory, roundTrip: roundTrip}
}
func (*recordingFactory) Close() {}

func (output recordingOutput) Emit(topic string, data map[string]any) bool {
	output.factory.records = append(output.factory.records, emittedRecord{topic: topic, roundTrip: output.roundTrip, data: data})
	return true
}
func (output recordingOutput) EmitOwned(topic string, data map[string]any, release func()) bool {
	accepted := output.Emit(topic, data)
	if release != nil {
		release()
	}
	return accepted
}
func (output recordingOutput) EmitBorrowed(topic string, data map[string]any) bool {
	return output.Emit(topic, data)
}

func TestModuleObservesUsageAndPerformanceFromOneRawSSEPort(t *testing.T) {
	scope, records := configuredScope(t, `{"protocol":"auto","observe":["usage","performance"]}`, 1)
	if err := scope.UpstreamStarted(t.Context(), lifecycle.UpstreamStarted{}); err != nil {
		t.Fatal(err)
	}
	header := make(http.Header)
	header.Set("Content-Type", "text/event-stream")
	if err := scope.UpstreamResponseStarted(t.Context(), lifecycle.ResponseStarted{StatusCode: http.StatusOK, Header: header}); err != nil {
		t.Fatal(err)
	}
	body := []byte("event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"Hello\"}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"model\":\"gpt-test\",\"output\":[{\"text\":\"Hello\"}],\"usage\":{\"input_tokens\":8,\"output_tokens\":5,\"total_tokens\":13}}}\n\n")
	if err := scope.UpstreamBodyChunk(t.Context(), lifecycle.BodyChunk{Data: body}); err != nil {
		t.Fatal(err)
	}
	if err := scope.UpstreamBodyEnded(t.Context(), lifecycle.BodyEnded{Cause: io.EOF}); err != nil {
		t.Fatal(err)
	}
	if err := scope.Finish(context.Background(), module.FinishCompleted); err != nil {
		t.Fatal(err)
	}

	milestoneKinds := make(map[string]bool)
	var usageRecord, observedRecord *emittedRecord
	for index := range records.records {
		record := &records.records[index]
		switch record.topic {
		case TopicMilestone:
			milestoneKinds[record.data["kind"].(string)] = true
		case TopicUsage:
			usageRecord = record
		case TopicObserved:
			observedRecord = record
		case TopicFailed:
			t.Fatalf("unexpected failure: %#v", record)
		}
	}
	for _, kind := range []string{"first_byte", "first_output", "first_text", "generation_completed"} {
		if !milestoneKinds[kind] {
			t.Fatalf("missing milestone %q: %#v", kind, records.records)
		}
	}
	if usageRecord == nil || observedRecord == nil {
		t.Fatalf("missing final records: %#v", records.records)
	}
	usage := usageRecord.data["usage"].(map[string]any)
	if usage["total_tokens"] != int64(13) || usageRecord.data["response_id"] != "resp_1" || usageRecord.roundTrip != 1 {
		t.Fatalf("unexpected usage: %#v", usageRecord)
	}
	if observedRecord.data["protocol"] != "openai.responses" || observedRecord.data["terminal"] != "completed" {
		t.Fatalf("unexpected observation: %#v", observedRecord)
	}
}

func TestRecoveryRoundTripsUseIsolatedInstances(t *testing.T) {
	records := &recordingFactory{}
	runtime, err := program.NewRuntime(module.MustCatalog(New()), records)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	executable, err := runtime.Compile(module.Specs{{
		Module: Name, Config: []byte(`{"protocol":"openai.responses","observe":["usage"]}`),
	}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := runtime.StartRun("trace", executable, testMetadata(t))
	if err != nil {
		t.Fatal(err)
	}
	defer run.Close()

	first, err := run.OpenRoundTrip(module.OpenContext{RoundTrip: 1})
	if err != nil {
		t.Fatal(err)
	}
	firstScope := program.NewScopeSet(first)
	_ = firstScope.UpstreamStarted(t.Context(), lifecycle.UpstreamStarted{})
	_ = firstScope.UpstreamResponseStarted(t.Context(), lifecycle.ResponseStarted{StatusCode: http.StatusBadGateway, Header: http.Header{"Content-Type": {"application/json"}}})
	_ = firstScope.UpstreamBodyChunk(t.Context(), lifecycle.BodyChunk{Data: []byte(`{"error":"retry"}`)})
	_ = firstScope.UpstreamBodyEnded(t.Context(), lifecycle.BodyEnded{Cause: io.EOF})
	if err := firstScope.Finish(context.Background(), module.FinishCompleted); err != nil {
		t.Fatal(err)
	}

	second, err := run.OpenRoundTrip(module.OpenContext{RoundTrip: 2})
	if err != nil {
		t.Fatal(err)
	}
	secondScope := program.NewScopeSet(second)
	_ = secondScope.UpstreamStarted(t.Context(), lifecycle.UpstreamStarted{})
	_ = secondScope.UpstreamResponseStarted(t.Context(), lifecycle.ResponseStarted{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}})
	_ = secondScope.UpstreamBodyChunk(t.Context(), lifecycle.BodyChunk{Data: []byte(`{"id":"resp_2","usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}}`)})
	_ = secondScope.UpstreamBodyEnded(t.Context(), lifecycle.BodyEnded{Cause: io.EOF})
	if err := secondScope.Finish(context.Background(), module.FinishCompleted); err != nil {
		t.Fatal(err)
	}

	var usageRecords int
	for _, record := range records.records {
		if record.topic == TopicUsage {
			usageRecords++
			if record.roundTrip != 2 || record.data["response_id"] != "resp_2" {
				t.Fatalf("round-trip state leaked: %#v", record)
			}
		}
	}
	if usageRecords != 1 {
		t.Fatalf("unexpected usage records: %#v", records.records)
	}
}

func TestModuleConfigIsStrictAndBreaking(t *testing.T) {
	invalid := []string{
		`{"protocol":"auto"}`,
		`{"protocol":"auto","observe":[]}`,
		`{"protocol":"auto","observe":["usage","usage"]}`,
		`{"protocol":"auto","observe":["tokens"]}`,
		`{"protocol":"auto","observe":["usage"],"max-retained-bytes":1}`,
		`{"protocol":"auto","observe":["usage"],"limits":{"unknown":1}}`,
		`{"protocol":"auto","protocol":"openai.responses","observe":["usage"]}`,
		`{"Protocol":"auto","observe":["usage"]}`,
		`{"protocol":"auto","observe":["usage"]} {}`,
	}
	for _, raw := range invalid {
		if _, err := New().CompileProgram([]byte(raw)); err == nil {
			t.Fatalf("invalid config was accepted: %s", raw)
		}
	}
	raw := []byte(`{"protocol":"auto","observe":["usage","performance"],"limits":{"max-sse-metadata-bytes":1024,"max-retained-bytes":4096,"max-nesting-depth":32,"max-object-members":128,"max-key-bytes":256}}`)
	compiled, err := New().CompileProgram(raw)
	if err != nil {
		t.Fatal(err)
	}
	configured := compiled.(binding)
	if configured.spec.Limits.MaxRetainedBytes != 4096 || configured.spec.Limits.MaxObjectMembers != 128 || len(configured.spec.Observe) != 2 {
		t.Fatalf("limits were not applied: %#v", configured.spec)
	}
	if New().Lifetime() != module.LifetimeRoundTrip {
		t.Fatal("llm did not declare round-trip lifetime")
	}
}

func configuredScope(t *testing.T, raw string, roundTrip int) (*program.ScopeSet, *recordingFactory) {
	t.Helper()
	records := &recordingFactory{}
	runtime, err := program.NewRuntime(module.MustCatalog(New()), records)
	if err != nil {
		t.Fatal(err)
	}
	executable, err := runtime.Compile(module.Specs{{Module: Name, Config: []byte(raw)}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := runtime.StartRun("trace", executable, testMetadata(t))
	if err != nil {
		t.Fatal(err)
	}
	scope, err := run.OpenRoundTrip(module.OpenContext{RoundTrip: roundTrip})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		run.Close()
		runtime.Close()
	})
	return program.NewScopeSet(scope), records
}

func testMetadata(t *testing.T) metadata.Set {
	t.Helper()
	fields, err := metadata.Compile(map[string]string{metadata.KeyUserKey: "uk_test"})
	if err != nil {
		t.Fatal(err)
	}
	return fields
}
