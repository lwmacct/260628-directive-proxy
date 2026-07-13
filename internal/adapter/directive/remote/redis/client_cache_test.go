package remoteredis

import (
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/core/directive"
)

func TestClientCacheRejectsNewURLWhenAllEntriesActive(t *testing.T) {
	one := miniredis.RunT(t)
	two := miniredis.RunT(t)
	cache := newClientCache(1, time.Minute, 1, time.Second)
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
