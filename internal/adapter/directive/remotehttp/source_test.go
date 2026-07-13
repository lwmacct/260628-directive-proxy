package remotehttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/core/directive"
)

func testSource() *Source {
	return New(Options{Timeout: time.Second, MaxRequestBytes: 64 << 10, MaxResponseBytes: 64 << 10})
}

func TestSourceCallsResolverWithRequestMetadata(t *testing.T) {
	var got resolveRequest
	resolver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer policy-token" || r.Header.Get("Content-Type") != "application/json" {
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
	req := httptest.NewRequest(http.MethodPost, "https://relay.example.com/v1/chat?region=cn", nil)
	req.Host = "relay.example.com"
	req.Header.Set("Authorization", "Bearer dproxy.14.r.secret")
	req.Header.Set("X-Tenant", "team-a")
	req.Header.Set("Connection", "X-Hop")
	req.Header.Set("X-Hop", "drop")

	raw, err := source.Read(context.Background(), directive.RemoteSpec{
		Type: directive.RemoteTypeHTTP, URL: resolver.URL, Key: "team-a/openai",
		RequestHeaders: []string{"Authorization", "X-Hop", "X-Tenant"},
		Headers:        map[string]string{"Authorization": "Bearer policy-token"},
	}, req)
	if err != nil || string(raw) != `{"target":{"url":"https://api.example.com/v1"}}` {
		t.Fatalf("unexpected response: raw=%s err=%v", raw, err)
	}
	if got.Protocol != "dproxy.resolve.v1" || got.Key != "team-a/openai" || got.Request.Method != http.MethodPost ||
		got.Request.URL != "https://relay.example.com/v1/chat?region=cn" || got.Request.Host != "relay.example.com" {
		t.Fatalf("unexpected metadata: %#v", got)
	}
	if got.Request.Headers["X-Tenant"][0] != "team-a" || got.Request.Headers["Authorization"] != nil || got.Request.Headers["X-Hop"] != nil {
		t.Fatalf("unexpected forwarded headers: %#v", got.Request.Headers)
	}
}

func TestSourceDoesNotDiscloseHeadersByDefault(t *testing.T) {
	resolver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got resolveRequest
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if len(got.Request.Headers) != 0 {
			t.Errorf("unexpected disclosed headers: %#v", got.Request.Headers)
		}
		_, _ = w.Write([]byte(`{"target":{"url":"https://api.example.com"}}`))
	}))
	defer resolver.Close()
	source := testSource()
	t.Cleanup(func() { _ = source.Close() })
	req := httptest.NewRequest(http.MethodGet, "http://relay.local/", nil)
	req.Header.Set("Cookie", "session=secret")
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
			_, err := source.Read(context.Background(), directive.RemoteSpec{Type: directive.RemoteTypeHTTP, URL: server.URL}, httptest.NewRequest(http.MethodGet, "http://relay.local/", nil))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("unexpected error: got %v want %v", err, tt.wantErr)
			}
		})
	}
}
