package directive

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/core/proxy"
)

type storeFunc func(context.Context, string) ([]byte, error)

func (f storeFunc) Get(ctx context.Context, key string) ([]byte, error) {
	return f(ctx, key)
}

func TestResolverLoadsCompleteDirectiveFromRedis(t *testing.T) {
	var requestedKey string
	resolver := NewResolver(ResolverOptions{
		Store: storeFunc(func(_ context.Context, key string) ([]byte, error) {
			requestedKey = key
			return []byte(`{"target":{"url":"https://redis.example.com/v1"},"headers":{"ops":[{"op":"=","name":"X-Source","values":["redis"]}]}}`), nil
		}),
		LookupTimeout: time.Second,
		MaxValueBytes: 1024,
	})
	token, err := EncodeRedisKey("team-a/openai")
	if err != nil {
		t.Fatalf("encode token failed: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/chat", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	plan, err := resolver.Resolve(req)
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if requestedKey != "team-a/openai" || plan.Target.String() != "https://redis.example.com/v1" {
		t.Fatalf("unexpected resolved directive: key=%q plan=%#v", requestedKey, plan)
	}
	if plan.DirectiveSource != "redis" || plan.DirectiveKey != "team-a/openai" {
		t.Fatalf("unexpected directive metadata: %#v", plan)
	}
	if len(plan.HeaderOps) != 2 || plan.HeaderOps[1].Values[0] != "redis" {
		t.Fatalf("unexpected header ops: %#v", plan.HeaderOps)
	}
}

func TestResolverRedisFailures(t *testing.T) {
	token, err := EncodeRedisKey("team-a/openai")
	if err != nil {
		t.Fatalf("encode token failed: %v", err)
	}
	newRequest := func() *http.Request {
		req := httptest.NewRequest(http.MethodGet, "http://proxy.local/", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		return req
	}

	tests := []struct {
		name    string
		opts    ResolverOptions
		wantErr error
	}{
		{name: "store disabled", wantErr: proxy.ErrDirectiveStoreUnavailable},
		{
			name: "missing",
			opts: ResolverOptions{Store: storeFunc(func(context.Context, string) ([]byte, error) {
				return nil, ErrStoreKeyNotFound
			})},
			wantErr: proxy.ErrDirectiveNotFound,
		},
		{
			name: "invalid stored payload",
			opts: ResolverOptions{Store: storeFunc(func(context.Context, string) ([]byte, error) {
				return []byte(`{"target":{}}`), nil
			})},
			wantErr: proxy.ErrStoredDirectiveInvalid,
		},
		{
			name: "value too large",
			opts: ResolverOptions{
				Store: storeFunc(func(context.Context, string) ([]byte, error) {
					return []byte(`{"target":{"url":"https://api.example.com"}}`), nil
				}),
				MaxValueBytes: 8,
			},
			wantErr: proxy.ErrStoredDirectiveInvalid,
		},
		{
			name: "timeout",
			opts: ResolverOptions{
				Store: storeFunc(func(ctx context.Context, _ string) ([]byte, error) {
					<-ctx.Done()
					return nil, ctx.Err()
				}),
				LookupTimeout: time.Millisecond,
			},
			wantErr: proxy.ErrDirectiveStoreUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewResolver(tt.opts).Resolve(newRequest())
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("unexpected error: got %v want %v", err, tt.wantErr)
			}
		})
	}
}
