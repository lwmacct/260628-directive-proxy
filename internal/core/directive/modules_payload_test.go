package directive

import (
	"testing"

	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
)

func TestPayloadRoundTripsOrderedModules(t *testing.T) {
	token, err := Encode(testHMACSecret, Payload{
		Metadata: testDirectiveMetadata(),
		Target:   TargetSection{BaseURL: "https://api.example.com/v1/responses"},
		Modules: module.Specs{
			{Module: "capture", Config: []byte(`{}`)},
			{Module: "llmobserve", Config: []byte(`{"protocol":"openai.responses","observe":["usage"]}`)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	document, err := Decode(testHMACSecret, token)
	if err != nil {
		t.Fatal(err)
	}
	if document.Payload == nil || len(document.Payload.Modules) != 2 ||
		document.Payload.Modules[0].Module != "capture" || document.Payload.Modules[1].Module != "llmobserve" {
		t.Fatalf("unexpected modules: %#v", document)
	}
	compiled, err := CompilePayload(*document.Payload, AssembleOptions{})
	if err != nil || compiled.Plan == nil {
		t.Fatalf("payload did not compile to proxy plan: %#v err=%v", compiled.Plan, err)
	}
}

func TestPayloadRejectsDuplicateModule(t *testing.T) {
	_, err := DecodePayload([]byte(`{"target":{"base_url":"https://api.example.com"},"modules":[{"module":"capture"},{"module":"capture"}]}`))
	if err == nil {
		t.Fatal("duplicate module was accepted")
	}
}
