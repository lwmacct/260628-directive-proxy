package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/bodystore"
	"github.com/lwmacct/260628-directive-proxy/internal/core/httpheader"
	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
)

type resolverFunc func(*http.Request) (Resolution, error)

type errorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (f resolverFunc) Prepare(req *http.Request) (PreparedDirective, error) {
	resolution, err := f(req)
	if err != nil {
		return nil, err
	}
	return staticPrepared{resolution: resolution}, nil
}

type staticPrepared struct {
	resolution Resolution
	err        error
}

type preparedResolver struct{ prepared PreparedDirective }

func (r preparedResolver) Prepare(*http.Request) (PreparedDirective, error) { return r.prepared, nil }

func (p staticPrepared) Kind() string { return p.resolution.Source.Mode }

func (p staticPrepared) Source() SourceMetadata { return p.resolution.Source }

func (p staticPrepared) RequestProgram() []module.Spec { return nil }

func (p staticPrepared) ResolveAttempt(context.Context, int) (Resolution, error) {
	return Resolution{Plan: ClonePlan(p.resolution.Plan), Source: p.resolution.Source}, p.err
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestHandlerPassesUnmatchedRequestToNextWithoutProxySideEffects(t *testing.T) {
	nextCalled := false
	resolveCalls := 0
	handler := NewHandler(resolverFunc(func(*http.Request) (Resolution, error) {
		resolveCalls++
		return Resolution{}, ErrNoMatch
	}), http.DefaultTransport, HandlerOptions{
		Next: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			nextCalled = true
			w.WriteHeader(http.StatusNoContent)
		}),
	})

	req := httptest.NewRequest(http.MethodGet, "http://proxy.local/", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
	if !nextCalled {
		t.Fatal("next handler was not called")
	}
	if resolveCalls != 1 {
		t.Fatalf("unexpected resolver call count: %d", resolveCalls)
	}
}

func TestHandlerReturnsBadRequestWhenDirectiveIsInvalid(t *testing.T) {
	handler := NewHandler(resolverFunc(func(*http.Request) (Resolution, error) {
		return Resolution{}, ErrInvalidDirective
	}), http.DefaultTransport, HandlerOptions{})

	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/responses", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
	var body errorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body failed: %v", err)
	}
	if body.Error.Code != "invalid_directive" || body.Error.Message != "directive: invalid proxy directive payload" {
		t.Fatalf("unexpected response body: %#v", body)
	}
}

func TestHandlerMapsDirectiveResolutionErrors(t *testing.T) {
	tests := []struct {
		err    error
		status int
		code   string
		body   string
	}{
		{ErrDirectiveNotFound, http.StatusNotFound, "directive_not_found", "directive: reference not found"},
		{ErrDirectiveUnauthorized, http.StatusUnauthorized, "directive_unauthorized", "directive: token authentication failed"},
		{ErrRemoteDirectiveUnavailable, http.StatusServiceUnavailable, "remote_unavailable", "directive: remote resolver unavailable"},
		{ErrDirectiveTokenTooLarge, http.StatusRequestHeaderFieldsTooLarge, "directive_token_too_large", "directive: token is too large"},
		{ErrRemoteDirectiveInvalid, http.StatusBadGateway, "remote_response_invalid", "directive: remote payload is invalid"},
	}
	for _, tt := range tests {
		handler := NewHandler(resolverFunc(func(*http.Request) (Resolution, error) {
			return Resolution{}, tt.err
		}), http.DefaultTransport, HandlerOptions{})
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://proxy.local/", nil))
		var body errorResponse
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode error response: %v", err)
		}
		if recorder.Code != tt.status || body.Error.Code != tt.code || body.Error.Message != tt.body {
			t.Fatalf("unexpected response for %v: status=%d body=%q", tt.err, recorder.Code, recorder.Body.String())
		}
		if errors.Is(tt.err, ErrDirectiveUnauthorized) && recorder.Header().Get("WWW-Authenticate") != "Bearer" {
			t.Fatalf("missing bearer challenge for unauthorized directive: %#v", recorder.Header())
		}
	}
}

func TestHandlerMapsPerAttemptDirectiveResolutionError(t *testing.T) {
	handler := NewHandler(preparedResolver{prepared: staticPrepared{err: ErrDirectiveNotFound, resolution: Resolution{Source: SourceMetadata{Mode: "remote"}}}}, http.DefaultTransport, HandlerOptions{})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://proxy.local/", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("unexpected status: %d body=%s", recorder.Code, recorder.Body.String())
	}
	var body errorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "directive_not_found" {
		t.Fatalf("unexpected error response: %#v", body)
	}
}

func TestHandlerReturnsInternalErrorWhenDirectiveTargetMissing(t *testing.T) {
	handler := NewHandler(resolverFunc(func(*http.Request) (Resolution, error) {
		return Resolution{Plan: &Plan{}}, nil
	}), http.DefaultTransport, HandlerOptions{})

	req := httptest.NewRequest(http.MethodGet, "http://proxy.local/", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}

	var body errorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body failed: %v", err)
	}
	if got := body.Error.Message; got != "resolver: resolve proxy plan failed" {
		t.Fatalf("unexpected response body: %#v", body)
	}
}

func TestHandlerDoesNotExposeResolverErrorText(t *testing.T) {
	const rawAuthorization = "Bearer encoded-auth-secret"
	const decodedSecret = "decoded-auth-secret"

	handler := NewHandler(resolverFunc(func(*http.Request) (Resolution, error) {
		return Resolution{}, errors.New("resolve failed with " + rawAuthorization + " and " + decodedSecret)
	}), http.DefaultTransport, HandlerOptions{})

	req := httptest.NewRequest(http.MethodGet, "http://proxy.local/", nil)
	req.Header.Set("Authorization", rawAuthorization)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("unexpected content type: %s", got)
	}
	body := recorder.Body.String()
	if strings.Contains(body, rawAuthorization) || strings.Contains(body, decodedSecret) {
		t.Fatalf("response leaked authorization content: %q", body)
	}
	var parsed errorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("unmarshal body failed: %v", err)
	}
	if parsed.Error.Code != "resolver_failed" || parsed.Error.Message != "resolver: resolve proxy plan failed" {
		t.Fatalf("unexpected response body: %#v", parsed)
	}
}

func TestHandlerDoesNotExposeProxyTransportErrorText(t *testing.T) {
	const rawAuthorization = "Bearer encoded-auth-secret"
	const targetFromAuthorization = "https://target-from-authorization.example.com/private"

	target, err := url.Parse(targetFromAuthorization)
	if err != nil {
		t.Fatalf("parse target failed: %v", err)
	}
	handler := NewHandler(
		resolverFunc(func(*http.Request) (Resolution, error) {
			return Resolution{Plan: &Plan{
				Target:   target,
				JoinPath: true,
			}}, nil
		}),
		roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return nil, errors.New("dial failed for " + req.URL.String() + " with " + rawAuthorization)
		}),
		HandlerOptions{},
	)

	req := httptest.NewRequest(http.MethodGet, "http://proxy.local/v1/resources", nil)
	req.Header.Set("Authorization", rawAuthorization)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
	body := recorder.Body.String()
	if strings.Contains(body, rawAuthorization) || strings.Contains(body, targetFromAuthorization) {
		t.Fatalf("response leaked proxy error content: %q", body)
	}
	if got := recorder.Header().Get("Location"); got != "" {
		t.Fatalf("unexpected location header: %q", got)
	}
	var parsed errorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("unmarshal body failed: %v", err)
	}
	if parsed.Error.Code != "upstream_request_failed" || parsed.Error.Message != "upstream: request failed" {
		t.Fatalf("unexpected response body: %#v", parsed)
	}
}

func TestHandlerPassesThroughUpstreamErrorResponse(t *testing.T) {
	const upstreamBody = `{"error":"upstream rejected request","target":"https://api.example.com/private","authorization":"Bearer upstream-secret"}`

	target, err := url.Parse("https://api.example.com/private")
	if err != nil {
		t.Fatalf("parse target failed: %v", err)
	}
	handler := NewHandler(
		resolverFunc(func(*http.Request) (Resolution, error) {
			return Resolution{Plan: &Plan{
				Target:   target,
				JoinPath: true,
			}}, nil
		}),
		roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Status:     "401 Unauthorized",
				Header: http.Header{
					"Content-Type":       {"application/json"},
					"X-Upstream-Target":  {req.URL.String()},
					"X-Upstream-Auth":    {"Bearer upstream-secret"},
					"X-Upstream-Routing": {"retry-other-pool"},
				},
				Body:          io.NopCloser(strings.NewReader(upstreamBody)),
				ContentLength: int64(len(upstreamBody)),
				Request:       req,
			}, nil
		}),
		HandlerOptions{},
	)

	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/responses", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected upstream status to pass through, got %d", recorder.Code)
	}
	if got := strings.TrimSpace(recorder.Body.String()); got != upstreamBody {
		t.Fatalf("expected upstream body to pass through, got %q", got)
	}
	if got := recorder.Header().Get("X-Upstream-Target"); got != "https://api.example.com/private/v1/responses" {
		t.Fatalf("expected upstream target header to pass through, got %q", got)
	}
	if got := recorder.Header().Get("X-Upstream-Auth"); got != "Bearer upstream-secret" {
		t.Fatalf("expected upstream auth header to pass through, got %q", got)
	}
	if got := recorder.Header().Get("X-Upstream-Routing"); got != "retry-other-pool" {
		t.Fatalf("expected upstream routing header to pass through, got %q", got)
	}
}

func TestHandlerPatchHeaderPolicySurvivesReverseProxyPreprocessing(t *testing.T) {
	target, err := url.Parse("https://api.example.com")
	if err != nil {
		t.Fatalf("parse target failed: %v", err)
	}
	for _, tt := range []struct {
		name          string
		preserve      bool
		wantForwarded string
	}{
		{name: "removes by default"},
		{name: "preserves when requested", preserve: true, wantForwarded: "for=client.example"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewHandler(
				resolverFunc(func(*http.Request) (Resolution, error) {
					return Resolution{Plan: &Plan{Target: target, JoinPath: true, Headers: httpheader.Plan{
						Request: httpheader.RequestPlan{PreserveProxyDisclosure: tt.preserve},
					}}}, nil
				}),
				roundTripFunc(func(req *http.Request) (*http.Response, error) {
					if got := req.Header.Get("Forwarded"); got != tt.wantForwarded {
						t.Fatalf("unexpected outbound Forwarded header: %q", got)
					}
					return &http.Response{
						StatusCode: http.StatusNoContent,
						Header:     make(http.Header),
						Body:       http.NoBody,
						Request:    req,
					}, nil
				}),
				HandlerOptions{},
			)
			req := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/resources", nil)
			req.Header.Set("Forwarded", "for=client.example")
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusNoContent {
				t.Fatalf("unexpected response status: %d", recorder.Code)
			}
		})
	}
}

func TestHandleProxyErrorSkipsResponseWhenRequestIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, "http://proxy.local/v1/responses", nil).WithContext(ctx)
	recorder := httptest.NewRecorder()

	handleProxyError(recorder, req, context.Canceled)

	if recorder.Body.Len() != 0 {
		t.Fatalf("unexpected response body: %q", recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != "" {
		t.Fatalf("unexpected content type: %q", got)
	}
}

type countedBody struct {
	reads atomic.Int32
	data  *strings.Reader
}

func (b *countedBody) Read(data []byte) (int, error) {
	b.reads.Add(1)
	return b.data.Read(data)
}

func (*countedBody) Close() error { return nil }

type stagedBody struct {
	first     *strings.Reader
	second    *strings.Reader
	firstRead chan struct{}
	release   chan struct{}
	stage     atomic.Int32
}

func (b *stagedBody) Read(data []byte) (int, error) {
	if b.stage.Load() == 0 {
		n, err := b.first.Read(data)
		if n > 0 {
			return n, nil
		}
		if !errors.Is(err, io.EOF) {
			return n, err
		}
		b.stage.Store(1)
		close(b.firstRead)
	}
	if b.stage.CompareAndSwap(1, 2) {
		<-b.release
	}
	return b.second.Read(data)
}

func (*stagedBody) Close() error { return nil }

func TestHandlerStreamsRequestBodyBeforeClientEOF(t *testing.T) {
	target, _ := url.Parse("https://upstream.example")
	upstreamPrefix := make(chan string, 1)
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		prefix := make([]byte, 4)
		if _, err := io.ReadFull(req.Body, prefix); err != nil {
			return nil, err
		}
		upstreamPrefix <- string(prefix)
		if _, err := io.ReadAll(req.Body); err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: http.NoBody, Request: req}, nil
	})
	store := bodystore.New(bodystore.Config{
		MemoryMaxBytes: 16, MemoryPerBodyBytes: 16, DiskMaxBytes: 64,
		MaxBodyBytes: 16, ChunkBytes: 4, TempDir: t.TempDir(),
	})
	handler := NewHandler(resolverFunc(func(*http.Request) (Resolution, error) {
		return Resolution{Plan: &Plan{Target: target}}, nil
	}), transport, HandlerOptions{BodyStore: store, BodyReadTimeout: time.Second})

	body := &stagedBody{
		first: strings.NewReader("live"), second: strings.NewReader("-tail"),
		firstRead: make(chan struct{}), release: make(chan struct{}),
	}
	request := httptest.NewRequest(http.MethodPost, "http://proxy.local/stream", nil)
	request.Body = body
	request.ContentLength = 9
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(httptest.NewRecorder(), request)
		close(done)
	}()
	<-body.firstRead
	select {
	case prefix := <-upstreamPrefix:
		if prefix != "live" {
			t.Fatalf("unexpected live prefix: %q", prefix)
		}
	case <-time.After(time.Second):
		t.Fatal("upstream did not receive body prefix before client EOF")
	}
	close(body.release)
	<-done
}

func TestHandlerAcceptsUnknownContentLength(t *testing.T) {
	target, _ := url.Parse("https://upstream.example")
	var upstreamCalls atomic.Int32
	store := bodystore.New(bodystore.Config{
		MemoryMaxBytes: 16, MemoryPerBodyBytes: 16, DiskMaxBytes: 64,
		MaxBodyBytes: 16, ChunkBytes: 4, TempDir: t.TempDir(),
	})
	handler := NewHandler(resolverFunc(func(*http.Request) (Resolution, error) {
		return Resolution{Plan: &Plan{Target: target}}, nil
	}), roundTripFunc(func(request *http.Request) (*http.Response, error) {
		upstreamCalls.Add(1)
		if _, err := io.ReadAll(request.Body); err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: http.NoBody, Request: request}, nil
	}), HandlerOptions{BodyStore: store, BodyReadTimeout: time.Second})

	body := &countedBody{data: strings.NewReader("payload")}
	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/resource", nil)
	req.Body = body
	req.ContentLength = -1
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent || body.reads.Load() == 0 || upstreamCalls.Load() != 1 {
		t.Fatalf("unexpected streaming result: status=%d reads=%d upstream_calls=%d body=%s", recorder.Code, body.reads.Load(), upstreamCalls.Load(), recorder.Body.String())
	}
}

func TestHandleProxyErrorMapsBodyStoreAdmissionErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		status     int
		code       string
		retryAfter string
	}{
		{name: "too large", err: bodystore.ErrBodyTooLarge, status: http.StatusRequestEntityTooLarge, code: "request_body_too_large"},
		{name: "capacity", err: bodystore.ErrStoreCapacity, status: http.StatusServiceUnavailable, code: "body_store_capacity", retryAfter: "1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handleProxyError(recorder, httptest.NewRequest(http.MethodPost, "http://proxy.local/resource", nil), tt.err)
			var response errorResponse
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if recorder.Code != tt.status || response.Error.Code != tt.code || recorder.Header().Get("Retry-After") != tt.retryAfter {
				t.Fatalf("unexpected error mapping: status=%d retry_after=%q body=%s", recorder.Code, recorder.Header().Get("Retry-After"), recorder.Body.String())
			}
		})
	}
}
