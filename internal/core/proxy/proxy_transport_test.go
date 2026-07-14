package proxy

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestNewProxyAwareTransportUsesRequestProxy(t *testing.T) {
	transport := NewProxyAwareTransport(http.DefaultTransport.(*http.Transport))

	proxyURL, err := url.Parse("socks5://user:pass@127.0.0.1:1080")
	if err != nil {
		t.Fatalf("parse proxy failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "https://api.example.com/v1", nil)
	req = withRequestProxy(req, proxyURL)

	got, err := transport.Proxy(req)
	if err != nil {
		t.Fatalf("resolve proxy failed: %v", err)
	}
	if got == nil || got.String() != proxyURL.String() {
		t.Fatalf("unexpected proxy: %#v", got)
	}
	if !transport.DisableCompression {
		t.Fatal("expected implicit transport compression to be disabled")
	}
}

func TestNewProxyAwareTransportWithOptionsOverridesIdlePolicy(t *testing.T) {
	transport := NewProxyAwareTransportWithOptions(http.DefaultTransport.(*http.Transport), ProxyTransportOptions{
		MaxIdleConns:        17,
		MaxIdleConnsPerHost: 1,
		MaxConnsPerHost:     3000,
		IdleConnTimeout:     7 * time.Second,
		DisableKeepAlives:   true,
	})
	if transport.MaxIdleConns != 17 {
		t.Fatalf("unexpected max idle conns: %d", transport.MaxIdleConns)
	}
	if transport.MaxIdleConnsPerHost != 1 {
		t.Fatalf("unexpected max idle conns per host: %d", transport.MaxIdleConnsPerHost)
	}
	if transport.MaxConnsPerHost != 3000 {
		t.Fatalf("unexpected max conns per host: %d", transport.MaxConnsPerHost)
	}
	if transport.IdleConnTimeout != 7*time.Second {
		t.Fatalf("unexpected idle conn timeout: %s", transport.IdleConnTimeout)
	}
	if !transport.DisableKeepAlives {
		t.Fatal("expected keep-alives to be disabled")
	}
}

func TestBuildAttemptRequestCarriesProxyToOutboundRequest(t *testing.T) {
	target, _ := url.Parse("https://example.com/base")
	proxyURL, _ := url.Parse("socks5://user:pass@127.0.0.1:1080")

	in := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/resources", nil)
	out := BuildAttemptRequest(NewRequestTemplate(in), &Plan{
		Target: target,
		Proxy:  proxyURL,
	}, in.Context(), http.NoBody)

	got, ok := requestProxyFromContext(out.Context())
	if !ok || got == nil || got.String() != proxyURL.String() {
		t.Fatalf("unexpected proxy in request context: %#v", got)
	}
}
