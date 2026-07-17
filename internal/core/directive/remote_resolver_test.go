package directive

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/proxy"
)

type remoteReaderFunc func(context.Context, RemoteSpec, *http.Request) ([]byte, error)

func (f remoteReaderFunc) Read(ctx context.Context, spec RemoteSpec, req *http.Request) ([]byte, error) {
	return f(ctx, spec, req)
}

func TestRemotePreparedDereferencesPayloadOnceFromOriginalRequestMetadata(t *testing.T) {
	var calls int
	resolver := NewResolver(ResolverOptions{RemoteReader: remoteReaderFunc(func(_ context.Context, _ RemoteSpec, req *http.Request) ([]byte, error) {
		calls++
		if req.Method != http.MethodPost || req.Host != "proxy.local" || req.URL.Path != "/v1/chat" || req.Header.Get("X-Tenant") != "original" {
			t.Fatalf("remote resolver saw mutated request metadata: method=%s host=%s url=%s headers=%#v", req.Method, req.Host, req.URL, req.Header)
		}
		return []byte(`{"target":{"url":"https://one.example"},"headers":{"ops":[{"side":"request","op":"set","name":"X-Route","values":["one"]}]},"program":{"request":[{"id":"capture","module":"builtin.capture","config":{}}],"attempt":[{"id":"usage","module":"builtin.llmusage","config":{"protocol":"openai.responses"}}]},"recovery":{"controller":{"url":"https://controller.example/recovery"},"triggers":{"transport_error":true},"budget":{"max_attempts":3}}}`), nil
	})})
	token, err := EncodeRemote(RemoteSpec{Type: RemoteTypeHTTP, URL: "https://resolver.example/resolve", Key: "routing"})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/chat", strings.NewReader("body"))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Tenant", "original")
	prepared, err := resolver.Prepare(req)
	if err != nil {
		t.Fatal(err)
	}
	req.Method = http.MethodDelete
	req.Host = "mutated.local"
	req.URL.Path = "/mutated"
	req.Header.Set("X-Tenant", "mutated")

	first, err := prepared.ResolveAttempt(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	second, err := prepared.ResolveAttempt(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || first.Plan.Target.Host != "one.example" || second.Plan.Target.Host != "one.example" {
		t.Fatalf("remote payload was not dereferenced once: calls=%d first=%#v second=%#v", calls, first.Plan, second.Plan)
	}
	if len(prepared.RequestProgram()) != 1 || len(first.Plan.Modules) != 1 || first.Plan.Recovery == nil {
		t.Fatalf("remote payload did not preserve inline program/recovery semantics: prepared=%#v plan=%#v", prepared.RequestProgram(), first.Plan)
	}
	if first.Source.PayloadSHA256 == "" || first.Source.PayloadSHA256 != second.Source.PayloadSHA256 {
		t.Fatalf("unexpected remote payload digests: first=%q second=%q", first.Source.PayloadSHA256, second.Source.PayloadSHA256)
	}
}

func TestResolverLoadsCompleteRemoteDirective(t *testing.T) {
	var requested RemoteSpec
	resolver := NewResolver(ResolverOptions{
		RemoteReader: remoteReaderFunc(func(_ context.Context, spec RemoteSpec, _ *http.Request) ([]byte, error) {
			requested = spec
			return []byte(`{"target":{"url":"https://remote.example.com/v1"},"headers":{"ops":[{"side":"request","op":"set","name":"X-Source","values":["remote"]}]}}`), nil
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

	resolution, err := resolveRequest(resolver, req)
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
	if len(plan.Headers.Request.StripBeforeOps) != 1 || len(plan.Headers.Request.Ops) != 1 || plan.Headers.Request.Ops[0].Values[0] != "remote" {
		t.Fatalf("unexpected request header plan: %#v", plan.Headers.Request)
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
			_, err := resolveRequest(NewResolver(tt.opts), newRequest())
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
		if _, err := resolveRequest(NewResolver(opts), request()); !errors.Is(err, proxy.ErrDirectiveTokenTooLarge) {
			t.Fatalf("unexpected size error: %v", err)
		}
	}
}
