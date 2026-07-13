package remote

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	miniredisServer "github.com/alicebob/miniredis/v2/server"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/core/directive"
)

func testOptions() Options {
	return Options{
		Timeout:                  time.Second,
		MaxRequestBytes:          64 << 10,
		MaxResponseBytes:         64 << 10,
		RedisClientCacheCapacity: 2,
		RedisClientIdleTimeout:   time.Minute,
		RedisPoolSize:            2,
	}
}

func enableRedisJSON(t *testing.T, redisServer *miniredis.Miniredis) {
	t.Helper()
	if err := redisServer.Server().Register("JSON.GET", func(peer *miniredisServer.Peer, _ string, args []string) {
		if len(args) != 1 {
			peer.WriteError("ERR wrong number of arguments for 'json.get' command")
			return
		}
		value, err := redisServer.Get(args[0])
		if err != nil {
			peer.WriteNull()
			return
		}
		peer.WriteBulk(value)
	}); err != nil {
		t.Fatalf("register JSON.GET: %v", err)
	}
}

func TestReaderCallsHTTPResolverWithRequestMetadata(t *testing.T) {
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
	reader := New(testOptions())
	t.Cleanup(func() { _ = reader.Close() })
	req := httptest.NewRequest(http.MethodPost, "https://relay.example.com/v1/chat?region=cn", nil)
	req.Host = "relay.example.com"
	req.Header.Set("Authorization", "Bearer dproxy.14.r.secret")
	req.Header.Set("X-Tenant", "team-a")
	req.Header.Set("Connection", "X-Hop")
	req.Header.Set("X-Hop", "drop")

	raw, err := reader.Read(context.Background(), directive.RemoteSpec{
		Type:           directive.RemoteTypeHTTP,
		URL:            resolver.URL,
		Key:            "team-a/openai",
		RequestHeaders: []string{"Authorization", "X-Hop", "X-Tenant"},
		Headers: map[string]string{
			"Authorization": "Bearer policy-token",
		},
	}, req)
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if string(raw) != `{"target":{"url":"https://api.example.com/v1"}}` {
		t.Fatalf("unexpected response: %s", raw)
	}
	if got.Protocol != "dproxy.resolve.v1" || got.Key != "team-a/openai" || got.Request.Method != http.MethodPost ||
		got.Request.URL != "https://relay.example.com/v1/chat?region=cn" || got.Request.Host != "relay.example.com" {
		t.Fatalf("unexpected metadata: %#v", got)
	}
	if got.Request.Headers["X-Tenant"][0] != "team-a" || got.Request.Headers["Authorization"] != nil || got.Request.Headers["X-Hop"] != nil {
		t.Fatalf("unexpected forwarded headers: %#v", got.Request.Headers)
	}
}

func TestReaderDoesNotDiscloseRequestHeadersByDefault(t *testing.T) {
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
	reader := New(testOptions())
	t.Cleanup(func() { _ = reader.Close() })
	req := httptest.NewRequest(http.MethodGet, "http://relay.local/", nil)
	req.Header.Set("Cookie", "session=secret")
	req.Header.Set("X-API-Key", "secret")
	if _, err := reader.Read(context.Background(), directive.RemoteSpec{Type: directive.RemoteTypeHTTP, URL: resolver.URL}, req); err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
}

func TestReaderHTTPStatusAndLimits(t *testing.T) {
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
			opts := testOptions()
			opts.MaxResponseBytes = 8
			reader := New(opts)
			defer func() { _ = reader.Close() }()
			_, err := reader.Read(context.Background(), directive.RemoteSpec{Type: directive.RemoteTypeHTTP, URL: server.URL}, httptest.NewRequest(http.MethodGet, "http://relay.local/", nil))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("unexpected error: got %v want %v", err, tt.wantErr)
			}
		})
	}
}

func TestReaderReadsExactRedisKeyAndReusesClient(t *testing.T) {
	server := miniredis.RunT(t)
	enableRedisJSON(t, server)
	server.Set("team-a/openai", `{"target":{"url":"https://api.example.com"}}`)
	reader := New(testOptions())
	t.Cleanup(func() { _ = reader.Close() })
	spec := directive.RemoteSpec{Type: directive.RemoteTypeRedis, URL: "redis://" + server.Addr() + "/0", Key: "team-a/openai"}
	for range 2 {
		raw, err := reader.Read(context.Background(), spec, nil)
		if err != nil || string(raw) != `{"target":{"url":"https://api.example.com"}}` {
			t.Fatalf("unexpected Redis result: raw=%s err=%v", raw, err)
		}
	}
	reader.redisClients.mu.Lock()
	entries := len(reader.redisClients.entries)
	reader.redisClients.mu.Unlock()
	if entries != 1 {
		t.Fatalf("expected one cached Redis client, got %d", entries)
	}
}

func TestReaderReturnsNotFoundForMissingRedisJSONDocument(t *testing.T) {
	server := miniredis.RunT(t)
	enableRedisJSON(t, server)
	reader := New(testOptions())
	t.Cleanup(func() { _ = reader.Close() })

	_, err := reader.Read(context.Background(), directive.RemoteSpec{
		Type: directive.RemoteTypeRedis,
		URL:  "redis://" + server.Addr() + "/0",
		Key:  "missing",
	}, nil)
	if !errors.Is(err, directive.ErrRemoteNotFound) {
		t.Fatalf("unexpected missing document error: %v", err)
	}
}

func TestRedisClientCacheRejectsNewURLWhenAllEntriesActive(t *testing.T) {
	one := miniredis.RunT(t)
	two := miniredis.RunT(t)
	cache := newRedisClientCache(1, time.Minute, 1, time.Second)
	t.Cleanup(func() { _ = cache.close() })
	_, release, err := cache.acquire("redis://" + one.Addr() + "/0")
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	defer release()
	if _, _, err := cache.acquire("redis://" + two.Addr() + "/0"); !errors.Is(err, directive.ErrRemoteUnavailable) {
		t.Fatalf("unexpected capacity error: %v", err)
	}
}
