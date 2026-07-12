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
			Mode: "replace",
			Ops: []HeaderOp{
				{Op: "-", Preset: "proxy-disclosure"},
				{Op: "=", Name: "Authorization", Values: []string{"Bearer secret"}},
				{Op: "=", Name: "X-Test", Values: []string{"a"}},
			},
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
	if token.Kind != TokenInline {
		t.Fatalf("unexpected token kind: %q", token.Kind)
	}
	decoded, err := DecodePayload(token.Payload)
	if err != nil {
		t.Fatalf("decode payload failed: %v", err)
	}
	if decoded.Target.URL != input.Target.URL {
		t.Fatalf("unexpected url: %s", decoded.Target.URL)
	}
	if decoded.Proxy != input.Proxy {
		t.Fatalf("unexpected proxy: %#v", decoded.Proxy)
	}
	if decoded.Headers == nil || decoded.Headers.Mode != "replace" {
		t.Fatalf("unexpected header mode: %#v", decoded.Headers)
	}
	if len(decoded.Headers.Ops) != 3 ||
		decoded.Headers.Ops[0].Preset != "proxy-disclosure" ||
		decoded.Headers.Ops[1].Name != "Authorization" ||
		len(decoded.Headers.Ops[1].Values) != 1 ||
		decoded.Headers.Ops[1].Values[0] != "Bearer secret" {
		t.Fatalf("unexpected headers: %#v", decoded.Headers)
	}
}

func TestEncodeDecodeRedisKeyRoundTrip(t *testing.T) {
	encoded, err := EncodeRedisKey("team-a/生产/openai")
	if err != nil {
		t.Fatalf("encode redis key failed: %v", err)
	}
	if !strings.HasPrefix(encoded, TokenFamily+"."+TokenVersion+"."+TokenRedis+".") {
		t.Fatalf("unexpected token: %q", encoded)
	}
	token, err := Decode(encoded)
	if err != nil {
		t.Fatalf("decode redis key failed: %v", err)
	}
	if token.Kind != TokenRedis || token.RedisKey != "team-a/生产/openai" {
		t.Fatalf("unexpected decoded token: %#v", token)
	}
}

func TestRedisKeyValidationIsMinimal(t *testing.T) {
	valid := []string{"team-a/openai", "region:cn/model:qwen", "客户甲/openai", strings.Repeat("a", maxRedisKeyBytes)}
	for _, key := range valid {
		if _, err := EncodeRedisKey(key); err != nil {
			t.Fatalf("expected key %q to be valid: %v", key, err)
		}
	}
	invalid := []string{"", " leading", "trailing ", "line\nbreak", strings.Repeat("a", maxRedisKeyBytes+1)}
	for _, key := range invalid {
		if _, err := EncodeRedisKey(key); err == nil {
			t.Fatalf("expected key %q to be invalid", key)
		}
	}
}

func TestDecodeRequiresDirectiveTokenPrefix(t *testing.T) {
	encoded := base64.RawURLEncoding.EncodeToString([]byte(`{"target":{"url":"https://api.example.com/v1"}}`))

	if _, err := Decode(encoded); err == nil {
		t.Fatal("expected validation error")
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
		Headers: &HeaderSection{Mode: "invalid"},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateRejectsHeaderSetWithoutValues(t *testing.T) {
	err := Validate(Payload{
		Target: TargetSection{URL: "https://api.example.com/v1"},
		Headers: &HeaderSection{Ops: []HeaderOp{
			{Op: "=", Name: "X-Test"},
		}},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateRejectsMultiValueHost(t *testing.T) {
	err := Validate(Payload{
		Target: TargetSection{URL: "https://api.example.com/v1"},
		Headers: &HeaderSection{Ops: []HeaderOp{
			{Op: "=", Name: "Host", Values: []string{"a.example.com", "b.example.com"}},
		}},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateRejectsAppendHost(t *testing.T) {
	err := Validate(Payload{
		Target: TargetSection{URL: "https://api.example.com/v1"},
		Headers: &HeaderSection{Ops: []HeaderOp{
			{Op: "+", Name: "Host", Values: []string{"api.example.com"}},
		}},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateRejectsHeaderOpWithBothNameAndGlob(t *testing.T) {
	err := Validate(Payload{
		Target: TargetSection{URL: "https://api.example.com/v1"},
		Headers: &HeaderSection{Ops: []HeaderOp{
			{Op: "-", Name: "X-Test", Glob: "X-*"},
		}},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateRejectsUnknownOrNonRemovePreset(t *testing.T) {
	for _, op := range []HeaderOp{
		{Op: "-", Preset: "unknown"},
		{Op: "=", Preset: "proxy-disclosure", Values: []string{"value"}},
		{Op: "-", Name: "X-Test", Preset: "proxy-disclosure"},
	} {
		err := Validate(Payload{
			Target:  TargetSection{URL: "https://api.example.com/v1"},
			Headers: &HeaderSection{Ops: []HeaderOp{op}},
		})
		if err == nil {
			t.Fatalf("expected validation error for %#v", op)
		}
	}
}

func TestValidateRejectsInvalidHeaderGlob(t *testing.T) {
	err := Validate(Payload{
		Target: TargetSection{URL: "https://api.example.com/v1"},
		Headers: &HeaderSection{Ops: []HeaderOp{
			{Op: "-", Glob: "X-["},
		}},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateRejectsInvalidExactHeaderName(t *testing.T) {
	err := Validate(Payload{
		Target: TargetSection{URL: "https://api.example.com/v1"},
		Headers: &HeaderSection{Ops: []HeaderOp{
			{Op: "-", Name: "Bad Header"},
		}},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateRejectsRemoveWithValues(t *testing.T) {
	err := Validate(Payload{
		Target: TargetSection{URL: "https://api.example.com/v1"},
		Headers: &HeaderSection{Ops: []HeaderOp{
			{Op: "-", Glob: "X-*", Values: []string{"value"}},
		}},
	})
	if err == nil {
		t.Fatal("expected validation error")
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
	return DecodePayload(token.Payload)
}
