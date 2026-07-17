package remotehttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/directive"
)

func testSource() *Source {
	return New(Options{Timeout: time.Second, MaxRequestBytes: 64 << 10, MaxResponseBytes: 64 << 10})
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
		_, _ = w.Write([]byte(`{"target":{"url":"https://api.example.com/v1"}}`))
	}))
	defer resolver.Close()
	source := testSource()
	t.Cleanup(func() { _ = source.Close() })
	req := httptest.NewRequest(http.MethodPost, "https://gateway.example.com/v1/resources?region=cn", nil)
	req.Host = "gateway.example.com"
	req.Header.Set("Authorization", "Bearer dp.18.remote.secret")
	req.Header.Set("X-Tenant", "team-a")
	req.Header.Set("Connection", "X-Hop")
	req.Header.Set("X-Hop", "drop")

	raw, err := source.Read(context.Background(), directive.RemoteSpec{
		Type: directive.RemoteTypeHTTP, URL: resolver.URL, Key: "team-a/service-a",
		Headers: &directive.HeaderPolicy{Mutations: []directive.HeaderMutation{{
			Side: directive.HeaderSideRequest, Action: directive.HeaderActionSet, Name: "Authorization", Values: []string{"Bearer policy-token"},
		}}},
	}, req)
	if err != nil || string(raw) != `{"target":{"url":"https://api.example.com/v1"}}` {
		t.Fatalf("unexpected response: raw=%s err=%v", raw, err)
	}
	if got.Protocol != "dproxy.resolve.v1" || got.Key != "team-a/service-a" || got.Request.Method != http.MethodPost ||
		got.Request.URL != "https://gateway.example.com/v1/resources?region=cn" || got.Request.Host != "gateway.example.com" {
		t.Fatalf("unexpected metadata: %#v", got)
	}
}

func TestSourceReplaceHeaderPolicyStartsEmpty(t *testing.T) {
	resolver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("X-Policy") != "resolver" {
			t.Errorf("unexpected resolver headers: %#v", r.Header)
		}
		_, _ = w.Write([]byte(`{"target":{"url":"https://api.example.com"}}`))
	}))
	defer resolver.Close()
	source := testSource()
	t.Cleanup(func() { _ = source.Close() })
	req := httptest.NewRequest(http.MethodGet, "http://gateway.local/", nil)
	req.Header.Set("Cookie", "session=secret")
	if _, err := source.Read(context.Background(), directive.RemoteSpec{
		Type: directive.RemoteTypeHTTP,
		URL:  resolver.URL,
		Headers: &directive.HeaderPolicy{Mode: "replace", Mutations: []directive.HeaderMutation{{
			Side: directive.HeaderSideRequest, Action: directive.HeaderActionSet, Name: "X-Policy", Values: []string{"resolver"},
		}}},
	}, req); err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
}

func TestSourceDefaultPolicyStripsReservedHeaders(t *testing.T) {
	resolver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("X-Dproxy-Secret") != "" || r.Header.Get("X-Forwarded-For") != "" ||
			r.Header.Get("Connection") != "" || r.Header.Get("Upgrade") != "" || r.Header.Get("X-Tenant") != "team-a" ||
			r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected resolver headers: %#v", r.Header)
		}
		_, _ = w.Write([]byte(`{"target":{"url":"https://api.example.com"}}`))
	}))
	defer resolver.Close()
	source := testSource()
	t.Cleanup(func() { _ = source.Close() })
	req := httptest.NewRequest(http.MethodPost, "http://gateway.local/", nil)
	req.Header.Set("Authorization", "Bearer dp.18.remote.secret")
	req.Header.Set("X-Dproxy-Secret", "drop")
	req.Header.Set("X-Forwarded-For", "192.0.2.1")
	req.Header.Set("X-Tenant", "team-a")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	if _, err := source.Read(context.Background(), directive.RemoteSpec{Type: directive.RemoteTypeHTTP, URL: resolver.URL}, req); err != nil {
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
			source := New(Options{Timeout: time.Second, MaxRequestBytes: 1024, MaxResponseBytes: 8})
			defer func() { _ = source.Close() }()
			_, err := source.Read(context.Background(), directive.RemoteSpec{Type: directive.RemoteTypeHTTP, URL: server.URL}, httptest.NewRequest(http.MethodGet, "http://gateway.local/", nil))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("unexpected error: got %v want %v", err, tt.wantErr)
			}
		})
	}
}
