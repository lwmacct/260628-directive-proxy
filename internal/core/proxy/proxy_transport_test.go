package proxy

import (
	"crypto/tls"
	"crypto/x509"
	"io"
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
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.DisableKeepAlives = true
	transport := NewProxyAwareTransportWithOptions(base, ProxyTransportOptions{
		MaxIdleConns:        17,
		MaxIdleConnsPerHost: 1,
		MaxConnsPerHost:     3000,
		IdleConnTimeout:     7 * time.Second,
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
	if transport.DisableKeepAlives {
		t.Fatal("expected HTTP connection reuse to be enabled")
	}
	if transport.Protocols == nil || !transport.Protocols.HTTP1() || !transport.Protocols.HTTP2() ||
		transport.Protocols.UnencryptedHTTP2() || !transport.ForceAttemptHTTP2 {
		t.Fatalf("unexpected upstream protocols: %v", transport.Protocols)
	}
}

func TestProxyTransportPrefersHTTP2OverTLS(t *testing.T) {
	var protocol string
	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		protocol = r.Proto
		_, _ = io.WriteString(w, "ok")
	}))
	upstream.EnableHTTP2 = true
	upstream.StartTLS()
	defer upstream.Close()

	roots := x509.NewCertPool()
	roots.AddCert(upstream.Certificate())
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	transport := NewProxyAwareTransport(base)
	defer transport.CloseIdleConnections()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, upstream.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatalf("HTTP/2 upstream request failed: %v", err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if protocol != "HTTP/2.0" {
		t.Fatalf("upstream did not negotiate HTTP/2: %q", protocol)
	}
}

func TestProxyTransportFallsBackToHTTP1(t *testing.T) {
	var protocol string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		protocol = r.Proto
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()
	transport := NewProxyAwareTransport(http.DefaultTransport.(*http.Transport))
	defer transport.CloseIdleConnections()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, upstream.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatalf("HTTP/1.1 upstream request failed: %v", err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if protocol != "HTTP/1.1" {
		t.Fatalf("upstream did not fall back to HTTP/1.1: %q", protocol)
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
