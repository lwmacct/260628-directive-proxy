package directive

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/program"
	"github.com/lwmacct/260628-directive-proxy/internal/core/proxy"
)

type httpReaderFunc func(context.Context, HTTPReference, RequestSnapshot) ([]byte, error)

func (f httpReaderFunc) Read(ctx context.Context, reference HTTPReference, request RequestSnapshot) ([]byte, error) {
	return f(ctx, reference, request)
}

type redisReaderFunc func(context.Context, RedisReference) ([]byte, error)

func (f redisReaderFunc) Read(ctx context.Context, reference RedisReference) ([]byte, error) {
	return f(ctx, reference)
}

type fileReaderFunc func(context.Context, FileReference) ([]byte, error)

func (f fileReaderFunc) Read(ctx context.Context, reference FileReference) ([]byte, error) {
	return f(ctx, reference)
}

func TestRemotePreparedDereferencesPayloadOnceFromOriginalRequestMetadata(t *testing.T) {
	var calls int
	var compileCalls int
	resolver := newTestResolver(ResolverOptions{Compiler: compilerFunc(func(source program.Program) (*program.Executable, error) {
		compileCalls++
		if len(source.Request) != 1 || len(source.Attempt) != 1 {
			t.Fatalf("compiler received incomplete program: %#v", source)
		}
		return &program.Executable{}, nil
	}), HTTPReader: httpReaderFunc(func(_ context.Context, _ HTTPReference, req RequestSnapshot) ([]byte, error) {
		calls++
		if req.Method != http.MethodPost || req.Host != "proxy.local" || req.URL != "http://proxy.local/v1/chat" || req.Headers.Get("X-Tenant") != "original" {
			t.Fatalf("remote resolver saw mutated request metadata: method=%s host=%s url=%s headers=%#v", req.Method, req.Host, req.URL, req.Headers)
		}
		return []byte(`{"target":{"base_url":"https://one.example"},"headers":{"mutations":[{"side":"request","action":"set","name":"X-Route","values":["one"]}]},"program":{"request":[{"id":"capture","module":"builtin.capture","config":{}}],"attempt":[{"id":"usage","module":"builtin.llmusage","config":{"protocol":"openai.responses"}}]},"recovery":{"controller":{"url":"https://controller.example/recovery"},"triggers":{"transport_error":true},"budget":{"max_attempts":3}}}`), nil
	})})
	token, err := EncodeRemote(testTokenSecret, RemoteSpec{HTTP: &HTTPRemoteSpec{URL: "https://resolver.example/routing"}})
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
	if calls != 1 || compileCalls != 1 || first.Plan.Target.Host != "one.example" || second.Plan.Target.Host != "one.example" {
		t.Fatalf("remote payload was not prepared once: reads=%d compiles=%d first=%#v second=%#v", calls, compileCalls, first.Plan, second.Plan)
	}
	if prepared.Program() == nil || first.Plan.Recovery == nil {
		t.Fatalf("remote payload did not preserve program/recovery semantics: executable=%#v plan=%#v", prepared.Program(), first.Plan)
	}
	if first.Source.PayloadSHA256 == "" || first.Source.PayloadSHA256 != second.Source.PayloadSHA256 {
		t.Fatalf("unexpected remote payload digests: first=%q second=%q", first.Source.PayloadSHA256, second.Source.PayloadSHA256)
	}
}

func TestResolverLoadsCompleteRemoteDirective(t *testing.T) {
	var requested HTTPReference
	resolver := newTestResolver(ResolverOptions{
		HTTPReader: httpReaderFunc(func(_ context.Context, reference HTTPReference, _ RequestSnapshot) ([]byte, error) {
			requested = reference
			return []byte(`{"target":{"base_url":"https://remote.example.com/v1"},"headers":{"mutations":[{"side":"request","action":"set","name":"X-Source","values":["remote"]}]}}`), nil
		}),
		LookupTimeout: time.Second,
	})
	spec := RemoteSpec{
		HTTP: &HTTPRemoteSpec{
			URL: "https://policy.example.com/v1/team-a/service-a?secret=hidden",
			Headers: &HeaderPolicy{Mutations: []HeaderMutation{{
				Side: HeaderSideRequest, Action: HeaderActionSet, Name: "Authorization", Values: []string{"Bearer resolver"},
			}},
			},
		},
	}
	token, err := EncodeRemote(testTokenSecret, spec)
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
	if requested.Endpoint.String() != spec.HTTP.URL || len(requested.Headers.Ops) != 1 ||
		requested.Headers.Ops[0].Values[0] != "Bearer resolver" || plan.Target.String() != "https://remote.example.com/v1/v1/resources" {
		t.Fatalf("unexpected resolved directive: spec=%#v plan=%#v", requested, plan)
	}
	if resolution.Source.Mode != "remote" || resolution.Source.Backend != "http" ||
		resolution.Source.Endpoint != spec.HTTP.URL || resolution.Source.Resource != "" {
		t.Fatalf("unexpected directive metadata: %#v", resolution.Source)
	}
	if len(plan.Headers.Request.StripBeforeOps) != 1 || len(plan.Headers.Request.Ops) != 1 || plan.Headers.Request.Ops[0].Values[0] != "remote" {
		t.Fatalf("unexpected request header plan: %#v", plan.Headers.Request)
	}
}

func TestResolverLoadsCompleteFileDirective(t *testing.T) {
	var requested FileReference
	resolver := newTestResolver(ResolverOptions{FileReader: fileReaderFunc(func(_ context.Context, reference FileReference) ([]byte, error) {
		requested = reference
		return []byte(`{"target":{"base_url":"https://file.example.com/v1"}}`), nil
	})})
	token, err := EncodeRemote(testTokenSecret, RemoteSpec{File: &FileRemoteSpec{Path: "team-a/services/primary.json"}})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://proxy.local/", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	resolution, err := resolveRequest(resolver, request)
	if err != nil {
		t.Fatal(err)
	}
	if requested.Path != "team-a/services/primary.json" || resolution.Plan.Target.String() != "https://file.example.com/v1/" {
		t.Fatalf("unexpected file resolution: reference=%#v plan=%#v", requested, resolution.Plan)
	}
	if resolution.Source.Backend != RemoteTypeFile || resolution.Source.Endpoint != "" || resolution.Source.Resource != requested.Path {
		t.Fatalf("unexpected file source metadata: %#v", resolution.Source)
	}
}

func TestResolverRemoteFailures(t *testing.T) {
	token, err := EncodeRemote(testTokenSecret, RemoteSpec{Redis: &RedisRemoteSpec{URL: "redis://redis.example.com:6379/0", Key: "team-a/service-a"}})
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
		{name: "missing", opts: ResolverOptions{RedisReader: redisReaderFunc(func(context.Context, RedisReference) ([]byte, error) {
			return nil, ErrRemoteNotFound
		})}, wantErr: proxy.ErrDirectiveNotFound},
		{name: "invalid payload", opts: ResolverOptions{RedisReader: redisReaderFunc(func(context.Context, RedisReference) ([]byte, error) {
			return []byte(`{"target":{}}`), nil
		})}, wantErr: proxy.ErrRemoteDirectiveInvalid},
		{name: "timeout", opts: ResolverOptions{RedisReader: redisReaderFunc(func(ctx context.Context, _ RedisReference) ([]byte, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}), LookupTimeout: time.Millisecond}, wantErr: proxy.ErrRemoteDirectiveUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolveRequest(newTestResolver(tt.opts), newRequest())
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("unexpected error: got %v want %v", err, tt.wantErr)
			}
		})
	}
}

func TestResolverRejectsOversizedTokenAndInlinePayload(t *testing.T) {
	token, err := Encode(testTokenSecret, Payload{Target: TargetSection{BaseURL: "https://api.example.com"}})
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
	} {
		if _, err := resolveRequest(newTestResolver(opts), request()); !errors.Is(err, proxy.ErrDirectiveTokenTooLarge) {
			t.Fatalf("unexpected size error: %v", err)
		}
	}
}
