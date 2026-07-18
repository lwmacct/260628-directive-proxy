package llmperf

import (
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/lwmacct/260628-directive-proxy/internal/core/event"
	"github.com/lwmacct/260628-directive-proxy/internal/core/lifecycle"
	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
	"github.com/lwmacct/260628-directive-proxy/internal/core/program"
)

type emittedRecord struct {
	topic string
	data  map[string]any
}

type recordingFactory struct{ records []emittedRecord }
type recordingOutput struct{ factory *recordingFactory }

func (factory *recordingFactory) Open(string) event.Session { return factory }
func (factory *recordingFactory) Emitter(string, int) event.Emitter {
	return recordingOutput{factory: factory}
}
func (*recordingFactory) Close() {}

func (output recordingOutput) Emit(topic string, data map[string]any) bool {
	output.factory.records = append(output.factory.records, emittedRecord{topic: topic, data: data})
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

func TestModuleMeasuresOpenAIResponsesSSEFromRawPort(t *testing.T) {
	scope, records := configuredScope(t, `{"protocol":"openai.responses","labels":{"provider":"openai"}}`)
	_ = scope.UpstreamStarted(t.Context(), lifecycle.UpstreamStarted{})
	header := make(http.Header)
	header.Set("Content-Type", "text/event-stream")
	_ = scope.UpstreamResponseStarted(t.Context(), lifecycle.ResponseStarted{StatusCode: http.StatusOK, Header: header})
	body := []byte("event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"Hello\"}\n\nevent: response.completed\ndata: {\"type\":\"response.completed\"}\n\n")
	_ = scope.UpstreamBodyChunk(t.Context(), lifecycle.BodyChunk{Data: body})
	_ = scope.UpstreamBodyEnded(t.Context(), lifecycle.BodyEnded{Cause: io.EOF})
	if err := scope.Finish(context.Background(), module.FinishCompleted); err != nil {
		t.Fatal(err)
	}
	var sawFirstText, sawResult bool
	for _, record := range records.records {
		sawFirstText = sawFirstText || record.topic == "llm.perf.first_text"
		if record.topic == "llm.perf.observed" {
			sawResult = true
			if record.data["protocol"] != "openai.responses" || record.data["labels"].(map[string]string)["provider"] != "openai" {
				t.Fatalf("unexpected result: %#v", record.data)
			}
		}
	}
	if !sawFirstText || !sawResult {
		t.Fatalf("missing perf records: %#v", records.records)
	}
}

func TestModuleRejectsUnknownConfigFields(t *testing.T) {
	if _, err := New().Compile([]byte(`{"protocol":"auto","unknown":true}`)); err == nil {
		t.Fatal("unknown field was accepted")
	}
}

func TestModuleAcceptsResourceLimits(t *testing.T) {
	raw := []byte(`{"protocol":"auto","max-sse-metadata-bytes":1024,"max-retained-bytes":4096,"max-nesting-depth":32}`)
	compiled, err := New().Compile(raw)
	if err != nil {
		t.Fatalf("resource limits were rejected: %v", err)
	}
	configured := compiled.(binding)
	if configured.spec.MaxSSEMetadataBytes != 1024 || configured.spec.MaxRetainedBytes != 4096 || configured.spec.MaxNestingDepth != 32 {
		t.Fatalf("resource limits were not applied: %#v", configured.spec)
	}
}

func configuredScope(t *testing.T, raw string) (*program.Scope, *recordingFactory) {
	t.Helper()
	records := &recordingFactory{}
	runtime, err := program.NewRuntime([]module.Definition{New()}, records)
	if err != nil {
		t.Fatal(err)
	}
	executable, err := runtime.Compile(program.Program{Attempt: []program.Spec{{ID: "perf", Module: Name, Config: []byte(raw)}}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := runtime.StartRun("trace", executable)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := run.OpenAttempt(module.OpenContext{Attempt: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		run.Close()
		runtime.Close()
	})
	return scope, records
}
