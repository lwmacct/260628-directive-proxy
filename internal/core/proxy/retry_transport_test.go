package proxy

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	proxyrequestadapter "github.com/lwmacct/260628-directive-proxy/internal/adapter/proxyrequest"
	"github.com/lwmacct/260628-directive-proxy/internal/core/bodymemory"
	"github.com/lwmacct/260628-directive-proxy/internal/core/observability"
	"github.com/lwmacct/260628-directive-proxy/internal/core/proxyrequest"
	"github.com/lwmacct/260628-directive-proxy/internal/core/requestmeta"
)

type rotatingPrepared struct {
	plans []*Plan
	errs  []error
	calls int
}

func TestRetryTransportRejectsDirectiveSpecForDisabledPluginBeforeUpstream(t *testing.T) {
	pipeline, err := observability.NewPipeline(context.Background(), nil, observability.SinkConfig{})
	if err != nil {
		t.Fatal(err)
	}
	tracker := proxyrequestadapter.NewProxyRequestService(proxyrequestadapter.ProxyRequestOptions{MaxAttempts: 2}, pipeline)
	inbound, _ := http.NewRequest(http.MethodGet, "http://proxy.local/chat", nil)
	session := tracker.Start(inbound, proxyrequest.Identity{})
	called := false
	transport, err := NewRetryTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, nil
	}), RetryTransportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	target, _ := url.Parse("https://upstream.example")
	ctx := retryTestContext(t, inbound, session, staticPrepared{resolution: Resolution{Plan: &Plan{
		Target: target, PluginSpecs: map[string][]byte{"llmusage": []byte(`{"protocol":"openai.responses"}`)},
	}}})
	if _, err = transport.RoundTrip(inbound.Clone(ctx)); !errors.Is(err, ErrInvalidDirective) {
		t.Fatalf("unexpected plugin configuration error: %v", err)
	}
	if called {
		t.Fatal("upstream was called for a disabled directive plugin")
	}
	session.Complete()
}

func (*rotatingPrepared) Kind() string { return "remote" }

func (*rotatingPrepared) Source() SourceMetadata {
	return SourceMetadata{Mode: "remote", Backend: "http", Endpoint: "https://resolver.example/resolve", Key: "routing"}
}

func (p *rotatingPrepared) ResolveAttempt(context.Context, int) (Resolution, error) {
	index := p.calls
	p.calls++
	if index < len(p.errs) && p.errs[index] != nil {
		return Resolution{}, p.errs[index]
	}
	if index >= len(p.plans) {
		index = len(p.plans) - 1
	}
	return Resolution{Plan: ClonePlan(p.plans[index]), Source: p.Source()}, nil
}

func TestRetryTransportDoesNotFallBackWhenRemoteRefreshFails(t *testing.T) {
	tracker := proxyrequestadapter.NewProxyRequestService(proxyrequestadapter.ProxyRequestOptions{MaxAttempts: 3}, nil)
	inbound, _ := http.NewRequest(http.MethodGet, "http://proxy.local/chat", nil)
	session := tracker.Start(inbound, proxyrequest.Identity{})
	target, _ := url.Parse("https://one.example")
	prepared := &rotatingPrepared{plans: []*Plan{{Target: target}}, errs: []error{nil, ErrDirectiveNotFound}}
	started := make(chan struct{})
	baseCalls := 0
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		baseCalls++
		close(started)
		<-req.Context().Done()
		return nil, req.Context().Err()
	})
	transport, err := NewRetryTransport(base, RetryTransportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := retryTestContext(t, inbound, session, prepared)
	result := make(chan error, 1)
	go func() {
		_, roundTripErr := transport.RoundTrip(inbound.Clone(ctx))
		result <- roundTripErr
	}()
	<-started
	if _, err := tracker.RetryByTraceID(session.TraceID(), 1, proxyrequest.RetryTriggerAdminAPI); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, ErrDirectiveNotFound) {
		t.Fatalf("unexpected refresh error: %v", err)
	}
	if prepared.calls != 2 || baseCalls != 1 {
		t.Fatalf("old plan was reused after refresh failure: resolve_calls=%d upstream_calls=%d", prepared.calls, baseCalls)
	}
	if len(tracker.ListActive()) != 0 {
		t.Fatal("failed refresh remained active")
	}
	session.Complete()
}

func TestRetryTransportReplaysBodyAfterManualRetry(t *testing.T) {
	tracker := proxyrequestadapter.NewProxyRequestService(proxyrequestadapter.ProxyRequestOptions{MaxAttempts: 3}, nil)
	inbound, err := http.NewRequest(http.MethodPost, "http://proxy.local/chat", strings.NewReader("request-body"))
	if err != nil {
		t.Fatal(err)
	}
	inbound.Header.Set("Idempotency-Key", "replay-test")
	session := tracker.Start(inbound, proxyrequest.Identity{})
	started := make(chan struct{})
	var mu sync.Mutex
	var bodies []string
	calls := 0
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, readErr := io.ReadAll(req.Body)
		if readErr != nil {
			return nil, readErr
		}
		mu.Lock()
		calls++
		call := calls
		bodies = append(bodies, string(body))
		mu.Unlock()
		if call == 1 {
			close(started)
			<-req.Context().Done()
			return nil, req.Context().Err()
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("response")),
			Request:    req,
		}, nil
	})
	transport, err := NewRetryTransport(base, RetryTransportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	target, _ := url.Parse("https://upstream.example")
	ctx := retryTestContext(t, inbound, session, staticPrepared{resolution: Resolution{Plan: &Plan{Target: target, JoinPath: true}}})
	outbound := inbound.Clone(ctx)
	result := make(chan struct {
		response *http.Response
		err      error
	}, 1)
	go func() {
		response, roundTripErr := transport.RoundTrip(outbound)
		result <- struct {
			response *http.Response
			err      error
		}{response: response, err: roundTripErr}
	}()

	<-started
	active := tracker.ListActive()
	if len(active) != 1 || active[0].Attempt != 1 {
		t.Fatalf("unexpected active request: %#v", active)
	}
	if _, err = tracker.RetryByTraceID(session.TraceID(), 1, proxyrequest.RetryTriggerAdminAPI); err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	completed := <-result
	if completed.err != nil {
		t.Fatalf("round trip failed: %v", completed.err)
	}
	responseBody, _ := io.ReadAll(completed.response.Body)
	_ = completed.response.Body.Close()
	if string(responseBody) != "response" {
		t.Fatalf("unexpected response: %q", responseBody)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 2 || len(bodies) != 2 || bodies[0] != "request-body" || bodies[1] != "request-body" {
		t.Fatalf("request was not replayed: calls=%d bodies=%#v", calls, bodies)
	}
	session.Complete()
}

func TestRetryTransportRefreshesPlanAndRebuildsFromOriginalTemplate(t *testing.T) {
	tracker := proxyrequestadapter.NewProxyRequestService(proxyrequestadapter.ProxyRequestOptions{MaxAttempts: 3}, nil)
	inbound, err := http.NewRequest(http.MethodPost, "http://proxy.local/chat", strings.NewReader("same-body"))
	if err != nil {
		t.Fatal(err)
	}
	inbound.Header.Set("Idempotency-Key", "refresh-test")
	inbound.Host = "proxy.local"
	inbound.Header.Set("Authorization", "Bearer dproxy.remote")
	inbound.Header.Set("X-Original", "kept-on-patch")
	session := tracker.Start(inbound, proxyrequest.Identity{})
	firstTarget, _ := url.Parse("https://one.example/base")
	secondTarget, _ := url.Parse("https://two.example/other")
	secondProxy, _ := url.Parse("socks5://127.0.0.1:1080")
	prepared := &rotatingPrepared{plans: []*Plan{
		{
			Target:   firstTarget,
			Metadata: requestmeta.Metadata{"X-Dproxy-Request-Id": {"request-1"}},
			HeaderOps: []HeaderOp{
				{Action: HeaderRemove, Selector: HeaderSelector{Kind: HeaderSelectorExact, Pattern: "Authorization"}},
				{Action: HeaderSet, Selector: HeaderSelector{Kind: HeaderSelectorExact, Pattern: "Host"}, Values: []string{"one.internal"}},
				{Action: HeaderSet, Selector: HeaderSelector{Kind: HeaderSelectorExact, Pattern: "X-Only-First"}, Values: []string{"one"}},
			},
		},
		{
			Target:     secondTarget,
			Proxy:      secondProxy,
			HeaderMode: HeaderModeReplace,
			Metadata:   requestmeta.Metadata{"X-Dproxy-Request-Id": {"changed"}},
			HeaderOps: []HeaderOp{
				{Action: HeaderRemove, Selector: HeaderSelector{Kind: HeaderSelectorExact, Pattern: "Authorization"}},
				{Action: HeaderSet, Selector: HeaderSelector{Kind: HeaderSelectorExact, Pattern: "Host"}, Values: []string{"two.internal"}},
				{Action: HeaderSet, Selector: HeaderSelector{Kind: HeaderSelectorExact, Pattern: "X-Second"}, Values: []string{"two"}},
			},
		},
	}}
	type seenRequest struct {
		url      string
		host     string
		headers  http.Header
		body     string
		proxyURL string
	}
	var seen []seenRequest
	var mu sync.Mutex
	started := make(chan struct{})
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, readErr := io.ReadAll(req.Body)
		if readErr != nil {
			return nil, readErr
		}
		proxyURL, _ := requestProxyFromContext(req.Context())
		proxyValue := ""
		if proxyURL != nil {
			proxyValue = proxyURL.String()
		}
		mu.Lock()
		seen = append(seen, seenRequest{url: req.URL.String(), host: req.Host, headers: req.Header.Clone(), body: string(body), proxyURL: proxyValue})
		call := len(seen)
		mu.Unlock()
		if call == 1 {
			close(started)
			<-req.Context().Done()
			return nil, req.Context().Err()
		}
		return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: http.NoBody, Request: req}, nil
	})
	transport, err := NewRetryTransport(base, RetryTransportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := retryTestContext(t, inbound, session, prepared)
	result := make(chan error, 1)
	go func() {
		response, roundTripErr := transport.RoundTrip(inbound.Clone(ctx))
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		result <- roundTripErr
	}()
	<-started
	active := tracker.ListActive()
	if len(active) != 1 || active[0].Metadata["X-Dproxy-Request-Id"][0] != "request-1" {
		t.Fatalf("request metadata was not bound from the first plan: %#v", active)
	}
	if _, err := tracker.RetryByTraceID(session.TraceID(), 1, proxyrequest.RetryTriggerAdminAPI); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if prepared.calls != 2 {
		t.Fatalf("directive resolution calls=%d want=2", prepared.calls)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 {
		t.Fatalf("upstream calls=%d want=2", len(seen))
	}
	if seen[0].url != "https://one.example/base" || seen[0].host != "one.internal" || seen[0].headers.Get("X-Only-First") != "one" {
		t.Fatalf("unexpected first attempt: %#v", seen[0])
	}
	if seen[1].url != "https://two.example/other" || seen[1].host != "two.internal" || seen[1].proxyURL != secondProxy.String() {
		t.Fatalf("unexpected second routing: %#v", seen[1])
	}
	if seen[1].headers.Get("X-Only-First") != "" || seen[1].headers.Get("X-Original") != "" || seen[1].headers.Get("Authorization") != "" || seen[1].headers.Get("X-Second") != "two" {
		t.Fatalf("attempt-one headers contaminated attempt two: %#v", seen[1].headers)
	}
	if seen[0].body != "same-body" || seen[1].body != "same-body" {
		t.Fatalf("request body changed across attempts: %#v", seen)
	}
	session.Complete()
}

func retryTestContext(t *testing.T, inbound *http.Request, session proxyrequest.Session, prepared PreparedDirective) context.Context {
	t.Helper()
	var data []byte
	if inbound.Body != nil && inbound.Body != http.NoBody {
		var err error
		data, err = io.ReadAll(inbound.Body)
		if err != nil {
			t.Fatal(err)
		}
		_ = inbound.Body.Close()
		inbound.Body = http.NoBody
	}
	body := bodymemory.NewBody(data, nil)
	t.Cleanup(body.Release)
	ctx := proxyrequest.ContextWithSession(inbound.Context(), session)
	return contextWithPreparedRequest(ctx, prepared, NewRequestTemplate(inbound), body)
}
