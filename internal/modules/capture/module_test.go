package capture

import (
	"context"
	"net/http"
	"testing"

	"github.com/lwmacct/260628-directive-proxy/internal/core/bodymemory"
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

type captureOutputFactory struct{ output *captureOutput }

func (factory captureOutputFactory) Output(string, int) module.Output { return factory.output }
func (captureOutputFactory) ModuleFailed(string)                      {}

func TestRequestCaptureReferencesCanonicalBody(t *testing.T) {
	controller := bodymemory.New(bodymemory.Config{MaxActiveBytes: 16, MaxBodyBytes: 16})
	reservation, err := controller.Reserve(t.Context(), 7)
	if err != nil {
		t.Fatal(err)
	}
	body := bodymemory.NewBody([]byte("payload"), reservation)
	output := &captureOutput{}
	scope := configuredScope(t, `{"body-chunk-bytes":4}`, output)
	if err := scope.RequestBodyAvailable(t.Context(), module.RequestBodyAvailable{Body: body}); err != nil {
		t.Fatal(err)
	}
	if len(output.items) != 2 {
		t.Fatalf("unexpected body records: %#v", output.items)
	}
	body.Release()
	if snapshot := controller.Snapshot(); snapshot.UsedBytes != 7 {
		t.Fatalf("capture did not retain canonical body: %#v", snapshot)
	}
	first := output.items[0].data["data"].([]byte)
	if string(first) != "payl" || output.items[0].data["encoding"] != "binary" {
		t.Fatalf("unexpected binary body record: %#v", output.items[0])
	}
	for _, item := range output.items {
		item.release()
	}
	if snapshot := controller.Snapshot(); snapshot.UsedBytes != 0 {
		t.Fatalf("capture body leases were not released: %#v", snapshot)
	}
	_ = scope.Finish(context.Background(), module.FinishCompleted)
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
	}}, captureOutputFactory{output: output})
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
