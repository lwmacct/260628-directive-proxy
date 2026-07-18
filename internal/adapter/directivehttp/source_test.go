package directivehttp

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/directive"
)

func testSource() *Source {
	return New(Options{Timeout: time.Second, MaxPayloadBytes: 64 << 10})
}

func TestSourceConfiguresHTTP2AndConnectionReuse(t *testing.T) {
	source := New(Options{
		Timeout: time.Second, MaxPayloadBytes: 64 << 10,
		MaxIdleConns: 256, MaxIdleConnsPerHost: 64, MaxConnsPerHost: 32, IdleConnTimeout: 2 * time.Minute,
	})
	t.Cleanup(func() { _ = source.Close() })
	transport := source.transport
	if transport.Protocols == nil || !transport.Protocols.HTTP1() || !transport.Protocols.HTTP2() || transport.Protocols.UnencryptedHTTP2() ||
		!transport.ForceAttemptHTTP2 || transport.MaxIdleConns != 256 || transport.MaxIdleConnsPerHost != 64 ||
		transport.MaxConnsPerHost != 32 || transport.IdleConnTimeout != 2*time.Minute {
		t.Fatalf("unexpected HTTP resolver transport: %#v protocols=%v", transport, transport.Protocols)
	}
}

func TestSourcePrefersHTTP2OverTLS(t *testing.T) {
	var protocol string
	resolver := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		protocol = r.Proto
		_, _ = w.Write([]byte(`{"target":{"base_url":"https://api.example.com"}}`))
	}))
	resolver.EnableHTTP2 = true
	resolver.StartTLS()
	defer resolver.Close()

	roots := x509.NewCertPool()
	roots.AddCert(resolver.Certificate())
	source := testSource()
	source.transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	t.Cleanup(func() { _ = source.Close() })
	reference := testHTTPReference(t, directive.HTTPRemoteSpec{URL: resolver.URL})
	request := httptest.NewRequest(http.MethodGet, "http://gateway.local/", nil)
	if _, err := source.Read(t.Context(), reference, testRequestSnapshot(request)); err != nil {
		t.Fatalf("resolve over HTTP/2 failed: %v", err)
	}
	if protocol != "HTTP/2.0" {
		t.Fatalf("resolver did not negotiate HTTP/2: %q", protocol)
	}
}

func TestSourceFallsBackToHTTP1(t *testing.T) {
	var protocol string
	resolver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		protocol = r.Proto
		_, _ = w.Write([]byte(`{"target":{"base_url":"https://api.example.com"}}`))
	}))
	defer resolver.Close()
	source := testSource()
	t.Cleanup(func() { _ = source.Close() })
	reference := testHTTPReference(t, directive.HTTPRemoteSpec{URL: resolver.URL})
	request := httptest.NewRequest(http.MethodGet, "http://gateway.local/", nil)
	if _, err := source.Read(t.Context(), reference, testRequestSnapshot(request)); err != nil {
		t.Fatalf("resolve over HTTP/1.1 failed: %v", err)
	}
	if protocol != "HTTP/1.1" {
		t.Fatalf("resolver did not fall back to HTTP/1.1: %q", protocol)
	}
}

func testHTTPReference(t *testing.T, spec directive.HTTPRemoteSpec) directive.HTTPReference {
	t.Helper()
	endpoint, err := url.Parse(spec.URL)
	if err != nil {
		t.Fatal(err)
	}
	headers, err := directive.CompileResolverRequestHeaders(spec.Headers)
	if err != nil {
		t.Fatal(err)
	}
	return directive.HTTPReference{Endpoint: *endpoint, Headers: headers}
}

func testRequestSnapshot(req *http.Request) directive.RequestSnapshot {
	return directive.RequestSnapshot{Method: req.Method, URL: req.URL.String(), Host: req.Host, Headers: req.Header.Clone()}
}

func TestSourceCallsResolverWithRequestMetadata(t *testing.T) {
	var got resolveRequest
	resolver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer policy-token" || r.Header.Get("Content-Type") != "application/json" ||
			r.Header.Get("X-Tenant") != "team-a" || r.Header.Get("X-Hop") != "" {
			t.Errorf("unexpected resolver request: method=%s headers=%#v", r.Method, r.Header)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode request: %v", err)
		}
		_, _ = w.Write([]byte(`{"target":{"base_url":"https://api.example.com/v1"}}`))
	}))
	defer resolver.Close()
	source := testSource()
	t.Cleanup(func() { _ = source.Close() })
	req := httptest.NewRequest(http.MethodPost, "https://gateway.example.com/v1/resources?region=cn", nil)
	req.Host = "gateway.example.com"
	req.Header.Set("Authorization", "Bearer dp.21.remote.payload.mac")
	req.Header.Set("X-Tenant", "team-a")
	req.Header.Set("Connection", "X-Hop")
	req.Header.Set("X-Hop", "drop")

	reference := testHTTPReference(t, directive.HTTPRemoteSpec{
		URL: resolver.URL + "/team-a/service-a",
		Headers: &directive.HeaderPolicy{Mutations: []directive.HeaderMutation{{
			Side: directive.HeaderSideRequest, Action: directive.HeaderActionSet, Name: "Authorization", Values: []string{"Bearer policy-token"},
		}}},
	})
	raw, err := source.Read(context.Background(), reference, testRequestSnapshot(req))
	if err != nil || string(raw) != `{"target":{"base_url":"https://api.example.com/v1"}}` {
		t.Fatalf("unexpected response: raw=%s err=%v", raw, err)
	}
	if got.Protocol != "dproxy.resolve.v1" || got.Request.Method != http.MethodPost ||
		got.Request.URL != "https://gateway.example.com/v1/resources?region=cn" || got.Request.Host != "gateway.example.com" {
		t.Fatalf("unexpected metadata: %#v", got)
	}
}

func TestSourceDeleteAllHeaderMutationStartsEmpty(t *testing.T) {
	resolver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("Content-Type") != "" || r.Header.Get("User-Agent") != "" || r.Header.Get("X-Policy") != "resolver" {
			t.Errorf("unexpected resolver headers: %#v", r.Header)
		}
		_, _ = w.Write([]byte(`{"target":{"base_url":"https://api.example.com"}}`))
	}))
	defer resolver.Close()
	source := testSource()
	t.Cleanup(func() { _ = source.Close() })
	req := httptest.NewRequest(http.MethodGet, "http://gateway.local/", nil)
	req.Header.Set("Cookie", "session=secret")
	reference := testHTTPReference(t, directive.HTTPRemoteSpec{
		URL: resolver.URL,
		Headers: &directive.HeaderPolicy{Mutations: []directive.HeaderMutation{
			{Side: directive.HeaderSideRequest, Action: directive.HeaderActionDel, Glob: "*"},
			{Side: directive.HeaderSideRequest, Action: directive.HeaderActionSet, Name: "X-Policy", Values: []string{"resolver"}},
		}},
	})
	if _, err := source.Read(context.Background(), reference, testRequestSnapshot(req)); err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
}

func TestSourceDefaultPolicyStripsReservedHeaders(t *testing.T) {
	resolver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("X-Dp-Secret") != "" || r.Header.Get("X-Forwarded-For") != "" ||
			r.Header.Get("Connection") != "" || r.Header.Get("Upgrade") != "" || r.Header.Get("X-Tenant") != "team-a" ||
			r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected resolver headers: %#v", r.Header)
		}
		_, _ = w.Write([]byte(`{"target":{"base_url":"https://api.example.com"}}`))
	}))
	defer resolver.Close()
	source := testSource()
	t.Cleanup(func() { _ = source.Close() })
	req := httptest.NewRequest(http.MethodPost, "http://gateway.local/", nil)
	req.Header.Set("Authorization", "Bearer dp.21.remote.payload.mac")
	req.Header.Set("X-Dp-Secret", "drop")
	req.Header.Set("X-Forwarded-For", "192.0.2.1")
	req.Header.Set("X-Tenant", "team-a")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	reference := testHTTPReference(t, directive.HTTPRemoteSpec{URL: resolver.URL})
	if _, err := source.Read(context.Background(), reference, testRequestSnapshot(req)); err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
}

func TestSourceStatusAndLimits(t *testing.T) {
	for _, tt := range []struct {
		name    string
		status  int
		body    string
		wantErr error
	}{
		{name: "not found", status: http.StatusNotFound, wantErr: directive.ErrRemoteNotFound},
		{name: "no content", status: http.StatusNoContent, wantErr: directive.ErrRemoteNotFound},
		{name: "unavailable", status: http.StatusTooManyRequests, wantErr: directive.ErrRemoteUnavailable},
		{name: "oversized", status: http.StatusOK, body: "123456789", wantErr: directive.ErrRemoteInvalid},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()
			source := New(Options{Timeout: time.Second, MaxPayloadBytes: 8})
			defer func() { _ = source.Close() }()
			reference := testHTTPReference(t, directive.HTTPRemoteSpec{URL: server.URL})
			request := httptest.NewRequest(http.MethodGet, "http://gateway.local/", nil)
			_, err := source.Read(context.Background(), reference, testRequestSnapshot(request))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("unexpected error: got %v want %v", err, tt.wantErr)
			}
		})
	}
}
