package directive

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	input := Payload{
		Target: TargetSection{URL: "https://api.example.com/v1"},
		Proxy:  "socks5://user:pass@127.0.0.1:1080",
		Headers: &HeaderPolicy{
			Mode:                    "replace",
			PreserveProxyDisclosure: true,
			Mutations: []HeaderMutation{
				{Side: HeaderSideRequest, Action: HeaderActionSet, Name: "Authorization", Values: []string{"Bearer secret"}},
				{Side: HeaderSideRequest, Action: HeaderActionSet, Name: "X-Test", Values: []string{"a"}},
				{Side: HeaderSideResponse, Action: HeaderActionRemove, Name: "Server"},
			},
		},
	}

	encoded, err := Encode(testTokenSecret, input)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	if !strings.HasPrefix(encoded, TokenFamily+"."+TokenVersion+".") {
		t.Fatalf("expected token prefix: %q", encoded)
	}

	token, err := Decode(testTokenSecret, encoded)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if token.Kind != KindInline {
		t.Fatalf("unexpected token kind: %q", token.Kind)
	}
	decoded := *token.Payload
	if decoded.Target.URL != input.Target.URL {
		t.Fatalf("unexpected url: %s", decoded.Target.URL)
	}
	if decoded.Proxy != input.Proxy {
		t.Fatalf("unexpected proxy: %#v", decoded.Proxy)
	}
	if decoded.Headers == nil || decoded.Headers.Mode != "replace" || !decoded.Headers.PreserveProxyDisclosure {
		t.Fatalf("unexpected header mode: %#v", decoded.Headers)
	}
	if len(decoded.Headers.Mutations) != 3 || decoded.Headers.Mutations[0].Name != "Authorization" ||
		len(decoded.Headers.Mutations[0].Values) != 1 || decoded.Headers.Mutations[0].Values[0] != "Bearer secret" ||
		decoded.Headers.Mutations[2].Side != HeaderSideResponse {
		t.Fatalf("unexpected headers: %#v", decoded.Headers)
	}
}

func TestDecodeRejectsWrongSecretAndTamperedPayload(t *testing.T) {
	encoded, err := Encode(testTokenSecret, Payload{Target: TargetSection{URL: "https://api.example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode("wrong-secret", encoded); !errors.Is(err, ErrTokenUnauthorized) {
		t.Fatalf("unexpected wrong secret error: %v", err)
	}
	parts := strings.Split(encoded, ".")
	last := "A"
	if parts[3][len(parts[3])-1] == last[0] {
		last = "B"
	}
	parts[3] = parts[3][:len(parts[3])-1] + last
	if _, err := Decode(testTokenSecret, strings.Join(parts, ".")); !errors.Is(err, ErrTokenUnauthorized) {
		t.Fatalf("unexpected tampered token error: %v", err)
	}
}

func TestEncodeDecodeRemoteRoundTrip(t *testing.T) {
	input := RemoteSpec{
		HTTP: &HTTPRemoteSpec{
			URL: "https://policy.example.com/v1/team-a/service-a",
			Headers: &HeaderPolicy{
				Mode:                    "replace",
				PreserveProxyDisclosure: true,
				Mutations: []HeaderMutation{{
					Side: HeaderSideRequest, Action: HeaderActionSet, Name: "Authorization", Values: []string{"Bearer policy-token"},
				}},
			},
		},
	}
	encoded, err := EncodeRemote(testTokenSecret, input)
	if err != nil {
		t.Fatalf("encode remote failed: %v", err)
	}
	if !strings.HasPrefix(encoded, TokenFamily+"."+TokenVersion+"."+TokenRemote+".") {
		t.Fatalf("unexpected token: %q", encoded)
	}
	token, err := Decode(testTokenSecret, encoded)
	if err != nil {
		t.Fatalf("decode remote failed: %v", err)
	}
	if token.Kind != KindRemote || token.Remote.HTTP == nil || token.Remote.Redis != nil || token.Remote.File != nil ||
		token.Remote.HTTP.URL != input.HTTP.URL || token.Remote.HTTP.Headers == nil || token.Remote.HTTP.Headers.Mode != "replace" ||
		!token.Remote.HTTP.Headers.PreserveProxyDisclosure || len(token.Remote.HTTP.Headers.Mutations) != 1 || token.Remote.HTTP.Headers.Mutations[0].Values[0] != "Bearer policy-token" {
		t.Fatalf("unexpected decoded token: %#v", token)
	}
}

func TestEncodeDecodeFileRemoteRoundTrip(t *testing.T) {
	input := RemoteSpec{File: &FileRemoteSpec{Path: "team-a/services/primary.json"}}
	encoded, err := EncodeRemote(testTokenSecret, input)
	if err != nil {
		t.Fatalf("encode file remote failed: %v", err)
	}
	decoded, err := Decode(testTokenSecret, encoded)
	if err != nil {
		t.Fatalf("decode file remote failed: %v", err)
	}
	if decoded.Kind != KindRemote || decoded.Remote.File == nil || decoded.Remote.HTTP != nil || decoded.Remote.Redis != nil ||
		decoded.Remote.File.Path != input.File.Path {
		t.Fatalf("unexpected file remote: %#v", decoded.Remote)
	}
}

func TestDecodeRemoteRejectsInvalidBackendUnion(t *testing.T) {
	for _, raw := range []string{
		`{}`,
		`{"http":null}`,
		`{"http":null,"redis":{"url":"redis://redis.example/0","key":"directive"}}`,
		`{"http":{"url":"https://resolver.example"},"file":{"path":"directive.json"}}`,
		`{"http":{"url":"https://resolver.example","key":"legacy-key"}}`,
		`{"redis":{"url":"redis://redis.example","key":"directive","headers":null}}`,
		`{"file":{"path":"directive.json","url":"file:///tmp"}}`,
		`{"type":"file","path":"directive.json"}`,
		`{"type":"http","url":"https://resolver.example"}`,
	} {
		if _, err := Decode(testTokenSecret, encodeRawRemoteToken([]byte(raw))); err == nil {
			t.Fatalf("invalid remote backend field combination was accepted: %s", raw)
		}
	}
}

func TestRemoteSpecValidation(t *testing.T) {
	valid := []string{"team-a/service-a", "region:cn/service:primary", "客户甲/服务一", strings.Repeat("a", maxRemoteKeyBytes)}
	for _, key := range valid {
		if _, err := EncodeRemote(testTokenSecret, RemoteSpec{Redis: &RedisRemoteSpec{URL: "rediss://user:pass@redis.example.com:6380/1", Key: key}}); err != nil {
			t.Fatalf("expected key %q to be valid: %v", key, err)
		}
	}
	invalid := []string{"", " leading", "trailing ", "line\nbreak", strings.Repeat("a", maxRemoteKeyBytes+1)}
	for _, key := range invalid {
		if _, err := EncodeRemote(testTokenSecret, RemoteSpec{Redis: &RedisRemoteSpec{URL: "redis://redis.example.com:6379/0", Key: key}}); err == nil {
			t.Fatalf("expected key %q to be invalid", key)
		}
	}
	for _, path := range []string{"directive.json", "team-a/services/primary.json", "客户甲/服务一.json"} {
		if _, err := EncodeRemote(testTokenSecret, RemoteSpec{File: &FileRemoteSpec{Path: path}}); err != nil {
			t.Fatalf("expected file path %q to be valid: %v", path, err)
		}
	}
	invalidSpecs := []RemoteSpec{
		{},
		{HTTP: &HTTPRemoteSpec{URL: "https://policy.example.com"}, File: &FileRemoteSpec{Path: "directive.json"}},
		{HTTP: &HTTPRemoteSpec{URL: "file:///tmp/directive"}},
		{HTTP: &HTTPRemoteSpec{URL: "https://user:pass@policy.example.com"}},
		{HTTP: &HTTPRemoteSpec{URL: "https://policy.example.com/#fragment"}},
		{HTTP: &HTTPRemoteSpec{URL: "https://policy.example.com", Headers: &HeaderPolicy{Mode: "invalid"}}},
		{Redis: &RedisRemoteSpec{URL: "http://redis.example.com", Key: "key"}},
		{Redis: &RedisRemoteSpec{URL: "redis://redis.example.com/0#fragment", Key: "key"}},
		{HTTP: &HTTPRemoteSpec{URL: "https://policy.example.com", Headers: &HeaderPolicy{Mutations: []HeaderMutation{{Side: HeaderSideRequest, Action: HeaderActionSet, Name: "X-Test"}}}}},
		{File: &FileRemoteSpec{}},
		{File: &FileRemoteSpec{Path: "."}},
		{File: &FileRemoteSpec{Path: "../directive.json"}},
		{File: &FileRemoteSpec{Path: "/directive.json"}},
		{File: &FileRemoteSpec{Path: "team-a\\directive.json"}},
	}
	for _, spec := range invalidSpecs {
		if _, err := EncodeRemote(testTokenSecret, spec); err == nil {
			t.Fatalf("expected spec to be invalid: %#v", spec)
		}
	}
}

func TestDecodeRequiresDirectiveTokenPrefix(t *testing.T) {
	encoded := base64.RawURLEncoding.EncodeToString([]byte(`{"target":{"url":"https://api.example.com/v1"}}`))

	if _, err := Decode(testTokenSecret, encoded); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestDecodeRejectsLegacyTokenFamilyAndKinds(t *testing.T) {
	encoded := base64.RawURLEncoding.EncodeToString([]byte(`{"payload":{"target":{"url":"https://api.example.com/v1"}}}`))
	for _, token := range []string{
		"dproxy.18.i." + encoded,
		"dp.18.i." + encoded,
		"dp.18.r." + encoded,
	} {
		if _, err := Decode(testTokenSecret, token); err == nil {
			t.Fatalf("expected legacy token %q to be rejected", token)
		}
	}
}

func TestDecodeRejectsUnknownField(t *testing.T) {
	encoded := encodeRawToken([]byte(`{"target":{"url":"https://api.example.com/v1"},"key":"secret"}`))

	if _, err := decodeInlinePayload(encoded); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestInlineTokenBodyIsPayloadAndRejectsEnvelope(t *testing.T) {
	if _, err := Decode(testTokenSecret, encodeRawToken([]byte(`{"payload":{"target":{"url":"https://api.example.com/v1"}}}`))); err == nil {
		t.Fatal("inline token envelope must be rejected")
	}
	if _, err := Decode(testTokenSecret, encodeRawToken([]byte(`{"target":{"url":"https://api.example.com/v1"}}`))); err != nil {
		t.Fatalf("direct inline payload was rejected: %v", err)
	}
}

func TestRemoteTokenBodyIsRemoteSpecOnly(t *testing.T) {
	valid := encodeRawRemoteToken([]byte(`{"http":{"url":"https://resolver.example/resolve"}}`))
	if _, err := Decode(testTokenSecret, valid); err != nil {
		t.Fatalf("direct remote spec was rejected: %v", err)
	}
	invalid := []string{
		`{"source":{"http":{"url":"https://resolver.example/resolve"}}}`,
		`{"http":{"url":"https://resolver.example/resolve"},"payload":{"target":{"url":"https://api.example.com"}}}`,
		`{"http":{"url":"https://resolver.example/resolve"},"program":{}}`,
		`{"http":{"url":"https://resolver.example/resolve"},"recovery":{}}`,
	}
	for _, raw := range invalid {
		if _, err := Decode(testTokenSecret, encodeRawRemoteToken([]byte(raw))); err == nil {
			t.Fatalf("invalid remote token body was accepted: %s", raw)
		}
	}
}

func TestDecodeRejectsLegacyTransportProxy(t *testing.T) {
	encoded := encodeRawToken([]byte(`{"target":{"url":"https://api.example.com/v1"},"transport":{"proxy":"socks5://127.0.0.1:1080"}}`))

	if _, err := decodeInlinePayload(encoded); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateRejectsInvalidHeaderMode(t *testing.T) {
	err := Validate(Payload{
		Target:  TargetSection{URL: "https://api.example.com/v1"},
		Headers: &HeaderPolicy{Mode: "invalid"},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateRejectsHeaderSetWithoutValues(t *testing.T) {
	err := Validate(Payload{
		Target:  TargetSection{URL: "https://api.example.com/v1"},
		Headers: requestHeaders(HeaderMutation{Action: HeaderActionSet, Name: "X-Test"}),
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateRejectsMultiValueHost(t *testing.T) {
	err := Validate(Payload{
		Target:  TargetSection{URL: "https://api.example.com/v1"},
		Headers: requestHeaders(HeaderMutation{Action: HeaderActionSet, Name: "Host", Values: []string{"a.example.com", "b.example.com"}}),
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateRejectsAppendHost(t *testing.T) {
	err := Validate(Payload{
		Target:  TargetSection{URL: "https://api.example.com/v1"},
		Headers: requestHeaders(HeaderMutation{Action: HeaderActionAppend, Name: "Host", Values: []string{"api.example.com"}}),
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateRejectsHeaderMutationWithBothNameAndGlob(t *testing.T) {
	err := Validate(Payload{
		Target:  TargetSection{URL: "https://api.example.com/v1"},
		Headers: requestHeaders(HeaderMutation{Action: HeaderActionRemove, Name: "X-Test", Glob: "X-*"}),
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestDecodeRejectsMissingOrInvalidHeaderSide(t *testing.T) {
	invalid := []string{
		`{"target":{"url":"https://api.example.com/v1"},"headers":{"mutations":[{"action":"remove","name":"Server"}]}}`,
		`{"target":{"url":"https://api.example.com/v1"},"headers":{"mutations":[{"side":"upstream","action":"remove","name":"Server"}]}}`,
		`{"target":{"url":"https://api.example.com/v1"},"headers":{"mutations":[{"side":" request ","action":"remove","name":"Server"}]}}`,
	}
	for _, raw := range invalid {
		if _, err := DecodePayload([]byte(raw)); err == nil {
			t.Fatalf("expected invalid header side to be rejected: %s", raw)
		}
	}
}

func TestRemoteHeadersRejectResponseSide(t *testing.T) {
	raw := []byte(`{"http":{"url":"https://resolver.example/resolve","headers":{"mutations":[{"side":"response","action":"remove","name":"Server"}]}}}`)
	if _, err := Decode(testTokenSecret, encodeRawRemoteToken(raw)); err == nil {
		t.Fatal("expected remote response header side to be rejected")
	}
}

func TestValidateRejectsInvalidHeaderGlob(t *testing.T) {
	err := Validate(Payload{
		Target:  TargetSection{URL: "https://api.example.com/v1"},
		Headers: requestHeaders(HeaderMutation{Action: HeaderActionRemove, Glob: "X-["}),
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateRejectsInvalidExactHeaderName(t *testing.T) {
	err := Validate(Payload{
		Target:  TargetSection{URL: "https://api.example.com/v1"},
		Headers: requestHeaders(HeaderMutation{Action: HeaderActionRemove, Name: "Bad Header"}),
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateRejectsRemoveWithValues(t *testing.T) {
	err := Validate(Payload{
		Target:  TargetSection{URL: "https://api.example.com/v1"},
		Headers: requestHeaders(HeaderMutation{Action: HeaderActionRemove, Glob: "X-*", Values: []string{"value"}}),
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateRejectsProtectedResponseHeader(t *testing.T) {
	for _, name := range []string{"Connection", "Content-Length", "Date", "Host", "X-Dproxy-Trace-ID"} {
		err := Validate(Payload{
			Target:  TargetSection{URL: "https://api.example.com/v1"},
			Headers: &HeaderPolicy{Mutations: []HeaderMutation{{Side: HeaderSideResponse, Action: HeaderActionRemove, Name: name}}},
		})
		if err == nil {
			t.Fatalf("expected protected response header %s to be rejected", name)
		}
	}
}

func TestValidateRejectsInvalidHeaderValue(t *testing.T) {
	for _, headers := range []*HeaderPolicy{
		requestHeaders(HeaderMutation{Action: HeaderActionSet, Name: "X-Test", Values: []string{"bad\rvalue"}}),
		&HeaderPolicy{Mutations: []HeaderMutation{{Side: HeaderSideResponse, Action: HeaderActionSet, Name: "X-Test", Values: []string{"bad\nvalue"}}}},
	} {
		if err := Validate(Payload{Target: TargetSection{URL: "https://api.example.com/v1"}, Headers: headers}); err == nil {
			t.Fatal("expected invalid header value to be rejected")
		}
	}
}

func TestValidateRejectsMissingURL(t *testing.T) {
	err := Validate(Payload{})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateRejectsUnsupportedTargetScheme(t *testing.T) {
	err := Validate(Payload{
		Target: TargetSection{URL: "ftp://api.example.com/v1"},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateRejectsInvalidProxy(t *testing.T) {
	err := Validate(Payload{
		Target: TargetSection{URL: "https://api.example.com/v1"},
		Proxy:  "http://127.0.0.1:1080",
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestParseProxy(t *testing.T) {
	parsed, err := ParseProxy("SOCKS5://user:pass@127.0.0.1:1080")
	if err != nil {
		t.Fatalf("parse proxy failed: %v", err)
	}
	if parsed.String() != "socks5://user:pass@127.0.0.1:1080" {
		t.Fatalf("unexpected proxy: %s", parsed.String())
	}
}

func encodeRawToken(raw []byte) string {
	token, _ := encodeToken(testTokenSecret, TokenInline, raw)
	return token
}

func encodeRawRemoteToken(raw []byte) string {
	token, _ := encodeToken(testTokenSecret, TokenRemote, raw)
	return token
}

func decodeInlinePayload(encoded string) (Payload, error) {
	token, err := Decode(testTokenSecret, encoded)
	if err != nil {
		return Payload{}, err
	}
	if token.Payload == nil {
		return Payload{}, ErrInvalidPayload
	}
	return *token.Payload, nil
}
