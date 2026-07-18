package directive

import (
	"testing"

	"github.com/lwmacct/260628-directive-proxy/internal/core/program"
)

func TestPayloadRoundTripsOrderedModuleProgram(t *testing.T) {
	token, err := Encode(testTokenSecret, Payload{
		Target: TargetSection{BaseURL: "https://api.example.com/v1/responses"},
		Program: program.Program{
			Request: []program.Spec{{ID: "capture", Module: "builtin.capture", Config: []byte(`{}`)}},
			Attempt: []program.Spec{{ID: "usage", Module: "builtin.llmusage", Config: []byte(`{"protocol":"openai.responses"}`)}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	document, err := Decode(testTokenSecret, token)
	if err != nil {
		t.Fatal(err)
	}
	if document.Payload == nil || len(document.Payload.Program.Request) != 1 || len(document.Payload.Program.Attempt) != 1 || document.Payload.Program.Attempt[0].ID != "usage" {
		t.Fatalf("unexpected module program: %#v", document)
	}
	plan, _, err := CompilePayload(*document.Payload, AssembleOptions{})
	if err != nil || plan == nil {
		t.Fatalf("payload did not compile to proxy plan: %#v err=%v", plan, err)
	}
}
