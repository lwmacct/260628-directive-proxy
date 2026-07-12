package proxy

import (
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"testing"
)

func TestApplyHeaderOps(t *testing.T) {
	headers := http.Header{
		"X-Test":  []string{"old", "keep"},
		"X-Other": []string{"gone"},
	}
	applyHeaderOps(headers, []HeaderOp{
		{Action: HeaderSet, Selector: exactSelector("X-Test"), Values: []string{"new", "alt"}},
		{Action: HeaderAdd, Selector: exactSelector("X-Extra"), Values: []string{"one", "two"}},
		{Action: HeaderRemove, Selector: exactSelector("X-Other")},
	})

	if got := headers.Values("X-Test"); len(got) != 2 || got[0] != "new" || got[1] != "alt" {
		t.Fatalf("unexpected X-Test: %#v", got)
	}
	if got := headers.Values("X-Extra"); len(got) != 2 {
		t.Fatalf("unexpected X-Extra: %#v", got)
	}
	if _, exists := headers["X-Other"]; exists {
		t.Fatal("expected X-Other to be removed")
	}
}

func TestApplyHeaderOpsWithGlobSelector(t *testing.T) {
	headers := http.Header{
		"X-Tenant-One": []string{"old"},
		"X-Tenant-Two": []string{"old"},
		"X-Other":      []string{"keep"},
	}
	applyHeaderOps(headers, []HeaderOp{
		{Action: HeaderSet, Selector: globSelector("x-tenant-*"), Values: []string{"shared"}},
		{Action: HeaderAdd, Selector: exactSelector("X-Tenant-Three"), Values: []string{"new"}},
		{Action: HeaderRemove, Selector: globSelector("X-Tenant-Tw?")},
	})

	if got := headers.Get("X-Tenant-One"); got != "shared" {
		t.Fatalf("unexpected glob set result: %q", got)
	}
	if got := headers.Get("X-Tenant-Two"); got != "" {
		t.Fatalf("expected matching header to be removed, got %q", got)
	}
	if got := headers.Get("X-Tenant-Three"); got != "new" {
		t.Fatalf("expected exact op to create header, got %q", got)
	}
	if got := headers.Get("X-Other"); got != "keep" {
		t.Fatalf("unexpected unrelated header: %q", got)
	}
}

func TestGlobSelectorOnlyMatchesExistingHeadersAtEachOperation(t *testing.T) {
	headers := make(http.Header)
	applyHeaderOps(headers, []HeaderOp{
		{Action: HeaderSet, Selector: globSelector("X-*"), Values: []string{"miss"}},
		{Action: HeaderSet, Selector: exactSelector("X-Created"), Values: []string{"first"}},
		{Action: HeaderSet, Selector: globSelector("x-*"), Values: []string{"second"}},
	})

	if got := headers.Get("X-Created"); got != "second" {
		t.Fatalf("unexpected ordered glob result: %q", got)
	}
}

func TestGlobSelectorNeverMatchesHost(t *testing.T) {
	headers := http.Header{
		"Host":   []string{"keep.example.com"},
		"X-Test": []string{"remove"},
	}
	applyHeaderOps(headers, []HeaderOp{
		{Action: HeaderRemove, Selector: globSelector("*")},
	})

	if got := headers.Get("Host"); got != "keep.example.com" {
		t.Fatalf("glob must not match Host, got %q", got)
	}
	if got := headers.Get("X-Test"); got != "" {
		t.Fatalf("expected ordinary header to be removed, got %q", got)
	}
}

func TestApplyRewrite(t *testing.T) {
	target, _ := url.Parse("https://example.com/base")
	in := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/chat", nil)
	out := in.Clone(in.Context())
	req := &httputil.ProxyRequest{In: in, Out: out}

	applyRewrite(req, &Plan{
		Target:   target,
		JoinPath: true,
		HeaderOps: []HeaderOp{{
			Action:   HeaderSet,
			Selector: exactSelector("Authorization"),
			Values:   []string{"Bearer abc"},
		}},
	})

	if req.Out.URL.String() != "https://example.com/base/v1/chat" {
		t.Fatalf("unexpected url: %s", req.Out.URL.String())
	}
	if got := req.Out.Header.Get("Authorization"); got != "Bearer abc" {
		t.Fatalf("unexpected auth header: %q", got)
	}
}

func TestApplyRewriteWithoutJoinPathUsesTargetPathAsIs(t *testing.T) {
	target, _ := url.Parse("https://example.com/ip?source=proxy")
	in := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/chat?client=1", nil)
	out := in.Clone(in.Context())
	req := &httputil.ProxyRequest{In: in, Out: out}

	applyRewrite(req, &Plan{
		Target:   target,
		JoinPath: false,
	})

	if req.Out.URL.String() != "https://example.com/ip?source=proxy" {
		t.Fatalf("unexpected url: %s", req.Out.URL.String())
	}
}

func TestBuildOutboundURL(t *testing.T) {
	tests := []struct {
		name     string
		target   string
		inbound  string
		joinPath bool
		want     string
	}{
		{
			name:     "joins paths and keeps inbound query",
			target:   "https://example.com/base",
			inbound:  "http://proxy.local/v1/chat?client=1",
			joinPath: true,
			want:     "https://example.com/base/v1/chat?client=1",
		},
		{
			name:     "uses target as-is without join",
			target:   "https://example.com/ip?source=proxy",
			inbound:  "http://proxy.local/v1/chat?client=1",
			joinPath: false,
			want:     "https://example.com/ip?source=proxy",
		},
		{
			name:     "preserves trailing slash",
			target:   "https://example.com/base/",
			inbound:  "http://proxy.local/v1/chat",
			joinPath: true,
			want:     "https://example.com/base/v1/chat",
		},
		{
			name:     "preserves escaped path segments",
			target:   "https://example.com/base%2Ftenant",
			inbound:  "http://proxy.local/v1/a%2Fb",
			joinPath: true,
			want:     "https://example.com/base%2Ftenant/v1/a%2Fb",
		},
		{
			name:     "joins target and inbound query",
			target:   "https://example.com/base?source=proxy",
			inbound:  "http://proxy.local/v1/chat?client=1",
			joinPath: true,
			want:     "https://example.com/base/v1/chat?source=proxy&client=1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target, err := url.Parse(tt.target)
			if err != nil {
				t.Fatalf("parse target failed: %v", err)
			}
			inbound, err := url.Parse(tt.inbound)
			if err != nil {
				t.Fatalf("parse inbound failed: %v", err)
			}
			if got := BuildOutboundURL(target, inbound, tt.joinPath).String(); got != tt.want {
				t.Fatalf("unexpected outbound url: %s", got)
			}
		})
	}
}

func TestApplyRewriteReplaceHeaderModeClearsInboundHeaders(t *testing.T) {
	target, _ := url.Parse("https://example.com/base")
	in := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/chat", nil)
	in.Header.Set("X-Inbound", "drop")
	out := in.Clone(in.Context())
	req := &httputil.ProxyRequest{In: in, Out: out}

	applyRewrite(req, &Plan{
		Target:     target,
		JoinPath:   true,
		HeaderMode: HeaderModeReplace,
		HeaderOps: []HeaderOp{{
			Action:   HeaderSet,
			Selector: exactSelector("X-Only"),
			Values:   []string{"keep"},
		}},
	})

	if got := req.Out.Header.Get("X-Inbound"); got != "" {
		t.Fatalf("expected inbound header to be cleared, got %q", got)
	}
	if got := req.Out.Header.Get("X-Only"); got != "keep" {
		t.Fatalf("unexpected X-Only header: %q", got)
	}
	if values, exists := req.Out.Header["User-Agent"]; !exists || values != nil {
		t.Fatalf("expected default user-agent suppression, got exists=%t values=%#v", exists, values)
	}
}

func TestApplyRewritePatchPreservesProxyDisclosureHeadersByDefault(t *testing.T) {
	target, _ := url.Parse("https://example.com/base")
	in := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/chat", nil)
	for _, name := range proxyDisclosureHeaders {
		in.Header.Set(name, "preserve")
	}
	out := in.Clone(in.Context())
	req := &httputil.ProxyRequest{In: in, Out: out}

	applyRewrite(req, &Plan{
		Target:   target,
		JoinPath: true,
	})

	for _, name := range proxyDisclosureHeaders {
		if got := req.Out.Header.Get(name); got != "preserve" {
			t.Fatalf("expected %s to be preserved, got %q", name, got)
		}
	}
}

func TestApplyRewriteProxyDisclosurePresetIsOrdered(t *testing.T) {
	target, _ := url.Parse("https://example.com/base")
	in := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/chat", nil)
	in.Header.Set("True-Client-IP", "drop")
	in.Header.Set("X-Forwarded-Custom", "drop")
	in.Header.Set("X-Inbound", "keep")
	out := in.Clone(in.Context())
	req := &httputil.ProxyRequest{In: in, Out: out}

	applyRewrite(req, &Plan{
		Target:   target,
		JoinPath: true,
		HeaderOps: []HeaderOp{
			{Action: HeaderRemove, Selector: presetSelector(HeaderPresetProxyDisclosure)},
			{Action: HeaderSet, Selector: exactSelector("True-Client-IP"), Values: []string{"explicit"}},
		},
	})

	if got := req.Out.Header.Get("True-Client-IP"); got != "explicit" {
		t.Fatalf("unexpected explicit proxy disclosure header: %q", got)
	}
	if got := req.Out.Header.Get("X-Forwarded-Custom"); got != "" {
		t.Fatalf("expected forwarding prefix to be removed, got %q", got)
	}
	if got := req.Out.Header.Get("X-Inbound"); got != "keep" {
		t.Fatalf("unexpected unrelated header: %q", got)
	}
}

func TestApplyRewritePatchRemovesHopByHopHeaders(t *testing.T) {
	target, _ := url.Parse("https://example.com/base")
	in := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/chat", nil)
	in.Header.Set("Connection", "X-Connection-Only")
	in.Header.Set("X-Connection-Only", "drop")
	in.Header.Set("Keep-Alive", "timeout=5")
	in.Header.Set("X-End-To-End", "keep")
	out := in.Clone(in.Context())
	out.Header = make(http.Header)
	req := &httputil.ProxyRequest{In: in, Out: out}

	applyRewrite(req, &Plan{Target: target, JoinPath: true})

	for _, name := range []string{"Connection", "X-Connection-Only", "Keep-Alive"} {
		if got := req.Out.Header.Get(name); got != "" {
			t.Fatalf("expected hop-by-hop header %s to be removed, got %q", name, got)
		}
	}
	if got := req.Out.Header.Get("X-End-To-End"); got != "keep" {
		t.Fatalf("unexpected end-to-end header: %q", got)
	}
}

func TestApplyRewritePreservesTrustedTransportHeadersAndRejectsDirectiveInjection(t *testing.T) {
	target, _ := url.Parse("https://example.com/base")
	in := httptest.NewRequest(http.MethodGet, "http://proxy.local/stream", nil)
	out := in.Clone(in.Context())
	out.Header = http.Header{"Connection": {"Upgrade"}, "Upgrade": {"websocket"}}
	req := &httputil.ProxyRequest{In: in, Out: out}

	applyRewrite(req, &Plan{
		Target: target,
		HeaderOps: []HeaderOp{
			{Action: HeaderSet, Selector: exactSelector("Connection"), Values: []string{"X-Injected"}},
			{Action: HeaderSet, Selector: exactSelector("X-Injected"), Values: []string{"unsafe"}},
		},
	})

	if got := req.Out.Header.Get("Connection"); got != "Upgrade" {
		t.Fatalf("unexpected trusted Connection header: %q", got)
	}
	if got := req.Out.Header.Get("Upgrade"); got != "websocket" {
		t.Fatalf("unexpected trusted Upgrade header: %q", got)
	}
	if got := req.Out.Header.Get("X-Injected"); got != "" {
		t.Fatalf("connection-scoped header was injected: %q", got)
	}
}

func TestApplyRewriteCanSetOutboundHost(t *testing.T) {
	target, _ := url.Parse("https://example.com/base")
	in := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/chat", nil)
	out := in.Clone(in.Context())
	req := &httputil.ProxyRequest{In: in, Out: out}

	applyRewrite(req, &Plan{
		Target:   target,
		JoinPath: true,
		HeaderOps: []HeaderOp{{
			Action:   HeaderSet,
			Selector: exactSelector("Host"),
			Values:   []string{"custom.example.com"},
		}},
	})

	if req.Out.Host != "custom.example.com" {
		t.Fatalf("unexpected host: %q", req.Out.Host)
	}
	if got := req.Out.Header.Get("Host"); got != "" {
		t.Fatalf("host should not be stored in Header map: %q", got)
	}
}

func exactSelector(pattern string) HeaderSelector {
	return HeaderSelector{Kind: HeaderSelectorExact, Pattern: pattern}
}

func globSelector(pattern string) HeaderSelector {
	return HeaderSelector{Kind: HeaderSelectorGlob, Pattern: pattern}
}

func presetSelector(pattern string) HeaderSelector {
	return HeaderSelector{Kind: HeaderSelectorPreset, Pattern: pattern}
}
