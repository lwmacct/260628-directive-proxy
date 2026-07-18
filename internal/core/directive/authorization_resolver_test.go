package directive

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lwmacct/260628-directive-proxy/internal/core/proxy"
)

func TestResolverUsesDirectiveAuthorizationPayload(t *testing.T) {
	raw, err := Encode(testTokenSecret, Payload{
		Metadata: testDirectiveMetadata(),
		Target:   TargetSection{BaseURL: "https://api.example.com/v1"},
		Headers: requestHeaders(
			HeaderMutation{Action: HeaderActionSet, Name: "Authorization", Values: []string{"Bearer secret"}},
			HeaderMutation{Action: HeaderActionSet, Name: "X-Test", Values: []string{"a"}},
			HeaderMutation{Action: HeaderActionAppend, Name: "X-Multi", Values: []string{"b", "c"}},
		),
	})
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	req := httptest.NewRequest("POST", "http://proxy.local/v1/resources", nil)
	req.Header.Set("Authorization", "Bearer "+raw)

	resolution, err := resolveRequest(newTestResolver(), req)
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	plan := resolution.Plan()
	if plan.Target.String() != "https://api.example.com/v1/v1/resources" {
		t.Fatalf("unexpected target: %s", plan.Target.String())
	}
	if len(plan.Headers.Request.StripBeforeOps) != 1 || len(plan.Headers.Request.Ops) != 3 {
		t.Fatalf("unexpected request header plan: %#v", plan.Headers.Request)
	}
	if plan.Headers.Request.StripBeforeOps[0] != "Authorization" {
		t.Fatalf("expected authorization pre-strip: %#v", plan.Headers.Request)
	}
}

func TestResolverCompilesExactTargetWithoutInboundURL(t *testing.T) {
	raw, err := Encode(testTokenSecret, Payload{
		Metadata: testDirectiveMetadata(),
		Target:   TargetSection{ExactURL: "https://api.example.com/action?signature=fixed"},
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "http://proxy.local/v1/resources?client=1", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	resolution, err := resolveRequest(newTestResolver(), req)
	if err != nil {
		t.Fatal(err)
	}
	if got := resolution.Plan().Target.String(); got != "https://api.example.com/action?signature=fixed" {
		t.Fatalf("unexpected exact target: %s", got)
	}
}

func TestInlinePreparedPlanIsImmutableAcrossAttempts(t *testing.T) {
	raw, err := Encode(testTokenSecret, Payload{
		Metadata: testDirectiveMetadata(),
		Target:   TargetSection{BaseURL: "https://api.example.com"},
		Headers: &HeaderPolicy{Mutations: []HeaderMutation{
			{Side: HeaderSideRequest, Action: HeaderActionSet, Name: "X-Test", Values: []string{"original"}},
			{Side: HeaderSideResponse, Action: HeaderActionSet, Name: "X-Response", Values: []string{"original"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "http://proxy.local/", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	prepared, err := newTestResolver().Prepare(req)
	if err != nil {
		t.Fatal(err)
	}
	first := prepared.Plan()
	first.Target.Host = "mutated.example"
	first.Headers.Request.Ops[0].Values[0] = "mutated"
	first.Headers.Response.Ops[0].Values[0] = "mutated"
	second := prepared.Plan()
	if second.Target.Host != "api.example.com" || second.Headers.Request.Ops[0].Values[0] != "original" || second.Headers.Response.Ops[0].Values[0] != "original" {
		t.Fatalf("inline plan mutation leaked from prepared value: %#v", second)
	}
}

func TestResolverReturnsNoMatchForNonDirectiveBearerToken(t *testing.T) {
	req := httptest.NewRequest("POST", "http://proxy.local/v1/resources", nil)
	req.Header.Set("Authorization", "Bearer opaque-upstream-token")

	_, err := resolveRequest(newTestResolver(), req)
	if !errors.Is(err, proxy.ErrNoMatch) {
		t.Fatalf("expected no match for non-directive bearer token, got %v", err)
	}
}

func TestDirectiveTokenFromAuthorizationReservesDPTokenFamily(t *testing.T) {
	tests := []struct {
		name          string
		authorization string
		want          bool
	}{
		{name: "current version", authorization: "Bearer dp.20.inline.payload.mac", want: true},
		{name: "unsupported version", authorization: "Bearer dp.999.inline.payload", want: true},
		{name: "malformed family token", authorization: "Bearer dp.", want: true},
		{name: "case insensitive scheme", authorization: "bearer dp.20.inline.payload.mac", want: true},
		{name: "legacy family", authorization: "Bearer dproxy.18.i.payload", want: false},
		{name: "opaque bearer", authorization: "Bearer opaque-upstream-token", want: false},
		{name: "other scheme", authorization: "Basic dp.20.inline.payload.mac", want: false},
		{name: "missing", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "http://proxy.local/v1/resources", nil)
			if tt.authorization != "" {
				req.Header.Set("Authorization", tt.authorization)
			}
			_, got := directiveTokenFromAuthorization(req)
			if got != tt.want {
				t.Fatalf("unexpected directive request match: got %t want %t", got, tt.want)
			}
			if MatchesRequest(req) != tt.want {
				t.Fatalf("unexpected public directive request match: got %t want %t", MatchesRequest(req), tt.want)
			}
		})
	}

	if _, ok := directiveTokenFromAuthorization(nil); ok {
		t.Fatal("nil request must not match")
	}
	if MatchesRequest(nil) {
		t.Fatal("nil request must not match through public matcher")
	}
}

func TestResolverReturnsInvalidDirectiveForUnsupportedDirectiveVersion(t *testing.T) {
	req := httptest.NewRequest("POST", "http://proxy.local/v1/resources", nil)
	req.Header.Set("Authorization", "Bearer "+TokenFamily+".10.payload")

	_, err := resolveRequest(newTestResolver(), req)
	if !errors.Is(err, proxy.ErrInvalidDirective) {
		t.Fatalf("expected invalid directive, got %v", err)
	}
}

func TestResolverReturnsInvalidDirectiveForMalformedDirectiveToken(t *testing.T) {
	req := httptest.NewRequest("POST", "http://proxy.local/v1/resources", nil)
	req.Header.Set("Authorization", "Bearer "+TokenFamily+"."+TokenVersion+".not-valid-base64url")

	_, err := resolveRequest(newTestResolver(), req)
	if !errors.Is(err, proxy.ErrInvalidDirective) {
		t.Fatalf("expected invalid directive, got %v", err)
	}
}

func TestResolverRejectsWrongTokenSecret(t *testing.T) {
	raw, err := Encode(testTokenSecret, Payload{Metadata: testDirectiveMetadata(), Target: TargetSection{BaseURL: "https://api.example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "http://proxy.local/v1/resources", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	_, err = resolveRequest(NewResolver(ResolverOptions{TokenSecret: "wrong-secret"}), req)
	if !errors.Is(err, proxy.ErrDirectiveUnauthorized) {
		t.Fatalf("unexpected authorization error: %v", err)
	}
}

func TestAuthorizationResolverErrorDoesNotExposeRawOrDecodedPayload(t *testing.T) {
	const decodedSecret = "decoded-auth-secret"

	raw, err := encodeToken(testTokenSecret, TokenInline, []byte(`{"target":{"base_url":"`+decodedSecret+`"}}`))
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "http://proxy.local/v1/resources", nil)
	req.Header.Set("Authorization", "Bearer "+raw)

	_, err = resolveRequest(newTestResolver(), req)
	if !errors.Is(err, proxy.ErrInvalidDirective) {
		t.Fatalf("expected invalid directive, got %v", err)
	}
	message := err.Error()
	if strings.Contains(message, raw) || strings.Contains(message, decodedSecret) {
		t.Fatalf("resolver error leaked authorization content: %q", message)
	}
}
