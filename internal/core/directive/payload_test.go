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

	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
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
	if len(decoded.Headers.Ops) != 2 ||
		decoded.Headers.Ops[0].Name != "Authorization" ||
		len(decoded.Headers.Ops[0].Values) != 1 ||
		decoded.Headers.Ops[0].Values[0] != "Bearer secret" {
		t.Fatalf("unexpected headers: %#v", decoded.Headers)
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

	if _, err := Decode(encoded); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestDecodeRejectsLegacyTransportProxy(t *testing.T) {
	encoded := encodeRawToken([]byte(`{"target":{"url":"https://api.example.com/v1"},"transport":{"proxy":"socks5://127.0.0.1:1080"}}`))

	if _, err := Decode(encoded); err == nil {
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
	return TokenFamily + "." + TokenVersion + "." + base64.RawURLEncoding.EncodeToString(raw)
}
