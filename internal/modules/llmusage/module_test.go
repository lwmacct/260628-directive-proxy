package llmusage

import (
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
)

type emittedRecord struct {
	topic   string
	attempt int
	data    map[string]any
}

type recordingFactory struct{ records []emittedRecord }
type recordingOutput struct {
	factory *recordingFactory
	attempt int
}

func (factory *recordingFactory) Output(_ string, attempt int) module.Output {
	return recordingOutput{factory: factory, attempt: attempt}
}
func (*recordingFactory) ModuleFailed(string) {}

func (output recordingOutput) Emit(topic string, data map[string]any) bool {
	output.factory.records = append(output.factory.records, emittedRecord{topic: topic, attempt: output.attempt, data: data})
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
	scope, records := configuredScope(t, `{"protocol":"openai.responses","labels":{"provider":"openai"}}`, 2)
	header := make(http.Header)
	header.Set("Content-Type", "text/event-stream")
	_ = scope.UpstreamResponseStarted(t.Context(), module.ResponseStarted{StatusCode: http.StatusOK, Header: header})
	_ = scope.UpstreamSSEData(t.Context(), module.SSEData{
		Event: "response.completed",
		Data:  []byte(`{"type":"response.completed","response":{"id":"resp_1","model":"gpt-test","usage":{"input_tokens":8,"output_tokens":5,"total_tokens":13}}}`),
	})
	_ = scope.UpstreamBodyEnded(t.Context(), module.BodyEnded{Cause: io.EOF})
	if err := scope.Finish(context.Background(), module.FinishCompleted); err != nil {
		t.Fatal(err)
	}
	if len(records.records) != 1 || records.records[0].topic != "llm.usage.observed" || records.records[0].attempt != 2 {
		t.Fatalf("unexpected records: %#v", records.records)
	}
	usage := records.records[0].data["usage"].(map[string]any)
	if usage["total_tokens"] != int64(13) || records.records[0].data["response_id"] != "resp_1" {
		t.Fatalf("unexpected usage: %#v", records.records[0].data)
	}
}

func TestModuleEmitsNotObservedForChatStreamWithoutUsage(t *testing.T) {
	scope, records := configuredScope(t, `{"protocol":"openai.chat-completions"}`, 1)
	header := make(http.Header)
	header.Set("Content-Type", "text/event-stream")
	_ = scope.UpstreamResponseStarted(t.Context(), module.ResponseStarted{StatusCode: http.StatusOK, Header: header})
	_ = scope.UpstreamSSEData(t.Context(), module.SSEData{Data: []byte("[DONE]")})
	_ = scope.UpstreamBodyEnded(t.Context(), module.BodyEnded{Cause: io.EOF})
	if err := scope.Finish(context.Background(), module.FinishCompleted); err != nil {
		t.Fatal(err)
	}
	if len(records.records) != 1 || records.records[0].topic != "llm.usage.not_observed" {
		t.Fatalf("unexpected records: %#v", records.records)
	}
}

func TestModuleRejectsUnknownConfigFields(t *testing.T) {
	if _, err := New().Compile([]byte(`{"protocol":"auto","unknown":true}`)); err == nil {
		t.Fatal("unknown config field was accepted")
	}
}

func TestModuleAcceptsResourceLimits(t *testing.T) {
	raw := []byte(`{"protocol":"auto","max-sse-metadata-bytes":1024,"max-result-bytes":4096,"max-nesting-depth":32}`)
	compiled, err := New().Compile(raw)
	if err != nil {
		t.Fatalf("resource limits were rejected: %v", err)
	}
	configured := compiled.(binding)
	if configured.spec.MaxSSEMetadataBytes != 1024 || configured.spec.MaxResultBytes != 4096 || configured.spec.MaxNestingDepth != 32 {
		t.Fatalf("resource limits were not applied: %#v", configured.spec)
	}
}

func configuredScope(t *testing.T, raw string, attempt int) (*module.Scope, *recordingFactory) {
	t.Helper()
	compiled, err := New().Compile([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	records := &recordingFactory{}
	scope, err := module.OpenScope(module.OpenContext{TraceID: "trace", Attempt: attempt}, []module.Compiled{{
		Spec: module.Spec{ID: "usage", Module: Name, Config: []byte(raw)}, Binding: compiled,
	}}, records)
	if err != nil {
		t.Fatal(err)
	}
	return scope, records
}
