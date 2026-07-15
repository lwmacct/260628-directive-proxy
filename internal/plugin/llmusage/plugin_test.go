package llmusageplugin

import (
	"io"
	"net/http"
	"testing"

	"github.com/lwmacct/260628-directive-proxy/internal/core/observability"
)

type emittedRecord struct {
	topic   string
	attempt int
	data    map[string]any
}

type recordingEmitter struct{ records []emittedRecord }

func (e *recordingEmitter) Emit(topic string, attempt int, data map[string]any) bool {
	e.records = append(e.records, emittedRecord{topic: topic, attempt: attempt, data: data})
	return true
}

func (e *recordingEmitter) EmitOwned(topic string, attempt int, data map[string]any, release func()) bool {
	e.Emit(topic, attempt, data)
	if release != nil {
		release()
	}
	return true
}

func (e *recordingEmitter) EmitBorrowed(topic string, attempt int, data map[string]any) bool {
	return e.Emit(topic, attempt, data)
}

func TestPluginExtractsOpenAIResponsesSSEUsage(t *testing.T) {
	rawSpec := []byte(`{"protocol":"openai.responses","labels":{"provider":"openai"}}`)
	plugin, err := New().ConfigureSpec(rawSpec)
	if err != nil {
		t.Fatal(err)
	}
	observer := plugin.NewTrace(observability.TraceContext{TraceID: "trace"})
	emitter := &recordingEmitter{}
	header := make(http.Header)
	header.Set("Content-Type", "text/event-stream")
	observer.Observe(observability.Signal{Attempt: 2, Value: observability.UpstreamResponseStarted{
		StatusCode: http.StatusOK, Header: header,
	}}, emitter)
	body := []byte("event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"model\":\"gpt-test\",\"usage\":{\"input_tokens\":8,\"output_tokens\":5,\"total_tokens\":13}}}\n\n")
	observer.Observe(observability.Signal{Attempt: 2, Value: observability.UpstreamBodyChunk{Data: body[:17]}}, emitter)
	observer.Observe(observability.Signal{Attempt: 2, Value: observability.UpstreamBodyChunk{Data: body[17:]}}, emitter)
	observer.Observe(observability.Signal{Attempt: 2, Value: observability.UpstreamBodyEnded{Cause: io.EOF}}, emitter)
	if len(emitter.records) != 1 || emitter.records[0].topic != "llm.usage.observed" || emitter.records[0].attempt != 2 {
		t.Fatalf("unexpected records: %#v", emitter.records)
	}
	usage := emitter.records[0].data["usage"].(map[string]any)
	if usage["total_tokens"] != int64(13) || emitter.records[0].data["response_id"] != "resp_1" {
		t.Fatalf("unexpected usage: %#v", emitter.records[0].data)
	}
}

func TestPluginEmitsNotObservedForChatStreamWithoutUsage(t *testing.T) {
	rawSpec := []byte(`{"protocol":"openai.chat-completions"}`)
	plugin, err := New().ConfigureSpec(rawSpec)
	if err != nil {
		t.Fatal(err)
	}
	observer := plugin.NewTrace(observability.TraceContext{TraceID: "trace"})
	emitter := &recordingEmitter{}
	header := make(http.Header)
	header.Set("Content-Type", "text/event-stream")
	observer.Observe(observability.Signal{Attempt: 1, Value: observability.UpstreamResponseStarted{
		StatusCode: http.StatusOK, Header: header,
	}}, emitter)
	observer.Observe(observability.Signal{Attempt: 1, Value: observability.UpstreamBodyChunk{Data: []byte("data: [DONE]\n\n")}}, emitter)
	observer.Observe(observability.Signal{Attempt: 1, Value: observability.UpstreamBodyEnded{Cause: io.EOF}}, emitter)
	if len(emitter.records) != 1 || emitter.records[0].topic != "llm.usage.not_observed" {
		t.Fatalf("unexpected records: %#v", emitter.records)
	}
}

func TestPluginRejectsUnknownSpecFields(t *testing.T) {
	if err := New().ValidateSpec([]byte(`{"protocol":"auto","unknown":true}`)); err == nil {
		t.Fatal("unknown spec field was accepted")
	}
}

func TestPluginAcceptsDirectiveResourceLimits(t *testing.T) {
	raw := []byte(`{"protocol":"auto","max-sse-metadata-bytes":1024,"max-result-bytes":4096,"max-nesting-depth":32}`)
	configured, err := New().ConfigureSpec(raw)
	if err != nil {
		t.Fatalf("directive resource limits were rejected: %v", err)
	}
	plugin := configured.(*Plugin)
	if plugin.spec.MaxSSEMetadataBytes != 1024 || plugin.spec.MaxResultBytes != 4096 || plugin.spec.MaxNestingDepth != 32 {
		t.Fatalf("directive resource limits were not applied: %#v", plugin.spec)
	}
}
