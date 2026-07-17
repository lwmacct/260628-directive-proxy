package directive

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	input := Payload{
		Target: TargetSection{URL: "https://api.example.com/v1"},
		Proxy:  "socks5://user:pass@127.0.0.1:1080",
		Headers: &HeaderSection{
			Request: &RequestHeaderSection{
				Mode:                    "replace",
				PreserveProxyDisclosure: true,
				Ops: []HeaderOp{
					{Op: "=", Name: "Authorization", Values: []string{"Bearer secret"}},
					{Op: "=", Name: "X-Test", Values: []string{"a"}},
				},
			},
			Response: &ResponseHeaderSection{Ops: []HeaderOp{{Op: "-", Name: "Server"}}},
		},
	}

	encoded, err := Encode(input)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	if !strings.HasPrefix(encoded, TokenFamily+"."+TokenVersion+".") {
		t.Fatalf("expected token prefix: %q", encoded)
	}

	token, err := Decode(encoded)
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
	if decoded.Headers == nil || decoded.Headers.Request == nil || decoded.Headers.Request.Mode != "replace" || !decoded.Headers.Request.PreserveProxyDisclosure {
		t.Fatalf("unexpected header mode: %#v", decoded.Headers)
	}
	if len(decoded.Headers.Request.Ops) != 2 ||
		decoded.Headers.Request.Ops[0].Name != "Authorization" ||
		len(decoded.Headers.Request.Ops[0].Values) != 1 ||
		decoded.Headers.Request.Ops[0].Values[0] != "Bearer secret" ||
		decoded.Headers.Response == nil || len(decoded.Headers.Response.Ops) != 1 {
		t.Fatalf("unexpected headers: %#v", decoded.Headers)
	}
}

func TestEncodeDecodeRemoteRoundTrip(t *testing.T) {
	input := RemoteSpec{
		Type: RemoteTypeHTTP,
		URL:  "https://policy.example.com/v1/resolve",
		Key:  "team-a/生产/service-a",
		Headers: map[string]string{
			"authorization": "Bearer policy-token",
		},
		RequestHeaders: []string{"Content-Type", "X-Tenant-*"},
	}
	encoded, err := EncodeRemote(input)
	if err != nil {
		t.Fatalf("encode remote failed: %v", err)
	}
	if !strings.HasPrefix(encoded, TokenFamily+"."+TokenVersion+"."+TokenRemote+".") {
		t.Fatalf("unexpected token: %q", encoded)
	}
	token, err := Decode(encoded)
	if err != nil {
		t.Fatalf("decode remote failed: %v", err)
	}
	if token.Kind != KindRemote || token.Remote.Source.Type != RemoteTypeHTTP || token.Remote.Source.URL != input.URL ||
		token.Remote.Source.Key != input.Key || token.Remote.Source.Headers["Authorization"] != "Bearer policy-token" ||
		len(token.Remote.Source.RequestHeaders) != 2 {
		t.Fatalf("unexpected decoded token: %#v", token)
	}
}

func TestRemoteSpecValidation(t *testing.T) {
	valid := []string{"team-a/service-a", "region:cn/service:primary", "客户甲/服务一", strings.Repeat("a", maxRemoteKeyBytes)}
	for _, key := range valid {
		if _, err := EncodeRemote(RemoteSpec{Type: RemoteTypeRedis, URL: "rediss://user:pass@redis.example.com:6380/1", Key: key}); err != nil {
			t.Fatalf("expected key %q to be valid: %v", key, err)
		}
	}
	invalid := []string{"", " leading", "trailing ", "line\nbreak", strings.Repeat("a", maxRemoteKeyBytes+1)}
	for _, key := range invalid {
		if _, err := EncodeRemote(RemoteSpec{Type: RemoteTypeRedis, URL: "redis://redis.example.com:6379/0", Key: key}); err == nil {
			t.Fatalf("expected key %q to be invalid", key)
		}
	}
	invalidSpecs := []RemoteSpec{
		{Type: "unknown", URL: "https://policy.example.com"},
		{Type: RemoteTypeHTTP, URL: "file:///tmp/directive"},
		{Type: RemoteTypeHTTP, URL: "https://user:pass@policy.example.com"},
		{Type: RemoteTypeHTTP, URL: "https://policy.example.com", Headers: map[string]string{"Host": "other.example.com"}},
		{Type: RemoteTypeRedis, URL: "http://redis.example.com", Key: "key"},
		{Type: RemoteTypeRedis, URL: "redis://redis.example.com", Key: "key", Headers: map[string]string{"X-Test": "value"}},
		{Type: RemoteTypeRedis, URL: "redis://redis.example.com", Key: "key", RequestHeaders: []string{"X-Tenant"}},
		{Type: RemoteTypeHTTP, URL: "https://policy.example.com", RequestHeaders: []string{"[invalid"}},
	}
	for _, spec := range invalidSpecs {
		if _, err := EncodeRemote(spec); err == nil {
			t.Fatalf("expected spec to be invalid: %#v", spec)
		}
	}
}

func TestDecodeRequiresDirectiveTokenPrefix(t *testing.T) {
	encoded := base64.RawURLEncoding.EncodeToString([]byte(`{"target":{"url":"https://api.example.com/v1"}}`))

	if _, err := Decode(encoded); err == nil {
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
		if _, err := Decode(token); err == nil {
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

func TestDecodeRejectsLegacyTransportProxy(t *testing.T) {
	encoded := encodeRawToken([]byte(`{"target":{"url":"https://api.example.com/v1"},"transport":{"proxy":"socks5://127.0.0.1:1080"}}`))

	if _, err := decodeInlinePayload(encoded); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateRejectsInvalidHeaderMode(t *testing.T) {
	err := Validate(Payload{
		Target:  TargetSection{URL: "https://api.example.com/v1"},
		Headers: &HeaderSection{Request: &RequestHeaderSection{Mode: "invalid"}},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateRejectsHeaderSetWithoutValues(t *testing.T) {
	err := Validate(Payload{
		Target:  TargetSection{URL: "https://api.example.com/v1"},
		Headers: requestHeaders(HeaderOp{Op: "=", Name: "X-Test"}),
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateRejectsMultiValueHost(t *testing.T) {
	err := Validate(Payload{
		Target:  TargetSection{URL: "https://api.example.com/v1"},
		Headers: requestHeaders(HeaderOp{Op: "=", Name: "Host", Values: []string{"a.example.com", "b.example.com"}}),
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateRejectsAppendHost(t *testing.T) {
	err := Validate(Payload{
		Target:  TargetSection{URL: "https://api.example.com/v1"},
		Headers: requestHeaders(HeaderOp{Op: "+", Name: "Host", Values: []string{"api.example.com"}}),
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateRejectsHeaderOpWithBothNameAndGlob(t *testing.T) {
	err := Validate(Payload{
		Target:  TargetSection{URL: "https://api.example.com/v1"},
		Headers: requestHeaders(HeaderOp{Op: "-", Name: "X-Test", Glob: "X-*"}),
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestDecodeRejectsLegacyHeaderSchema(t *testing.T) {
	raw := []byte(`{"target":{"url":"https://api.example.com/v1"},"headers":{"ops":[{"op":"-","preset":"proxy-disclosure"}]}}`)
	if _, err := DecodePayload(raw); err == nil {
		t.Fatal("expected legacy header schema to be rejected")
	}
}

func TestValidateRejectsInvalidHeaderGlob(t *testing.T) {
	err := Validate(Payload{
		Target:  TargetSection{URL: "https://api.example.com/v1"},
		Headers: requestHeaders(HeaderOp{Op: "-", Glob: "X-["}),
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateRejectsInvalidExactHeaderName(t *testing.T) {
	err := Validate(Payload{
		Target:  TargetSection{URL: "https://api.example.com/v1"},
		Headers: requestHeaders(HeaderOp{Op: "-", Name: "Bad Header"}),
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateRejectsRemoveWithValues(t *testing.T) {
	err := Validate(Payload{
		Target:  TargetSection{URL: "https://api.example.com/v1"},
		Headers: requestHeaders(HeaderOp{Op: "-", Glob: "X-*", Values: []string{"value"}}),
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateRejectsProtectedResponseHeader(t *testing.T) {
	for _, name := range []string{"Connection", "Content-Length", "Date", "Host", "X-Dproxy-Trace-ID"} {
		err := Validate(Payload{
			Target: TargetSection{URL: "https://api.example.com/v1"},
			Headers: &HeaderSection{Response: &ResponseHeaderSection{Ops: []HeaderOp{{
				Op: "-", Name: name,
			}}}},
		})
		if err == nil {
			t.Fatalf("expected protected response header %s to be rejected", name)
		}
	}
}

func TestValidateRejectsInvalidHeaderValue(t *testing.T) {
	for _, headers := range []*HeaderSection{
		requestHeaders(HeaderOp{Op: "=", Name: "X-Test", Values: []string{"bad\rvalue"}}),
		&HeaderSection{Response: &ResponseHeaderSection{Ops: []HeaderOp{
			{Op: "=", Name: "X-Test", Values: []string{"bad\nvalue"}},
		}}},
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
	return TokenFamily + "." + TokenVersion + "." + TokenInline + "." + base64.RawURLEncoding.EncodeToString(raw)
}

func decodeInlinePayload(encoded string) (Payload, error) {
	token, err := Decode(encoded)
	if err != nil {
		return Payload{}, err
	}
	if token.Payload == nil {
		return Payload{}, ErrInvalidPayload
	}
	return *token.Payload, nil
}
