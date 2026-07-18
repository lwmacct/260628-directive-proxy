package proxy

import (
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"

	"github.com/lwmacct/260628-directive-proxy/internal/core/httpheader"
)

func applyRewrite(r *httputil.ProxyRequest, plan *Plan) {
	template := NewRequestTemplate(r.In)
	for _, name := range []string{"Connection", "Upgrade", "Te"} {
		if values := r.Out.Header.Values(name); len(values) > 0 {
			template.Header.Del(name)
			for _, value := range values {
				template.Header.Add(name, value)
			}
		}
	}
	r.Out = BuildRoundTripRequest(template, plan, r.Out.Context(), r.Out.Body)
}

func TestApplyHeaderOps(t *testing.T) {
	headers := http.Header{
		"X-Test":  []string{"old", "keep"},
		"X-Other": []string{"gone"},
	}
	httpheader.Apply(headers, []httpheader.Op{
		{Action: httpheader.ActionSet, Selector: exactSelector("X-Test"), Values: []string{"new", "alt"}},
		{Action: httpheader.ActionAdd, Selector: exactSelector("X-Extra"), Values: []string{"one", "two"}},
		{Action: httpheader.ActionRemove, Selector: exactSelector("X-Other")},
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
	httpheader.Apply(headers, []httpheader.Op{
		{Action: httpheader.ActionSet, Selector: globSelector("x-tenant-*"), Values: []string{"shared"}},
		{Action: httpheader.ActionAdd, Selector: exactSelector("X-Tenant-Three"), Values: []string{"new"}},
		{Action: httpheader.ActionRemove, Selector: globSelector("X-Tenant-Tw?")},
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
	httpheader.Apply(headers, []httpheader.Op{
		{Action: httpheader.ActionSet, Selector: globSelector("X-*"), Values: []string{"miss"}},
		{Action: httpheader.ActionSet, Selector: exactSelector("X-Created"), Values: []string{"first"}},
		{Action: httpheader.ActionSet, Selector: globSelector("x-*"), Values: []string{"second"}},
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
	httpheader.Apply(headers, []httpheader.Op{
		{Action: httpheader.ActionRemove, Selector: globSelector("*")},
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
	in := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/resources", nil)
	out := in.Clone(in.Context())
	req := &httputil.ProxyRequest{In: in, Out: out}

	applyRewrite(req, &Plan{
		Target: target,
		Headers: requestHeaderPlan(httpheader.ModePatch, false, httpheader.Op{
			Action:   httpheader.ActionSet,
			Selector: exactSelector("Authorization"),
			Values:   []string{"Bearer abc"},
		}),
	})

	if req.Out.URL.String() != "https://example.com/base" {
		t.Fatalf("unexpected url: %s", req.Out.URL.String())
	}
	if got := req.Out.Header.Get("Authorization"); got != "Bearer abc" {
		t.Fatalf("unexpected auth header: %q", got)
	}
}

func TestApplyRewriteUsesCompiledTargetAsIs(t *testing.T) {
	target, _ := url.Parse("https://example.com/ip?source=proxy")
	in := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/resources?client=1", nil)
	out := in.Clone(in.Context())
	req := &httputil.ProxyRequest{In: in, Out: out}

	applyRewrite(req, &Plan{Target: target})

	if req.Out.URL.String() != "https://example.com/ip?source=proxy" {
		t.Fatalf("unexpected url: %s", req.Out.URL.String())
	}
}

func TestApplyRewriteReplaceHeaderModeClearsInboundHeaders(t *testing.T) {
	target, _ := url.Parse("https://example.com/base")
	in := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/resources", nil)
	in.Header.Set("X-Inbound", "drop")
	out := in.Clone(in.Context())
	req := &httputil.ProxyRequest{In: in, Out: out}

	applyRewrite(req, &Plan{
		Target: target,
		Headers: requestHeaderPlan(httpheader.ModeReplace, false, httpheader.Op{
			Action:   httpheader.ActionSet,
			Selector: exactSelector("X-Only"),
			Values:   []string{"keep"},
		}),
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

func TestApplyRewritePatchRemovesProxyDisclosureHeadersByDefault(t *testing.T) {
	target, _ := url.Parse("https://example.com/base")
	in := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/resources", nil)
	for _, name := range []string{"Forwarded", "X-Forwarded-For"} {
		in.Header.Set(name, "remove")
	}
	out := in.Clone(in.Context())
	req := &httputil.ProxyRequest{In: in, Out: out}

	applyRewrite(req, &Plan{
		Target: target,
	})

	for _, name := range []string{"Forwarded", "X-Forwarded-For"} {
		if got := req.Out.Header.Get(name); got != "" {
			t.Fatalf("expected %s to be removed, got %q", name, got)
		}
	}
}

func TestApplyRewriteCanPreserveProxyDisclosureHeaders(t *testing.T) {
	target, _ := url.Parse("https://example.com/base")
	in := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/resources", nil)
	in.Header.Set("Forwarded", "for=client.example")
	out := in.Clone(in.Context())
	req := &httputil.ProxyRequest{In: in, Out: out}

	applyRewrite(req, &Plan{
		Target:  target,
		Headers: requestHeaderPlan(httpheader.ModePatch, true),
	})

	if got := req.Out.Header.Get("Forwarded"); got != "for=client.example" {
		t.Fatalf("expected proxy disclosure header to be preserved, got %q", got)
	}
}

func TestApplyRewriteAlwaysRemovesSystemHeaders(t *testing.T) {
	target, _ := url.Parse("https://example.com/base")
	in := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/resources", nil)
	in.Header.Set("X-Dp-Inbound", "drop")
	in.Header["x-dp-lowercase"] = []string{"drop"}
	in.Header.Set("X-Upstream", "keep")
	out := in.Clone(in.Context())
	req := &httputil.ProxyRequest{In: in, Out: out}

	applyRewrite(req, &Plan{
		Target: target,
		Headers: requestHeaderPlan(httpheader.ModePatch, false, httpheader.Op{
			Action:   httpheader.ActionSet,
			Selector: exactSelector("X-Dp-Injected"),
			Values:   []string{"drop"},
		}),
	})

	for name := range req.Out.Header {
		if strings.HasPrefix(strings.ToLower(name), "x-dp-") {
			t.Fatalf("dproxy header reached outbound request: %s", name)
		}
	}
	if got := req.Out.Header.Get("X-Upstream"); got != "keep" {
		t.Fatalf("unexpected unrelated header: %q", got)
	}
}

func TestApplyRewriteProxyDisclosurePolicyRunsBeforeOps(t *testing.T) {
	target, _ := url.Parse("https://example.com/base")
	in := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/resources", nil)
	in.Header.Set("True-Client-IP", "drop")
	in.Header.Set("X-Forwarded-Custom", "drop")
	in.Header.Set("X-Inbound", "keep")
	out := in.Clone(in.Context())
	req := &httputil.ProxyRequest{In: in, Out: out}

	applyRewrite(req, &Plan{
		Target: target,
		Headers: requestHeaderPlan(httpheader.ModePatch, false,
			httpheader.Op{Action: httpheader.ActionSet, Selector: exactSelector("True-Client-IP"), Values: []string{"explicit"}},
		),
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
	in := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/resources", nil)
	in.Header.Set("Connection", "X-Connection-Only")
	in.Header.Set("X-Connection-Only", "drop")
	in.Header.Set("Keep-Alive", "timeout=5")
	in.Header.Set("X-End-To-End", "keep")
	out := in.Clone(in.Context())
	out.Header = make(http.Header)
	req := &httputil.ProxyRequest{In: in, Out: out}

	applyRewrite(req, &Plan{Target: target})

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
		Headers: requestHeaderPlan(httpheader.ModePatch, false,
			httpheader.Op{Action: httpheader.ActionSet, Selector: exactSelector("Connection"), Values: []string{"X-Injected"}},
			httpheader.Op{Action: httpheader.ActionSet, Selector: exactSelector("X-Injected"), Values: []string{"unsafe"}},
		),
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
	in := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/resources", nil)
	out := in.Clone(in.Context())
	req := &httputil.ProxyRequest{In: in, Out: out}

	applyRewrite(req, &Plan{
		Target: target,
		Headers: requestHeaderPlan(httpheader.ModePatch, false, httpheader.Op{
			Action:   httpheader.ActionSet,
			Selector: exactSelector("Host"),
			Values:   []string{"custom.example.com"},
		}),
	})

	if req.Out.Host != "custom.example.com" {
		t.Fatalf("unexpected host: %q", req.Out.Host)
	}
	if got := req.Out.Header.Get("Host"); got != "" {
		t.Fatalf("host should not be stored in Header map: %q", got)
	}
}

func exactSelector(pattern string) httpheader.Selector {
	return httpheader.Selector{Kind: httpheader.SelectorExact, Pattern: pattern}
}

func globSelector(pattern string) httpheader.Selector {
	return httpheader.Selector{Kind: httpheader.SelectorGlob, Pattern: pattern}
}

func requestHeaderPlan(mode httpheader.Mode, preserveProxyDisclosure bool, ops ...httpheader.Op) httpheader.Plan {
	return httpheader.Plan{Request: httpheader.RequestPlan{
		Mode: mode, PreserveProxyDisclosure: preserveProxyDisclosure, Ops: ops,
	}}
}
