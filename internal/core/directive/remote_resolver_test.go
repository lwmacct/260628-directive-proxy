package directive

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/proxy"
)

type remoteReaderFunc func(context.Context, RemoteSpec, *http.Request) ([]byte, error)

func (f remoteReaderFunc) Read(ctx context.Context, spec RemoteSpec, req *http.Request) ([]byte, error) {
	return f(ctx, spec, req)
}

func TestResolverLoadsCompleteRemoteDirective(t *testing.T) {
	var requested RemoteSpec
	resolver := NewResolver(ResolverOptions{
		RemoteReader: remoteReaderFunc(func(_ context.Context, spec RemoteSpec, _ *http.Request) ([]byte, error) {
			requested = spec
			return []byte(`{"target":{"url":"https://remote.example.com/v1"},"headers":{"ops":[{"op":"=","name":"X-Source","values":["remote"]}]}}`), nil
		}),
		LookupTimeout: time.Second,
	})
	spec := RemoteSpec{Type: RemoteTypeHTTP, URL: "https://policy.example.com/v1/resolve?secret=hidden", Key: "team-a/service-a"}
	token, err := EncodeRemote(spec)
	if err != nil {
		t.Fatalf("encode token failed: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/resources", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resolution, err := resolver.Resolve(req)
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	plan := resolution.Plan
	if requested.Key != spec.Key || plan.Target.String() != "https://remote.example.com/v1" {
		t.Fatalf("unexpected resolved directive: spec=%#v plan=%#v", requested, plan)
	}
	if resolution.Source.Mode != "remote" || resolution.Source.Backend != "http" ||
		resolution.Source.Endpoint != "https://policy.example.com/v1/resolve" || resolution.Source.Key != spec.Key {
		t.Fatalf("unexpected directive metadata: %#v", resolution.Source)
	}
	if len(plan.HeaderOps) != 2 || plan.HeaderOps[1].Values[0] != "remote" {
		t.Fatalf("unexpected header ops: %#v", plan.HeaderOps)
	}
}

func TestResolverRemoteFailures(t *testing.T) {
	token, err := EncodeRemote(RemoteSpec{Type: RemoteTypeRedis, URL: "redis://redis.example.com:6379/0", Key: "team-a/service-a"})
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
		{name: "reader disabled", wantErr: proxy.ErrRemoteDirectiveUnavailable},
		{name: "missing", opts: ResolverOptions{RemoteReader: remoteReaderFunc(func(context.Context, RemoteSpec, *http.Request) ([]byte, error) {
			return nil, ErrRemoteNotFound
		})}, wantErr: proxy.ErrDirectiveNotFound},
		{name: "metadata too large", opts: ResolverOptions{RemoteReader: remoteReaderFunc(func(context.Context, RemoteSpec, *http.Request) ([]byte, error) {
			return nil, ErrRemoteMetadataTooBig
		})}, wantErr: proxy.ErrDirectiveMetadataTooLarge},
		{name: "invalid payload", opts: ResolverOptions{RemoteReader: remoteReaderFunc(func(context.Context, RemoteSpec, *http.Request) ([]byte, error) {
			return []byte(`{"target":{}}`), nil
		})}, wantErr: proxy.ErrRemoteDirectiveInvalid},
		{name: "timeout", opts: ResolverOptions{RemoteReader: remoteReaderFunc(func(ctx context.Context, _ RemoteSpec, _ *http.Request) ([]byte, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}), LookupTimeout: time.Millisecond}, wantErr: proxy.ErrRemoteDirectiveUnavailable},
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

func TestResolverRejectsOversizedTokenAndInlinePayload(t *testing.T) {
	token, err := Encode(Payload{Target: TargetSection{URL: "https://api.example.com"}})
	if err != nil {
		t.Fatalf("encode token: %v", err)
	}
	request := func() *http.Request {
		req := httptest.NewRequest(http.MethodGet, "http://proxy.local/", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		return req
	}
	for _, opts := range []ResolverOptions{
		{MaxTokenBytes: int64(len(token) - 1)},
		{MaxInlineBytes: 1},
	} {
		if _, err := NewResolver(opts).Resolve(request()); !errors.Is(err, proxy.ErrDirectiveTokenTooLarge) {
			t.Fatalf("unexpected size error: %v", err)
		}
	}
}
