package remotetoken

import (
	"strings"
	"testing"

	"github.com/lwmacct/260628-directive-proxy/internal/core/directive"
)

func TestGenerateBuildsRelayHTTPRemoteToken(t *testing.T) {
	token, err := Generate("hmac-secret", "https://vendor.example/api/resolver", "entry-s2s-token")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, "dp.22.remote.") {
		t.Fatalf("unexpected token prefix: %q", token)
	}
	document, err := directive.Decode("hmac-secret", token)
	if err != nil {
		t.Fatalf("generated token failed proxy decode: %v", err)
	}
	if document.Kind != directive.KindRemote || document.Remote == nil || document.Remote.HTTP == nil {
		t.Fatalf("unexpected decoded document: %#v", document)
	}
	spec := document.Remote.HTTP
	if spec.URL != "https://vendor.example/api/resolver" || spec.Headers == nil || len(spec.Headers.Mutations) != 1 {
		t.Fatalf("unexpected remote spec: %#v", spec)
	}
	mutation := spec.Headers.Mutations[0]
	if mutation.Side != directive.HeaderSideRequest || mutation.Action != directive.HeaderActionSet ||
		mutation.Name != "Authorization" || len(mutation.Values) != 1 || mutation.Values[0] != "Bearer entry-s2s-token" {
		t.Fatalf("unexpected authorization mutation: %#v", mutation)
	}
}

func TestGenerateRejectsInvalidResolverURL(t *testing.T) {
	for _, resolverURL := range []string{
		"",
		"not-a-url",
		"ftp://vendor.example/resolver",
		"https://user:pass@vendor.example/resolver",
		"https://vendor.example/resolver#fragment",
	} {
		if _, err := Generate("hmac-secret", resolverURL, "entry-s2s-token"); err == nil {
			t.Fatalf("expected invalid resolver URL to fail: %q", resolverURL)
		}
	}
}
