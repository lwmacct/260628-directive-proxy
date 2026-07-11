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
	if plan.HeaderOps[0].Action != proxy.HeaderRemove || plan.HeaderOps[0].Selector.Pattern != "Authorization" {
		t.Fatalf("expected authorization strip op first: %#v", plan.HeaderOps)
	}
}

func TestResolverReturnsNoMatchForNonDirectiveBearerToken(t *testing.T) {
	req := httptest.NewRequest("POST", "http://proxy.local/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer opaque-upstream-token")

	_, err := NewResolver().Resolve(req)
	if !errors.Is(err, proxy.ErrNoMatch) {
		t.Fatalf("expected no match for non-directive bearer token, got %v", err)
	}
}

func TestDirectiveTokenFromAuthorizationReservesDProxyTokenFamily(t *testing.T) {
	tests := []struct {
		name          string
		authorization string
		want          bool
	}{
		{name: "current version", authorization: "Bearer dproxy.11.payload", want: true},
		{name: "unsupported version", authorization: "Bearer dproxy.12.payload", want: true},
		{name: "malformed family token", authorization: "Bearer dproxy.", want: true},
		{name: "case insensitive scheme", authorization: "bearer dproxy.11.payload", want: true},
		{name: "opaque bearer", authorization: "Bearer opaque-upstream-token", want: false},
		{name: "other scheme", authorization: "Basic dproxy.11.payload", want: false},
		{name: "missing", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "http://proxy.local/v1/chat/completions", nil)
			if tt.authorization != "" {
				req.Header.Set("Authorization", tt.authorization)
			}
			_, got := directiveTokenFromAuthorization(req)
			if got != tt.want {
				t.Fatalf("unexpected directive request match: got %t want %t", got, tt.want)
			}
		})
	}

	if _, ok := directiveTokenFromAuthorization(nil); ok {
		t.Fatal("nil request must not match")
	}
}

func TestResolverReturnsInvalidDirectiveForUnsupportedDirectiveVersion(t *testing.T) {
	req := httptest.NewRequest("POST", "http://proxy.local/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer "+TokenFamily+".10.payload")

	_, err := NewResolver().Resolve(req)
	if !errors.Is(err, proxy.ErrInvalidDirective) {
		t.Fatalf("expected invalid directive, got %v", err)
	}
}

func TestResolverReturnsInvalidDirectiveForMalformedDirectiveToken(t *testing.T) {
	req := httptest.NewRequest("POST", "http://proxy.local/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer "+TokenFamily+"."+TokenVersion+".not-valid-base64url")

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
