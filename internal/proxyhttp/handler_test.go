package proxyhttp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/proxyplan"
)

type resolverFunc func(*http.Request) (*proxyplan.Plan, error)

func (f resolverFunc) Resolve(req *http.Request) (*proxyplan.Plan, error) {
	return f(req)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type fixedIDGenerator struct {
	id string
}

func (g fixedIDGenerator) Generate() string {
	return g.id
}

func TestHandlerReturnsBadRequestWhenDirectiveIsMissing(t *testing.T) {
	handler := NewHandler(resolverFunc(func(*http.Request) (*proxyplan.Plan, error) {
		return nil, proxyplan.ErrInvalidPlan
	}), http.DefaultTransport, HandlerOptions{})

	req := httptest.NewRequest(http.MethodGet, "http://proxy.local/", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("unexpected content type: %s", got)
	}

	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body failed: %v", err)
	}
	if got := body["error"]; got != "directive: missing directive token" {
		t.Fatalf("unexpected response body: %#v", body)
	}
}

func TestHandlerReturnsBadRequestWhenDirectiveIsInvalid(t *testing.T) {
	handler := NewHandler(resolverFunc(func(*http.Request) (*proxyplan.Plan, error) {
		return nil, proxyplan.ErrInvalidDirective
	}), http.DefaultTransport, HandlerOptions{IDGenerator: fixedIDGenerator{id: "server-req-1"}})

	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/responses", nil)
	req.Header.Set("X-Client-Request-Id", "client-req-1")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
	if got := recorder.Header().Get("X-Client-Request-Id"); got != "server-req-1" {
		t.Fatalf("unexpected client request id header: %q", got)
	}

	var body struct {
		Error     string `json:"error"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body failed: %v", err)
	}
	if body.Error != "directive: invalid proxy directive payload" ||
		body.RequestID != "server-req-1" {
		t.Fatalf("unexpected response body: %#v", body)
	}
}

func TestHandlerReturnsHelloWorldJSONWhenDirectiveTargetMissing(t *testing.T) {
	handler := NewHandler(resolverFunc(func(*http.Request) (*proxyplan.Plan, error) {
		return &proxyplan.Plan{}, nil
	}), http.DefaultTransport, HandlerOptions{})

	req := httptest.NewRequest(http.MethodGet, "http://proxy.local/", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}

	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body failed: %v", err)
	}
	if got := body["message"]; got != "hello world" {
		t.Fatalf("unexpected response body: %#v", body)
	}
}

func TestHandlerDoesNotExposeResolverErrorText(t *testing.T) {
	const rawAuthorization = "Bearer encoded-auth-secret"
	const decodedSecret = "decoded-auth-secret"

	handler := NewHandler(resolverFunc(func(*http.Request) (*proxyplan.Plan, error) {
		return nil, errors.New("resolve failed with " + rawAuthorization + " and " + decodedSecret)
	}), http.DefaultTransport, HandlerOptions{IDGenerator: fixedIDGenerator{id: "server-req-2"}})

	req := httptest.NewRequest(http.MethodGet, "http://proxy.local/", nil)
	req.Header.Set("Authorization", rawAuthorization)
	req.Header.Set("X-Client-Request-Id", "client-req-2")
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
	var parsed struct {
		Error     string `json:"error"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("unmarshal body failed: %v", err)
	}
	if parsed.Error != "resolver: resolve proxy plan failed" ||
		parsed.RequestID != "server-req-2" {
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
		resolverFunc(func(*http.Request) (*proxyplan.Plan, error) {
			return &proxyplan.Plan{
				Target:   target,
				JoinPath: true,
			}, nil
		}),
		roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return nil, errors.New("dial failed for " + req.URL.String() + " with " + rawAuthorization)
		}),
		HandlerOptions{IDGenerator: fixedIDGenerator{id: "server-req-3"}},
	)

	req := httptest.NewRequest(http.MethodGet, "http://proxy.local/v1/chat", nil)
	req.Header.Set("Authorization", rawAuthorization)
	req.Header.Set("X-Client-Request-Id", "client-req-3")
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
	var parsed struct {
		Error     string `json:"error"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("unmarshal body failed: %v", err)
	}
	if parsed.Error != "upstream: request failed" ||
		parsed.RequestID != "server-req-3" {
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
		resolverFunc(func(*http.Request) (*proxyplan.Plan, error) {
			return &proxyplan.Plan{
				Target:   target,
				JoinPath: true,
			}, nil
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
		HandlerOptions{IDGenerator: fixedIDGenerator{id: "server-req-4"}},
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
