package directive

import (
	"encoding/json"
	"testing"
)

func TestPayloadRoundTripsPluginSpecs(t *testing.T) {
	token, err := Encode(Payload{
		Target:  TargetSection{URL: "https://api.example.com/v1/responses"},
		Plugins: map[string]json.RawMessage{"llmusage": json.RawMessage(`{"protocol":"openai.responses"}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	document, err := Decode(token)
	if err != nil {
		t.Fatal(err)
	}
	if document.Payload == nil || string(document.Payload.Plugins["llmusage"]) != `{"protocol":"openai.responses"}` {
		t.Fatalf("unexpected plugin payload: %#v", document)
	}
	plan, err := ToPlan(*document.Payload, AssembleOptions{})
	if err != nil || string(plan.PluginSpecs["llmusage"]) != `{"protocol":"openai.responses"}` {
		t.Fatalf("unexpected plugin plan: %#v err=%v", plan, err)
	}
}
