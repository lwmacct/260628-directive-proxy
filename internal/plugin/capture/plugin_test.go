package captureplugin

import (
	"net/http"
	"testing"

	"github.com/lwmacct/260628-directive-proxy/internal/core/bodymemory"
	"github.com/lwmacct/260628-directive-proxy/internal/core/observability"
)

type capturedEmission struct {
	topic   string
	data    map[string]any
	release func()
}

type captureEmitter struct {
	items          []capturedEmission
	rejectBorrowed bool
}

func (e *captureEmitter) Emit(topic string, _ int, data map[string]any) bool {
	e.items = append(e.items, capturedEmission{topic: topic, data: data})
	return true
}

func (e *captureEmitter) EmitOwned(topic string, _ int, data map[string]any, release func()) bool {
	e.items = append(e.items, capturedEmission{topic: topic, data: data, release: release})
	return true
}

func (e *captureEmitter) EmitBorrowed(topic string, _ int, data map[string]any) bool {
	if e.rejectBorrowed {
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
	e.items = append(e.items, capturedEmission{topic: topic, data: cloned})
	return true
}

func TestRequestCaptureReferencesCanonicalBody(t *testing.T) {
	controller := bodymemory.New(bodymemory.Config{MaxActiveBytes: 16, MaxBodyBytes: 16})
	reservation, err := controller.Reserve(t.Context(), 7)
	if err != nil {
		t.Fatal(err)
	}
	body := bodymemory.NewBody([]byte("payload"), reservation)
	observer := configuredObserver(t, `{"body-chunk-bytes":4}`)
	emitter := &captureEmitter{}
	observer.Observe(observability.Signal{Value: observability.RequestBodyAvailable{Body: body}}, emitter)
	if len(emitter.items) != 2 {
		t.Fatalf("unexpected body records: %#v", emitter.items)
	}
	body.Release()
	if snapshot := controller.Snapshot(); snapshot.UsedBytes != 7 {
		t.Fatalf("capture did not retain canonical body: %#v", snapshot)
	}
	first := emitter.items[0].data["data"].([]byte)
	if string(first) != "payl" || emitter.items[0].data["encoding"] != "binary" {
		t.Fatalf("unexpected binary body record: %#v", emitter.items[0])
	}
	for _, item := range emitter.items {
		item.release()
	}
	if snapshot := controller.Snapshot(); snapshot.UsedBytes != 0 {
		t.Fatalf("capture body leases were not released: %#v", snapshot)
	}
}

func TestResponseCaptureReportsOutputQueueDropsWithoutBlocking(t *testing.T) {
	observer := configuredObserver(t, `{"body-chunk-bytes":4}`)
	emitter := &captureEmitter{rejectBorrowed: true}
	header := make(http.Header)
	header.Set("Content-Type", "application/octet-stream")
	observer.Observe(observability.Signal{Attempt: 1, Value: observability.DownstreamResponseStarted{StatusCode: http.StatusOK, Header: header}}, emitter)
	observer.Observe(observability.Signal{Attempt: 1, Value: observability.DownstreamBodyChunk{Data: []byte("first")}}, emitter)
	if !hasTopic(emitter.items, "capture.response.body.gap") {
		t.Fatalf("queue overflow gap was not emitted: %#v", emitter.items)
	}
	observer.Observe(observability.Signal{Attempt: 1, Value: observability.DownstreamBodyEnded{}}, emitter)
	for _, item := range emitter.items {
		if item.topic == "capture.response.body.end" && item.data["dropped_bytes"] != int64(5) {
			t.Fatalf("unexpected dropped byte count: %#v", item.data)
		}
	}
}

func TestPluginRejectsUnknownDirectiveFields(t *testing.T) {
	if err := New().ValidateSpec([]byte(`{"unknown":true}`)); err == nil {
		t.Fatal("unknown directive field was accepted")
	}
}

func configuredObserver(t *testing.T, raw string) observability.TraceObserver {
	t.Helper()
	configured, err := New().ConfigureSpec([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return configured.NewTrace(observability.TraceContext{})
}

func hasTopic(items []capturedEmission, topic string) bool {
	for _, item := range items {
		if item.topic == topic {
			return true
		}
	}
	return false
}
