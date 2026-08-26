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
		"modules":[{"module":"capture","config":{"enabled":true}}]
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

func TestRemoteSpecUUIDIsNormalizedAndValidated(t *testing.T) {
	id := "01990f4a-9e4c-7c42-a7ec-5c3f37a6f6b2"
	spec, err := directive.DecodeRemoteSpec([]byte(`{"uuid":" 01990F4A-9E4C-7C42-A7EC-5C3F37A6F6B2 ","http":{"url":"https://resolver.example"}}`))
	if err != nil {
		t.Fatalf("decode RemoteSpec with uuid: %v", err)
	}
	if spec.UUID != id {
		t.Fatalf("uuid = %q, want %q", spec.UUID, id)
	}
	for _, raw := range []string{
		`{"uuid":"not-a-uuid","http":{"url":"https://resolver.example"}}`,
	} {
		if _, err := directive.DecodeRemoteSpec([]byte(raw)); !errors.Is(err, directive.ErrInvalidPayload) {
			t.Fatalf("DecodeRemoteSpec(%s) error = %v", raw, err)
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

func TestRemoteTokenCodecSignsAndVerifiesSpec(t *testing.T) {
	t.Parallel()
	spec := directive.RemoteSpec{UUID: "01990f4a-9e4c-7c42-a7ec-5c3f37a6f6b2", HTTP: &directive.HTTPRemoteSpec{URL: "https://resolver.example/api/resolver"}}
	token, err := directive.EncodeRemote("shared-secret", spec)
	if err != nil {
		t.Fatalf("encode remote: %v", err)
	}
	decoded, err := directive.DecodeRemote("shared-secret", token)
	if err != nil {
		t.Fatalf("decode remote: %v", err)
	}
	if decoded.UUID != spec.UUID || decoded.HTTP == nil || decoded.HTTP.URL != spec.HTTP.URL {
		t.Fatalf("decoded spec = %#v", decoded)
	}
	if _, err = directive.DecodeRemote("wrong-secret", token); !errors.Is(err, directive.ErrInvalidPayload) {
		t.Fatalf("wrong secret error = %v", err)
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

func TestStrictJSONRejectsAmbiguousPayloads(t *testing.T) {
	t.Parallel()
	for _, raw := range [][]byte{
		[]byte(`{"target":{"base_url":"https://vendor.example"},"target":{"base_url":"https://other.example"}}`),
		[]byte(`{"Target":{"base_url":"https://vendor.example"}}`),
		[]byte(`{"target":{"base_url":"https://vendor.example"}} {}`),
		{'{', '"', 't', 'a', 'r', 'g', 'e', 't', '"', ':', '{', '"', 'b', 'a', 's', 'e', '_', 'u', 'r', 'l', '"', ':', '"', 0xff, '"', '}', '}'},
	} {
		if _, err := directive.DecodePayload(raw); !errors.Is(err, directive.ErrInvalidPayload) {
			t.Fatalf("DecodePayload(%q) error = %v", raw, err)
		}
	}
}

func TestCanonicalTemplateSortsMapMembersDeterministically(t *testing.T) {
	t.Parallel()
	canonical, err := directive.CanonicalTemplate([]byte(`{"metadata":{"z":"last","a":"first"},"modules":[{"module":"test","config":{"z":1,"a":2}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(canonical), `{"metadata":{"a":"first","z":"last"},"modules":[{"config":{"a":2,"z":1},"module":"test"}]}`; got != want {
		t.Fatalf("canonical template = %s, want %s", got, want)
	}
}
