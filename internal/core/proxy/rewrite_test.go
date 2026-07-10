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
		{Action: HeaderSet, Name: "X-Test", Values: []string{"new", "alt"}},
		{Action: HeaderRemove, Name: "X-Test", Values: []string{"alt"}},
		{Action: HeaderAdd, Name: "X-Extra", Values: []string{"one", "two"}},
		{Action: HeaderRemove, Name: "X-Other", Values: []string{"gone"}},
	})

	if got := headers.Values("X-Test"); len(got) != 1 || got[0] != "new" {
		t.Fatalf("unexpected X-Test: %#v", got)
	}
	if got := headers.Values("X-Extra"); len(got) != 2 {
		t.Fatalf("unexpected X-Extra: %#v", got)
	}
	if _, exists := headers["X-Other"]; exists {
		t.Fatal("expected X-Other to be removed")
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
			Action: HeaderSet,
			Name:   "Authorization",
			Values: []string{"Bearer abc"},
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
			Action: HeaderSet,
			Name:   "X-Only",
			Values: []string{"keep"},
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

func TestApplyRewriteStripsProxyDisclosureHeaders(t *testing.T) {
	target, _ := url.Parse("https://example.com/base")
	in := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/chat", nil)
	for _, name := range proxyDisclosureHeaders {
		in.Header.Set(name, "leak")
	}
	in.Header.Set("X-Inbound", "keep")
	out := in.Clone(in.Context())
	req := &httputil.ProxyRequest{In: in, Out: out}

	applyRewrite(req, &Plan{
		Target:   target,
		JoinPath: true,
	})

	for _, name := range proxyDisclosureHeaders {
		if got := req.Out.Header.Get(name); got != "" {
			t.Fatalf("expected %s to be stripped, got %q", name, got)
		}
	}
	if got := req.Out.Header.Get("X-Inbound"); got != "keep" {
		t.Fatalf("expected unrelated inbound header to remain, got %q", got)
	}
}

func TestApplyRewriteCanExplicitlySetProxyDisclosureHeader(t *testing.T) {
	target, _ := url.Parse("https://example.com/base")
	in := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/chat", nil)
	in.Header.Set("True-Client-IP", "drop")
	out := in.Clone(in.Context())
	req := &httputil.ProxyRequest{In: in, Out: out}

	applyRewrite(req, &Plan{
		Target:   target,
		JoinPath: true,
		HeaderOps: []HeaderOp{{
			Action: HeaderSet,
			Name:   "True-Client-IP",
			Values: []string{"explicit"},
		}},
	})

	if got := req.Out.Header.Get("True-Client-IP"); got != "explicit" {
		t.Fatalf("unexpected explicit proxy disclosure header: %q", got)
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
			Action: HeaderSet,
			Name:   "Host",
			Values: []string{"custom.example.com"},
		}},
	})

	if req.Out.Host != "custom.example.com" {
		t.Fatalf("unexpected host: %q", req.Out.Host)
	}
	if got := req.Out.Header.Get("Host"); got != "" {
		t.Fatalf("host should not be stored in Header map: %q", got)
	}
}
