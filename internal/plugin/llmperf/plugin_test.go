package llmperfplugin

import (
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/observability"
)

type emittedRecord struct {
	topic string
	data  map[string]any
}

type recordingEmitter struct{ records []emittedRecord }

func (e *recordingEmitter) Emit(topic string, _ int, data map[string]any) bool {
	e.records = append(e.records, emittedRecord{topic: topic, data: data})
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

func TestPluginMeasuresOpenAIResponsesSSE(t *testing.T) {
	start := time.Unix(100, 0)
	rawSpec := []byte(`{"protocol":"openai.responses","labels":{"provider":"openai"}}`)
	plugin, err := New().ConfigureSpec(rawSpec)
	if err != nil {
		t.Fatal(err)
	}
	observer := plugin.NewTrace(observability.TraceContext{TraceID: "trace"})
	emitter := &recordingEmitter{}
	observer.Observe(observability.Signal{Attempt: 1, ObservedAt: start, Value: observability.UpstreamStarted{}}, emitter)
	header := make(http.Header)
	header.Set("Content-Type", "text/event-stream")
	observer.Observe(observability.Signal{Attempt: 1, ObservedAt: start.Add(100 * time.Millisecond), Value: observability.UpstreamResponseStarted{StatusCode: http.StatusOK, Header: header}}, emitter)
	body := []byte("event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"Hello\"}\n\nevent: response.completed\ndata: {\"type\":\"response.completed\"}\n\n")
	observer.Observe(observability.Signal{Attempt: 1, ObservedAt: start.Add(500 * time.Millisecond), Value: observability.UpstreamBodyChunk{Data: body}}, emitter)
	observer.Observe(observability.Signal{Attempt: 1, ObservedAt: start.Add(time.Second), Value: observability.UpstreamBodyEnded{Cause: io.EOF}}, emitter)
	var sawFirstText, sawResult bool
	for _, record := range emitter.records {
		sawFirstText = sawFirstText || record.topic == "llm.perf.first_text"
		if record.topic == "llm.perf.observed" {
			sawResult = true
			if record.data["protocol"] != "openai.responses" || record.data["labels"].(map[string]string)["provider"] != "openai" {
				t.Fatalf("unexpected result: %#v", record.data)
			}
		}
	}
	if !sawFirstText || !sawResult {
		t.Fatalf("missing perf records: %#v", emitter.records)
	}
}

func TestPluginRejectsUnknownSpecFields(t *testing.T) {
	if err := New().ValidateSpec([]byte(`{"protocol":"auto","unknown":true}`)); err == nil {
		t.Fatal("unknown field was accepted")
	}
}

func TestPluginAcceptsDirectiveResourceLimits(t *testing.T) {
	raw := []byte(`{"protocol":"auto","max-sse-metadata-bytes":1024,"max-retained-bytes":4096,"max-nesting-depth":32}`)
	configured, err := New().ConfigureSpec(raw)
	if err != nil {
		t.Fatalf("directive resource limits were rejected: %v", err)
	}
	plugin := configured.(*Plugin)
	if plugin.spec.MaxSSEMetadataBytes != 1024 || plugin.spec.MaxRetainedBytes != 4096 || plugin.spec.MaxNestingDepth != 32 {
		t.Fatalf("directive resource limits were not applied: %#v", plugin.spec)
	}
}
