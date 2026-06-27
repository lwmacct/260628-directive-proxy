package capture

import (
	"encoding/json"
	"testing"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/proxyplan"
)

func TestNextSSEEvent(t *testing.T) {
	event, rest, ok := nextSSEEvent([]byte("data: one\n\nrest"))
	if !ok {
		t.Fatal("expected event")
	}
	if string(event) != "data: one\n\n" {
		t.Fatalf("unexpected event: %q", string(event))
	}
	if string(rest) != "rest" {
		t.Fatalf("unexpected rest: %q", string(rest))
	}
}

func TestParseSSEEvent(t *testing.T) {
	parsed := parseSSEEvent([]byte("event: delta\nid: 42\ndata: a\ndata: b\n\n"))
	if parsed.Event != "delta" {
		t.Fatalf("unexpected event: %q", parsed.Event)
	}
	if parsed.ID != "42" {
		t.Fatalf("unexpected id: %q", parsed.ID)
	}
	if len(parsed.Data) != 2 || parsed.Data[0] != "a" || parsed.Data[1] != "b" {
		t.Fatalf("unexpected data: %#v", parsed.Data)
	}
}

func TestParsedSSEEventDataValueParsesJSON(t *testing.T) {
	parsed := parseSSEEvent([]byte(`event: delta
data: {"text":"hello","count":2}

`))

	data, ok := parsed.DataValue().(map[string]any)
	if !ok {
		t.Fatalf("expected JSON object data, got %#v", parsed.DataValue())
	}
	if data["text"] != "hello" || data["count"] != float64(2) {
		t.Fatalf("unexpected JSON data: %#v", data)
	}
}

func TestParsedSSEEventDataValueKeepsText(t *testing.T) {
	parsed := parseSSEEvent([]byte("event: delta\ndata: a\ndata: b\n\n"))

	if data := parsed.DataValue(); data != "a\nb" {
		t.Fatalf("unexpected text data: %#v", data)
	}
}

func TestStreamEventDataJSONOmitsRaw(t *testing.T) {
	raw, err := json.Marshal(&StreamEventData{
		Payload: map[string]any{"ok": true},
		Size:    10,
	})
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var encoded map[string]any
	if err := json.Unmarshal(raw, &encoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if _, ok := encoded["raw"]; ok {
		t.Fatalf("did not expect raw field: %s", string(raw))
	}
}

func TestResolveStreamPolicySSEEnablesSSEParsing(t *testing.T) {
	policy := resolveStreamPolicy(proxyplan.CapturePolicy{
		Configured:   true,
		StreamEvents: true,
	}, "text/event-stream")
	if !policy.parseSSE {
		t.Fatal("expected SSE parsing to be enabled")
	}
}
