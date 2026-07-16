package directive

import (
	"testing"

	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
)

func TestPayloadRoundTripsOrderedModuleProgram(t *testing.T) {
	token, err := Encode(Payload{
		Target: TargetSection{URL: "https://api.example.com/v1/responses"},
		Program: module.Program{
			Request: []module.Spec{{ID: "capture", Module: "builtin.capture", Config: []byte(`{}`)}},
			Attempt: []module.Spec{{ID: "usage", Module: "builtin.llmusage", Config: []byte(`{"protocol":"openai.responses"}`)}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	document, err := Decode(token)
	if err != nil {
		t.Fatal(err)
	}
	if document.Payload == nil || len(document.Payload.Program.Request) != 1 || len(document.Payload.Program.Attempt) != 1 || document.Payload.Program.Attempt[0].ID != "usage" {
		t.Fatalf("unexpected module program: %#v", document)
	}
	plan, err := ToPlan(*document.Payload, AssembleOptions{})
	if err != nil || len(plan.Modules) != 1 || plan.Modules[0].Module != "builtin.llmusage" {
		t.Fatalf("unexpected attempt program: %#v err=%v", plan, err)
	}
}
