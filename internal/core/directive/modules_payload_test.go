package directive

import (
	"testing"

	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
)

func TestPayloadRoundTripsOrderedModules(t *testing.T) {
	token, err := Encode(testTokenSecret, Payload{
		Metadata: testDirectiveMetadata(),
		Target:   TargetSection{BaseURL: "https://api.example.com/v1/responses"},
		Modules: module.Specs{
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
	if document.Payload == nil || len(document.Payload.Modules) != 2 ||
		document.Payload.Modules[0].Module != "builtin.capture" || document.Payload.Modules[1].Module != "builtin.llmusage" {
		t.Fatalf("unexpected modules: %#v", document)
	}
	compiled, err := CompilePayload(*document.Payload, AssembleOptions{})
	if err != nil || compiled.Plan == nil {
		t.Fatalf("payload did not compile to proxy plan: %#v err=%v", compiled.Plan, err)
	}
}

func TestPayloadRejectsRemovedProgramField(t *testing.T) {
	_, err := DecodePayload([]byte(`{"target":{"base_url":"https://api.example.com"},"program":[{"module":"builtin.capture"}]}`))
	if err == nil {
		t.Fatal("removed program field was accepted")
	}
}

func TestPayloadRejectsDuplicateModule(t *testing.T) {
	_, err := DecodePayload([]byte(`{"target":{"base_url":"https://api.example.com"},"modules":[{"module":"builtin.capture"},{"module":"builtin.capture"}]}`))
	if err == nil {
		t.Fatal("duplicate module was accepted")
	}
}

func TestPayloadRejectsRemovedModuleFields(t *testing.T) {
	for _, field := range []string{
		`{"scope":"exchange","module":"builtin.capture"}`,
		`{"id":"capture","module":"builtin.capture"}`,
	} {
		_, err := DecodePayload([]byte(`{"target":{"base_url":"https://api.example.com"},"modules":[` + field + `]}`))
		if err == nil {
			t.Fatalf("removed module field was accepted: %s", field)
		}
	}
}
