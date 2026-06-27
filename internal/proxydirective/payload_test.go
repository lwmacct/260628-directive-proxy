package proxydirective

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	input := Payload{
		Target: TargetSection{URL: "https://api.example.com/v1"},
		Transport: &TransportSection{
			Proxy: "socks5://user:pass@127.0.0.1:1080",
		},
		Headers: &HeaderSection{
			Mode: "replace",
			Ops: []HeaderOp{
				{Op: "=", Name: "Authorization", Values: []string{"Bearer secret"}},
				{Op: "=", Name: "X-Test", Values: []string{"a"}},
			},
		},
		Labels: map[string]any{
			"trace_id": "trace-123",
		},
		Capture: &CapturePolicy{
			Request: []string{"body"},
			Stream: &CaptureStreamSection{
				Events:     true,
				EventTypes: []string{"response.delta"},
			},
		},
	}

	encoded, err := Encode(input)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	if !strings.HasPrefix(encoded, TokenPrefix) {
		t.Fatalf("expected token prefix: %q", encoded)
	}

	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.Version != PayloadVersion || decoded.Kind != PayloadKind {
		t.Fatalf("unexpected protocol fields: %#v", decoded)
	}
	if decoded.Target.URL != input.Target.URL {
		t.Fatalf("unexpected url: %s", decoded.Target.URL)
	}
	if decoded.Transport == nil || decoded.Transport.Proxy != input.Transport.Proxy {
		t.Fatalf("unexpected proxy: %#v", decoded.Transport)
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
	if got := decoded.Labels["trace_id"]; got != "trace-123" {
		t.Fatalf("unexpected labels trace_id: %#v", got)
	}
	if decoded.Capture == nil {
		t.Fatal("expected capture policy")
	}
}

func TestDecodeRequiresDirectiveTokenPrefix(t *testing.T) {
	encoded := base64.RawURLEncoding.EncodeToString([]byte(`{"version":1,"kind":"directive-proxy.directive","target":{"url":"https://api.example.com/v1"}}`))

	if _, err := Decode(encoded); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestDecodeRejectsUnknownField(t *testing.T) {
	encoded := encodeRawToken([]byte(`{"version":1,"kind":"directive-proxy.directive","target":{"url":"https://api.example.com/v1"},"key":"secret"}`))

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
		Target:    TargetSection{URL: "https://api.example.com/v1"},
		Transport: &TransportSection{Proxy: "http://127.0.0.1:1080"},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateIgnoresEmptyCapturePolicy(t *testing.T) {
	err := Validate(Payload{
		Target:  TargetSection{URL: "https://api.example.com/v1"},
		Capture: &CapturePolicy{},
	})
	if err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestValidateRejectsInvalidCaptureFields(t *testing.T) {
	err := Validate(Payload{
		Target: TargetSection{URL: "https://api.example.com/v1"},
		Capture: &CapturePolicy{
			Request: []string{"headers", "cookies"},
		},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestDecodeRejectsMalformedCaptureFields(t *testing.T) {
	encoded := encodeRawToken([]byte(`{
		"version":1,
		"kind":"directive-proxy.directive",
		"target":{"url":"https://api.example.com/v1"},
		"capture":{
			"request":{"headers":true,"body":true},
			"response":["body"],
			"stream":"invalid"
		}
	}`))

	if _, err := Decode(encoded); err == nil {
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

func TestValidateAcceptsLabels(t *testing.T) {
	err := Validate(Payload{
		Target: TargetSection{URL: "https://api.example.com/v1"},
		Labels: map[string]any{
			"---": "x",
		},
	})
	if err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func encodeRawToken(raw []byte) string {
	return TokenPrefix + base64.RawURLEncoding.EncodeToString(raw)
}
