package directiveredis

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	miniredisServer "github.com/alicebob/miniredis/v2/server"

	"github.com/lwmacct/260628-directive-proxy/internal/core/directive"
)

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

func newTestSource() *Source {
	return New(Options{Timeout: time.Second, MaxResponseBytes: 64 << 10, ClientCacheCapacity: 2, ClientIdleTimeout: time.Minute, PoolSize: 2})
}

func testRedisReference(t *testing.T, rawURL, key string) directive.RedisReference {
	t.Helper()
	endpoint, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	return directive.RedisReference{Endpoint: *endpoint, Key: key}
}

func TestSourceReadsExactKey(t *testing.T) {
	server := miniredis.RunT(t)
	enableRedisJSON(t, server)
	if err := server.Set("team-a/service-a", `{"target":{"url":"https://api.example.com"}}`); err != nil {
		t.Fatalf("seed Redis directive: %v", err)
	}
	source := newTestSource()
	t.Cleanup(func() { _ = source.Close() })
	reference := testRedisReference(t, "redis://"+server.Addr()+"/0", "team-a/service-a")
	for range 2 {
		raw, err := source.Read(context.Background(), reference)
		if err != nil || string(raw) != `{"target":{"url":"https://api.example.com"}}` {
			t.Fatalf("unexpected Redis result: raw=%s err=%v", raw, err)
		}
	}
	if entries := len(source.clients.entries); entries != 1 {
		t.Fatalf("expected one cached Redis client, got %d", entries)
	}
}

func TestSourceReturnsNotFound(t *testing.T) {
	server := miniredis.RunT(t)
	enableRedisJSON(t, server)
	source := newTestSource()
	t.Cleanup(func() { _ = source.Close() })
	_, err := source.Read(context.Background(), testRedisReference(t, "redis://"+server.Addr()+"/0", "missing"))
	if !errors.Is(err, directive.ErrRemoteNotFound) {
		t.Fatalf("unexpected missing document error: %v", err)
	}
}

func TestSourceClassifiesWrongTypeAsInvalid(t *testing.T) {
	server := miniredis.RunT(t)
	if err := server.Server().Register("JSON.GET", func(peer *miniredisServer.Peer, _ string, _ []string) {
		peer.WriteError("WRONGTYPE Operation against a key holding the wrong kind of value")
	}); err != nil {
		t.Fatalf("register JSON.GET: %v", err)
	}
	source := newTestSource()
	t.Cleanup(func() { _ = source.Close() })
	_, err := source.Read(context.Background(), testRedisReference(t, "redis://"+server.Addr()+"/0", "legacy-string"))
	if !errors.Is(err, directive.ErrRemoteInvalid) {
		t.Fatalf("unexpected wrong type error: %v", err)
	}
}
