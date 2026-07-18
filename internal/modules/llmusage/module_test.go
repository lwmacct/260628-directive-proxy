package llmusage

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

func TestModuleExtractsOpenAIResponsesFromSSEDataPort(t *testing.T) {
	scope, records := configuredScope(t, `{"protocol":"openai.responses","labels":{"provider":"openai"}}`, 1)
	header := make(http.Header)
	header.Set("Content-Type", "text/event-stream")
	_ = scope.UpstreamResponseStarted(t.Context(), lifecycle.ResponseStarted{StatusCode: http.StatusOK, Header: header})
	_ = scope.UpstreamSSEData(t.Context(), lifecycle.SSEData{
		Event: "response.completed",
		Data:  []byte(`{"type":"response.completed","response":{"id":"resp_1","model":"gpt-test","usage":{"input_tokens":8,"output_tokens":5,"total_tokens":13}}}`),
	})
	_ = scope.UpstreamBodyEnded(t.Context(), lifecycle.BodyEnded{Cause: io.EOF})
	if err := scope.Finish(context.Background(), module.FinishCompleted); err != nil {
		t.Fatal(err)
	}
	if len(records.records) != 1 || records.records[0].topic != "llm.usage.observed" || records.records[0].roundTrip != 1 {
		t.Fatalf("unexpected records: %#v", records.records)
	}
	usage := records.records[0].data["usage"].(map[string]any)
	if usage["total_tokens"] != int64(13) || records.records[0].data["response_id"] != "resp_1" {
		t.Fatalf("unexpected usage: %#v", records.records[0].data)
	}
}

func TestRecoveryRoundTripsUseIsolatedUsageInstances(t *testing.T) {
	records := &recordingFactory{}
	runtime, err := program.NewRuntime(module.MustCatalog(New()), records)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	executable, err := runtime.Compile(program.Program{{Module: Name, Config: []byte(`{"protocol":"openai.responses"}`)}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := runtime.StartRun("trace", executable, usageTestMetadata(t))
	if err != nil {
		t.Fatal(err)
	}
	defer run.Close()

	first, err := run.OpenRoundTrip(module.OpenContext{RoundTrip: 1})
	if err != nil {
		t.Fatal(err)
	}
	firstScope := program.NewScopeSet(first)
	_ = firstScope.UpstreamResponseStarted(t.Context(), lifecycle.ResponseStarted{StatusCode: http.StatusBadGateway, Header: http.Header{"Content-Type": {"application/json"}}})
	_ = firstScope.UpstreamJSONChunk(t.Context(), lifecycle.BodyChunk{Data: []byte(`{"error":"retry"}`)})
	_ = firstScope.UpstreamBodyEnded(t.Context(), lifecycle.BodyEnded{Cause: io.EOF})
	if err := firstScope.Finish(context.Background(), module.FinishCompleted); err != nil {
		t.Fatal(err)
	}

	second, err := run.OpenRoundTrip(module.OpenContext{RoundTrip: 2})
	if err != nil {
		t.Fatal(err)
	}
	secondScope := program.NewScopeSet(second)
	_ = secondScope.UpstreamResponseStarted(t.Context(), lifecycle.ResponseStarted{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}})
	_ = secondScope.UpstreamJSONChunk(t.Context(), lifecycle.BodyChunk{Data: []byte(`{"id":"resp_2","usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}}`)})
	_ = secondScope.UpstreamBodyEnded(t.Context(), lifecycle.BodyEnded{Cause: io.EOF})
	if err := secondScope.Finish(context.Background(), module.FinishCompleted); err != nil {
		t.Fatal(err)
	}

	if len(records.records) != 1 || records.records[0].roundTrip != 2 || records.records[0].topic != "llm.usage.observed" {
		t.Fatalf("round-trip instances leaked state: %#v", records.records)
	}
}

func TestModuleEmitsNotObservedForChatStreamWithoutUsage(t *testing.T) {
	scope, records := configuredScope(t, `{"protocol":"openai.chat-completions"}`, 1)
	header := make(http.Header)
	header.Set("Content-Type", "text/event-stream")
	_ = scope.UpstreamResponseStarted(t.Context(), lifecycle.ResponseStarted{StatusCode: http.StatusOK, Header: header})
	_ = scope.UpstreamSSEData(t.Context(), lifecycle.SSEData{Data: []byte("[DONE]")})
	_ = scope.UpstreamBodyEnded(t.Context(), lifecycle.BodyEnded{Cause: io.EOF})
	if err := scope.Finish(context.Background(), module.FinishCompleted); err != nil {
		t.Fatal(err)
	}
	if len(records.records) != 1 || records.records[0].topic != "llm.usage.not_observed" {
		t.Fatalf("unexpected records: %#v", records.records)
	}
}

func TestModuleRejectsUnknownConfigFields(t *testing.T) {
	if _, err := New().CompileProgram([]byte(`{"protocol":"auto","unknown":true}`)); err == nil {
		t.Fatal("unknown config field was accepted")
	}
}

func TestModuleDeclaresRoundTripLifetime(t *testing.T) {
	if New().Lifetime() != module.LifetimeRoundTrip {
		t.Fatal("llmusage did not declare round-trip lifetime")
	}
}

func TestModuleAcceptsResourceLimits(t *testing.T) {
	raw := []byte(`{"protocol":"auto","max-sse-metadata-bytes":1024,"max-result-bytes":4096,"max-nesting-depth":32}`)
	compiled, err := New().CompileProgram(raw)
	if err != nil {
		t.Fatalf("resource limits were rejected: %v", err)
	}
	configured := compiled.(binding)
	if configured.spec.MaxSSEMetadataBytes != 1024 || configured.spec.MaxResultBytes != 4096 || configured.spec.MaxNestingDepth != 32 {
		t.Fatalf("resource limits were not applied: %#v", configured.spec)
	}
}

func configuredScope(t *testing.T, raw string, roundTrip int) (*program.ScopeSet, *recordingFactory) {
	t.Helper()
	records := &recordingFactory{}
	runtime, err := program.NewRuntime(module.MustCatalog(New()), records)
	if err != nil {
		t.Fatal(err)
	}
	executable, err := runtime.Compile(program.Program{{Module: Name, Config: []byte(raw)}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := runtime.StartRun("trace", executable, usageTestMetadata(t))
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

func usageTestMetadata(t *testing.T) metadata.Set {
	t.Helper()
	fields, err := metadata.Compile(map[string]string{metadata.KeyUserKey: "uk_test"})
	if err != nil {
		t.Fatal(err)
	}
	fields, err = fields.WithTraceID("trace")
	if err != nil {
		t.Fatal(err)
	}
	return fields
}
