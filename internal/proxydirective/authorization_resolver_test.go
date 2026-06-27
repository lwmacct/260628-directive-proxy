package proxydirective

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/proxyplan"
)

func TestResolverUsesDirectiveAuthorizationPayload(t *testing.T) {
	raw, err := Encode(Payload{
		Target: TargetSection{URL: "https://api.example.com/v1"},
		Labels: map[string]any{
			"trace_id": "trace-123",
		},
		Headers: &HeaderSection{Ops: []HeaderOp{
			{Op: "=", Name: "Authorization", Values: []string{"Bearer secret"}},
			{Op: "=", Name: "X-Test", Values: []string{"a"}},
			{Op: "+", Name: "X-Multi", Values: []string{"b", "c"}},
		}},
	})
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	req := httptest.NewRequest("POST", "http://proxy.local/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer "+raw)

	plan, err := NewResolver().Resolve(req)
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if plan.Target.String() != "https://api.example.com/v1" {
		t.Fatalf("unexpected target: %s", plan.Target.String())
	}
	if !plan.JoinPath {
		t.Fatal("expected join path")
	}
	if len(plan.HeaderOps) != 4 {
		t.Fatalf("unexpected header op count: %d", len(plan.HeaderOps))
	}
	if plan.HeaderOps[0].Action != proxyplan.HeaderRemove || plan.HeaderOps[0].Name != "Authorization" {
		t.Fatalf("expected authorization strip op first: %#v", plan.HeaderOps)
	}
	if got := plan.Labels["trace_id"]; got != "trace-123" {
		t.Fatalf("unexpected directive labels: %#v", plan.Labels)
	}
}

func TestResolverAddsRuntimeFromIncomingRequest(t *testing.T) {
	raw, err := Encode(Payload{
		Target: TargetSection{URL: "https://api.example.com/v1"},
	})
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	req := httptest.NewRequest("POST", "http://proxy.local/v1/chat/completions", nil)
	req.RemoteAddr = "203.0.113.10:54321"
	req.Header.Set("Authorization", "Bearer "+raw)
	req.Header.Set(proxyplan.ClientRequestIDHeader, "client-req-1")
	req.Header.Set("M-Runtime-Shell", "digitflow")
	req.Header.Add("M-Runtime-Flag", "one")
	req.Header.Add("M-Runtime-Flag", "two")

	plan, err := NewResolver().Resolve(req)
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if got := plan.Runtime.IncomingRemoteAddr; got != "203.0.113.10:54321" {
		t.Fatalf("unexpected incoming_remote_addr: %#v", plan.Runtime)
	}
	if got := plan.Runtime.ClientRequestID; got != "client-req-1" {
		t.Fatalf("unexpected client request id: %#v", plan.Runtime)
	}
	if got := plan.Runtime.Headers["M-Runtime-Shell"]; len(got) != 1 || got[0] != "digitflow" {
		t.Fatalf("unexpected runtime shell header: %#v", plan.Runtime.Headers)
	}
	if got := plan.Runtime.Headers["M-Runtime-Flag"]; len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("unexpected runtime flag header: %#v", plan.Runtime.Headers)
	}
}

func TestResolverIgnoresNonDirectiveBearerToken(t *testing.T) {
	req := httptest.NewRequest("POST", "http://proxy.local/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer opaque-upstream-token")

	_, err := NewResolver().Resolve(req)
	if !errors.Is(err, proxyplan.ErrInvalidPlan) {
		t.Fatalf("expected invalid plan for non-directive bearer token, got %v", err)
	}
}

func TestResolverReturnsInvalidDirectiveForMalformedDirectiveToken(t *testing.T) {
	req := httptest.NewRequest("POST", "http://proxy.local/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer "+TokenPrefix+"not-valid-base64url")

	_, err := NewResolver().Resolve(req)
	if !errors.Is(err, proxyplan.ErrInvalidDirective) {
		t.Fatalf("expected invalid directive, got %v", err)
	}
}

func TestAuthorizationResolverErrorDoesNotExposeRawOrDecodedPayload(t *testing.T) {
	const decodedSecret = "decoded-auth-secret"

	raw, err := Encode(Payload{
		Target: TargetSection{URL: decodedSecret},
	})
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	req := httptest.NewRequest("POST", "http://proxy.local/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer "+raw)

	_, err = NewResolver().Resolve(req)
	if !errors.Is(err, proxyplan.ErrInvalidDirective) {
		t.Fatalf("expected invalid directive, got %v", err)
	}
	message := err.Error()
	if strings.Contains(message, raw) || strings.Contains(message, decodedSecret) {
		t.Fatalf("resolver error leaked authorization content: %q", message)
	}
}
