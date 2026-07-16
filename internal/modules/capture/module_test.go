package capture

import (
	"context"
	"net/http"
	"testing"

	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
)

type capturedEmission struct {
	topic   string
	data    map[string]any
	release func()
}

type captureOutput struct {
	items          []capturedEmission
	rejectBorrowed bool
}

func (output *captureOutput) Emit(topic string, data map[string]any) bool {
	output.items = append(output.items, capturedEmission{topic: topic, data: data})
	return true
}

func (output *captureOutput) EmitOwned(topic string, data map[string]any, release func()) bool {
	output.items = append(output.items, capturedEmission{topic: topic, data: data, release: release})
	return true
}

func (output *captureOutput) EmitBorrowed(topic string, data map[string]any) bool {
	if output.rejectBorrowed {
		return false
	}
	cloned := make(map[string]any, len(data))
	for key, value := range data {
		if bytes, ok := value.([]byte); ok {
			cloned[key] = append([]byte(nil), bytes...)
		} else {
			cloned[key] = value
		}
	}
	output.items = append(output.items, capturedEmission{topic: topic, data: cloned})
	return true
}

type captureRuntime struct{ output *captureOutput }

func (runtime captureRuntime) Emitter(string, int) module.Emitter { return runtime.output }
func (captureRuntime) ModuleFailed(string)                        {}

func TestRequestCaptureRechunksStreamingBody(t *testing.T) {
	output := &captureOutput{}
	scope := configuredScope(t, `{"body-chunk-bytes":4}`, output)
	if err := scope.RequestBodyChunk(t.Context(), module.BodyChunk{Data: []byte("pay")}); err != nil {
		t.Fatal(err)
	}
	if err := scope.RequestBodyChunk(t.Context(), module.BodyChunk{Data: []byte("load")}); err != nil {
		t.Fatal(err)
	}
	if err := scope.RequestBodyEnded(t.Context(), module.RequestBodyEnded{Total: 7, Complete: true}); err != nil {
		t.Fatal(err)
	}
	if err := scope.Finish(context.Background(), module.FinishCompleted); err != nil {
		t.Fatal(err)
	}
	if len(output.items) != 3 {
		t.Fatalf("unexpected body records: %#v", output.items)
	}
	first := output.items[0].data["data"].([]byte)
	if string(first) != "payl" || output.items[0].data["encoding"] != "binary" {
		t.Fatalf("unexpected binary body record: %#v", output.items[0])
	}
	second := output.items[1].data["data"].([]byte)
	if string(second) != "oad" {
		t.Fatalf("unexpected trailing body record: %#v", output.items[1])
	}
}

func TestResponseCaptureReportsOutputQueueDropsWithoutBlocking(t *testing.T) {
	output := &captureOutput{rejectBorrowed: true}
	scope := configuredScope(t, `{"body-chunk-bytes":4}`, output)
	header := make(http.Header)
	header.Set("Content-Type", "application/octet-stream")
	_ = scope.DownstreamResponseStarted(t.Context(), module.ResponseStarted{StatusCode: http.StatusOK, Header: header})
	_ = scope.DownstreamBodyChunk(t.Context(), module.BodyChunk{Data: []byte("first")})
	_ = scope.DownstreamBodyEnded(t.Context(), module.BodyEnded{})
	if err := scope.Finish(context.Background(), module.FinishCompleted); err != nil {
		t.Fatal(err)
	}
	if !hasTopic(output.items, "capture.response.body.gap") {
		t.Fatalf("queue overflow gap was not emitted: %#v", output.items)
	}
	for _, item := range output.items {
		if item.topic == "capture.response.body.end" && item.data["dropped_bytes"] != int64(5) {
			t.Fatalf("unexpected dropped byte count: %#v", item.data)
		}
	}
}

func TestModuleRejectsUnknownConfigFields(t *testing.T) {
	if _, err := New().Compile([]byte(`{"unknown":true}`)); err == nil {
		t.Fatal("unknown config field was accepted")
	}
}

func configuredScope(t *testing.T, raw string, output *captureOutput) *module.Scope {
	t.Helper()
	binding, err := New().Compile([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	scope, err := module.OpenScope(module.OpenContext{TraceID: "trace"}, []module.Compiled{{
		Spec: module.Spec{ID: "capture", Module: Name, Config: []byte(raw)}, Binding: binding,
	}}, captureRuntime{output: output})
	if err != nil {
		t.Fatal(err)
	}
	return scope
}

func hasTopic(items []capturedEmission, topic string) bool {
	for _, item := range items {
		if item.topic == topic {
			return true
		}
	}
	return false
}
