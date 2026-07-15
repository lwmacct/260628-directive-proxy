package captureplugin

import (
	"net/http"
	"testing"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/bodymemory"
	"github.com/lwmacct/260628-directive-proxy/internal/core/observability"
)

type capturedEmission struct {
	topic   string
	data    map[string]any
	release func()
}

type captureEmitter struct{ items []capturedEmission }

func (e *captureEmitter) Emit(topic string, _ int, data map[string]any) {
	e.items = append(e.items, capturedEmission{topic: topic, data: data})
}

func (e *captureEmitter) EmitOwned(topic string, _ int, data map[string]any, release func()) {
	e.items = append(e.items, capturedEmission{topic: topic, data: data, release: release})
}

func TestRequestCaptureReferencesCanonicalBody(t *testing.T) {
	controller := bodymemory.New(bodymemory.Config{MaxActiveBytes: 16, MaxBodyBytes: 16})
	reservation, err := controller.Reserve(t.Context(), 7)
	if err != nil {
		t.Fatal(err)
	}
	body := bodymemory.NewBody([]byte("payload"), reservation)
	observer := New(Config{BodyChunkBytes: 4}).NewTrace(observability.TraceContext{})
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

func TestResponseCaptureDropsAtLimitAndReusesReleasedBudget(t *testing.T) {
	observer := New(Config{BodyChunkBytes: 4, MaxRetainedResponseBytes: 4, ResponseOverflow: "drop"}).NewTrace(observability.TraceContext{})
	emitter := &captureEmitter{}
	header := make(http.Header)
	header.Set("Content-Type", "application/octet-stream")
	observer.Observe(observability.Signal{Attempt: 1, Value: observability.DownstreamResponseStarted{StatusCode: http.StatusOK, Header: header}}, emitter)
	observer.Observe(observability.Signal{Attempt: 1, Value: observability.DownstreamBodyChunk{Data: []byte("first")}}, emitter)
	var firstOwned *capturedEmission
	for index := range emitter.items {
		if emitter.items[index].topic == "capture.response.body.chunk" {
			firstOwned = &emitter.items[index]
			break
		}
	}
	if firstOwned == nil || firstOwned.release == nil || firstOwned.data["encoding"] != "binary" {
		t.Fatalf("first response chunk was not retained: %#v", emitter.items)
	}
	if !hasTopic(emitter.items, "capture.response.body.gap") {
		t.Fatalf("overflow gap was not emitted: %#v", emitter.items)
	}
	firstOwned.release()
	before := len(emitter.items)
	observer.Observe(observability.Signal{Attempt: 1, Value: observability.DownstreamBodyChunk{Data: []byte("next")}}, emitter)
	if len(emitter.items) != before+1 || emitter.items[len(emitter.items)-1].topic != "capture.response.body.chunk" {
		t.Fatalf("released response budget was not reused: %#v", emitter.items[before:])
	}
	emitter.items[len(emitter.items)-1].release()
}

func TestResponseCaptureBackpressuresUntilOwnedChunkIsReleased(t *testing.T) {
	observer := New(Config{BodyChunkBytes: 4, MaxRetainedResponseBytes: 4, ResponseOverflow: "backpressure"}).NewTrace(observability.TraceContext{})
	emitter := &captureEmitter{}
	observer.Observe(observability.Signal{Attempt: 1, Value: observability.DownstreamResponseStarted{
		StatusCode: http.StatusOK, Header: make(http.Header),
	}}, emitter)
	observer.Observe(observability.Signal{Attempt: 1, Value: observability.DownstreamBodyChunk{Data: []byte("init")}}, emitter)
	var firstRelease func()
	for _, item := range emitter.items {
		if item.topic == "capture.response.body.chunk" {
			firstRelease = item.release
			break
		}
	}
	if firstRelease == nil {
		t.Fatalf("first owned chunk missing: %#v", emitter.items)
	}

	done := make(chan struct{})
	go func() {
		observer.Observe(observability.Signal{Attempt: 1, Value: observability.DownstreamBodyChunk{Data: []byte("next")}}, emitter)
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("response capture did not apply backpressure")
	case <-time.After(20 * time.Millisecond):
	}
	firstRelease()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("response capture remained blocked after memory release")
	}
	if hasTopic(emitter.items, "capture.response.body.gap") {
		t.Fatalf("backpressure mode emitted a gap: %#v", emitter.items)
	}
	for index := len(emitter.items) - 1; index >= 0; index-- {
		if emitter.items[index].topic == "capture.response.body.chunk" && emitter.items[index].release != nil {
			emitter.items[index].release()
			break
		}
	}
}

func hasTopic(items []capturedEmission, topic string) bool {
	for _, item := range items {
		if item.topic == topic {
			return true
		}
	}
	return false
}
