package jsoncapture

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCaptureIncludeFieldsAtObjectPath(t *testing.T) {
	raw := `{"type":"response.completed","response":{"id":"resp_1","model":"gpt-5.5","output":[{"big":"ignored"}],"usage":{"input_tokens":1,"extra":{"ok":true}}}}`

	result, err := Capture(strings.NewReader(raw), Options{
		ObjectPath: []string{"response"},
		Mode:       ModeInclude,
		Fields:     []string{"id", "usage"},
	})
	if err != nil {
		t.Fatalf("capture failed: %v", err)
	}
	if !result.Found {
		t.Fatal("expected target object")
	}
	if string(result.Fields["id"]) != `"resp_1"` {
		t.Fatalf("unexpected id: %s", result.Fields["id"])
	}
	if _, ok := result.Fields["model"]; ok {
		t.Fatalf("did not expect model: %#v", result.Fields)
	}
	var usage map[string]any
	if err := json.Unmarshal(result.Fields["usage"], &usage); err != nil {
		t.Fatalf("unmarshal usage failed: %v", err)
	}
	if usage["input_tokens"] != float64(1) {
		t.Fatalf("unexpected usage: %#v", usage)
	}
}

func TestCaptureExcludeFieldsAtObjectPath(t *testing.T) {
	raw := `{"response":{"instructions":"skip","tools":[{"name":"x"}],"output":[{"text":"keep"}],"usage":{"total_tokens":3}}}`

	result, err := Capture(strings.NewReader(raw), Options{
		ObjectPath: []string{"response"},
		Mode:       ModeExclude,
		Fields:     []string{"instructions", "tools"},
	})
	if err != nil {
		t.Fatalf("capture failed: %v", err)
	}
	if _, ok := result.Fields["instructions"]; ok {
		t.Fatalf("did not expect instructions: %#v", result.Fields)
	}
	if _, ok := result.Fields["tools"]; ok {
		t.Fatalf("did not expect tools: %#v", result.Fields)
	}
	if string(result.Fields["output"]) != `[{"text":"keep"}]` {
		t.Fatalf("unexpected output: %s", result.Fields["output"])
	}
}

func TestCaptureRootObject(t *testing.T) {
	raw := `{"id":"root","nested":{"id":"nope"},"usage":{"total_tokens":1}}`

	result, err := Capture(strings.NewReader(raw), Options{
		Mode:   ModeInclude,
		Fields: []string{"id", "usage"},
	})
	if err != nil {
		t.Fatalf("capture failed: %v", err)
	}
	if string(result.Fields["id"]) != `"root"` {
		t.Fatalf("unexpected root id: %s", result.Fields["id"])
	}
	if _, ok := result.Fields["nested"]; ok {
		t.Fatalf("did not expect nested: %#v", result.Fields)
	}
}
