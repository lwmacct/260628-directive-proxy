package directive_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/lwmacct/260628-directive-proxy/pkg/directive"
)

func TestCanonicalTemplateAndCompletePayloadShareContract(t *testing.T) {
	t.Parallel()
	raw := []byte(`{
		"metadata":{"user_key":"tenant-a"},
		"headers":{"mutations":[{"side":"request","action":"set","name":"X-Tenant","values":["a"]}]},
		"modules":[{"module":"builtin.capture","config":{"enabled":true}}]
	}`)
	canonical, err := directive.CanonicalTemplate(raw)
	if err != nil {
		t.Fatalf("canonical template: %v", err)
	}
	payload, err := directive.DecodeTemplate(canonical)
	if err != nil {
		t.Fatalf("decode template: %v", err)
	}
	if payload.Target != (directive.TargetSection{}) {
		t.Fatalf("template unexpectedly contains target: %#v", payload.Target)
	}
	payload.Target = directive.TargetSection{BaseURL: "https://vendor.example/v1"}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal completed payload: %v", err)
	}
	decoded, err := directive.DecodePayload(encoded)
	if err != nil {
		t.Fatalf("decode completed payload: %v", err)
	}
	if !reflect.DeepEqual(decoded, payload) {
		t.Fatalf("payload changed after round trip:\nwant %#v\n got %#v", payload, decoded)
	}
}

func TestCanonicalTemplateRejectsTargetAndUnknownFields(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		`{"target":{"base_url":"https://vendor.example"}}`,
		`{"future_field":true}`,
		`null`,
	} {
		if _, err := directive.CanonicalTemplate([]byte(raw)); !errors.Is(err, directive.ErrInvalidPayload) {
			t.Fatalf("CanonicalTemplate(%s) error = %v", raw, err)
		}
	}
}

func TestHTTPRemoteSpecNormalizesAndRejectsInvalidPolicies(t *testing.T) {
	t.Parallel()
	spec, err := directive.NormalizeRemoteSpec(directive.RemoteSpec{HTTP: &directive.HTTPRemoteSpec{
		URL: " https://resolver.example/api/resolver ",
		Headers: &directive.HeaderPolicy{Mutations: []directive.HeaderMutation{{
			Side: directive.HeaderSideRequest, Action: directive.HeaderActionSet,
			Name: "Authorization", Values: []string{"Bearer dpr_test"},
		}}},
	}})
	if err != nil {
		t.Fatalf("normalize HTTP RemoteSpec: %v", err)
	}
	if spec.HTTP == nil || spec.HTTP.URL != "https://resolver.example/api/resolver" {
		t.Fatalf("unexpected normalized RemoteSpec: %#v", spec)
	}

	invalid := []directive.RemoteSpec{
		{},
		{HTTP: &directive.HTTPRemoteSpec{URL: "https://resolver.example"}, File: &directive.FileRemoteSpec{Path: "route.json"}},
		{HTTP: &directive.HTTPRemoteSpec{URL: "https://user:pass@resolver.example"}},
		{HTTP: &directive.HTTPRemoteSpec{URL: "https://resolver.example", Headers: &directive.HeaderPolicy{Mutations: []directive.HeaderMutation{{Side: directive.HeaderSideResponse, Action: directive.HeaderActionDel, Name: "Server"}}}}},
	}
	for _, value := range invalid {
		if err := directive.ValidateRemoteSpec(value); !errors.Is(err, directive.ErrInvalidPayload) {
			t.Fatalf("ValidateRemoteSpec(%#v) error = %v", value, err)
		}
	}
}

func TestDecodeRemoteSpecIsStrictOneOf(t *testing.T) {
	t.Parallel()
	valid := []byte(`{"http":{"url":"https://resolver.example/api/resolver"}}`)
	if _, err := directive.DecodeRemoteSpec(valid); err != nil {
		t.Fatalf("decode HTTP RemoteSpec: %v", err)
	}
	for _, raw := range []string{
		`{"http":null}`,
		`{"http":{"url":"https://resolver.example"},"redis":{"url":"redis://redis.example:6379/0","key":"route"}}`,
		`{"http":{"url":"https://resolver.example","unknown":true}}`,
		`{"unknown":{}}`,
	} {
		if _, err := directive.DecodeRemoteSpec([]byte(raw)); !errors.Is(err, directive.ErrInvalidPayload) {
			t.Fatalf("DecodeRemoteSpec(%s) error = %v", raw, err)
		}
	}
}

func TestDecodePayloadRejectsInvalidTargetAndHeaderContract(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		`{}`,
		`{"target":{"base_url":"https://vendor.example","exact_url":"https://vendor.example/action"}}`,
		`{"target":{"base_url":"ftp://vendor.example"}}`,
		`{"target":{"base_url":"https://vendor.example"},"headers":{"mutations":[{"side":"request","action":"set","name":"Authorization"}]}}`,
		`{"target":{"base_url":"https://vendor.example"},"headers":{"mutations":[{"side":"response","action":"del","name":"Content-Length"}]}}`,
	} {
		if _, err := directive.DecodePayload([]byte(raw)); !errors.Is(err, directive.ErrInvalidPayload) {
			t.Fatalf("DecodePayload(%s) error = %v", raw, err)
		}
	}
}
