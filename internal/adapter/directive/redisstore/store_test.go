package redisstore

import (
	"context"
	"errors"
	"testing"

	"github.com/alicebob/miniredis/v2"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/core/directive"
)

func TestStoreReadsPrefixedDirective(t *testing.T) {
	server := miniredis.RunT(t)
	server.Set("dproxy:12:directive:team-a/openai", `{"target":{"url":"https://api.example.com"}}`)
	store, err := New("redis://"+server.Addr()+"/0", "dproxy:12:directive:")
	if err != nil {
		t.Fatalf("create store failed: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	raw, err := store.Get(context.Background(), "team-a/openai")
	if err != nil {
		t.Fatalf("get directive failed: %v", err)
	}
	if string(raw) != `{"target":{"url":"https://api.example.com"}}` {
		t.Fatalf("unexpected directive: %s", raw)
	}
}

func TestStoreMapsMissingKey(t *testing.T) {
	server := miniredis.RunT(t)
	store, err := New("redis://"+server.Addr()+"/0", "dproxy:12:directive:")
	if err != nil {
		t.Fatalf("create store failed: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	_, err = store.Get(context.Background(), "missing")
	if !errors.Is(err, directive.ErrStoreKeyNotFound) {
		t.Fatalf("unexpected error: %v", err)
	}
}
