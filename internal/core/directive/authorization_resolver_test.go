package directive

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/core/proxy"
)

func TestResolverUsesDirectiveAuthorizationPayload(t *testing.T) {
	raw, err := Encode(Payload{
		Target: TargetSection{URL: "https://api.example.com/v1"},
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
	if plan.HeaderOps[0].Action != proxy.HeaderRemove || plan.HeaderOps[0].Name != "Authorization" {
		t.Fatalf("expected authorization strip op first: %#v", plan.HeaderOps)
	}
}

func TestResolverIgnoresNonDirectiveBearerToken(t *testing.T) {
	req := httptest.NewRequest("POST", "http://proxy.local/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer opaque-upstream-token")

	_, err := NewResolver().Resolve(req)
	if !errors.Is(err, proxy.ErrInvalidPlan) {
		t.Fatalf("expected invalid plan for non-directive bearer token, got %v", err)
	}
}

func TestResolverReturnsInvalidDirectiveForMalformedDirectiveToken(t *testing.T) {
	req := httptest.NewRequest("POST", "http://proxy.local/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer "+TokenPrefix+"not-valid-base64url")

	_, err := NewResolver().Resolve(req)
	if !errors.Is(err, proxy.ErrInvalidDirective) {
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
	if !errors.Is(err, proxy.ErrInvalidDirective) {
		t.Fatalf("expected invalid directive, got %v", err)
	}
	message := err.Error()
	if strings.Contains(message, raw) || strings.Contains(message, decodedSecret) {
		t.Fatalf("resolver error leaked authorization content: %q", message)
	}
}
