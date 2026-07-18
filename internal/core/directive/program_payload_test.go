package directive

import (
	"testing"

	"github.com/lwmacct/260628-directive-proxy/internal/core/program"
)

func TestPayloadRoundTripsOrderedModuleProgram(t *testing.T) {
	token, err := Encode(testTokenSecret, Payload{
		Metadata: testDirectiveMetadata(),
		Target:   TargetSection{BaseURL: "https://api.example.com/v1/responses"},
		Program: program.Program{
			{Module: "builtin.capture", Config: []byte(`{}`)},
			{Module: "builtin.llmusage", Config: []byte(`{"protocol":"openai.responses"}`)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	document, err := Decode(testTokenSecret, token)
	if err != nil {
		t.Fatal(err)
	}
	if document.Payload == nil || len(document.Payload.Program) != 2 ||
		document.Payload.Program[0].Module != "builtin.capture" || document.Payload.Program[1].Module != "builtin.llmusage" {
		t.Fatalf("unexpected module program: %#v", document)
	}
	compiled, err := CompilePayload(*document.Payload, AssembleOptions{})
	if err != nil || compiled.Plan == nil {
		t.Fatalf("payload did not compile to proxy plan: %#v err=%v", compiled.Plan, err)
	}
}

func TestPayloadRejectsGroupedLegacyProgram(t *testing.T) {
	_, err := DecodePayload([]byte(`{"target":{"base_url":"https://api.example.com"},"program":{"request":[{"id":"capture","module":"builtin.capture"}]}}`))
	if err == nil {
		t.Fatal("legacy grouped program was accepted")
	}
}

func TestPayloadRejectsDuplicateProgramModule(t *testing.T) {
	_, err := DecodePayload([]byte(`{"target":{"base_url":"https://api.example.com"},"program":[{"module":"builtin.capture"},{"module":"builtin.capture"}]}`))
	if err == nil {
		t.Fatal("duplicate program module was accepted")
	}
}

func TestPayloadRejectsRemovedProgramFields(t *testing.T) {
	for _, field := range []string{
		`{"scope":"exchange","module":"builtin.capture"}`,
		`{"id":"capture","module":"builtin.capture"}`,
	} {
		_, err := DecodePayload([]byte(`{"target":{"base_url":"https://api.example.com"},"program":[` + field + `]}`))
		if err == nil {
			t.Fatalf("removed program field was accepted: %s", field)
		}
	}
}
