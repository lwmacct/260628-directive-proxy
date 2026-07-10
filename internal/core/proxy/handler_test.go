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
	"testing"
)

type resolverFunc func(*http.Request) (*Plan, error)

func (f resolverFunc) Resolve(req *http.Request) (*Plan, error) {
	return f(req)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestHandlerPassesUnmatchedRequestToNextWithoutProxySideEffects(t *testing.T) {
	nextCalled := false
	resolveCalls := 0
	handler := NewHandler(resolverFunc(func(*http.Request) (*Plan, error) {
		resolveCalls++
		return nil, ErrNoMatch
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
	handler := NewHandler(resolverFunc(func(*http.Request) (*Plan, error) {
		return nil, ErrInvalidDirective
	}), http.DefaultTransport, HandlerOptions{})

	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/responses", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body failed: %v", err)
	}
	if len(body) != 1 || body["error"] != "directive: invalid proxy directive payload" {
		t.Fatalf("unexpected response body: %#v", body)
	}
}

func TestHandlerReturnsInternalErrorWhenDirectiveTargetMissing(t *testing.T) {
	handler := NewHandler(resolverFunc(func(*http.Request) (*Plan, error) {
		return &Plan{}, nil
	}), http.DefaultTransport, HandlerOptions{})

	req := httptest.NewRequest(http.MethodGet, "http://proxy.local/", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}

	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body failed: %v", err)
	}
	if got := body["error"]; got != "resolver: resolve proxy plan failed" {
		t.Fatalf("unexpected response body: %#v", body)
	}
}

func TestHandlerDoesNotExposeResolverErrorText(t *testing.T) {
	const rawAuthorization = "Bearer encoded-auth-secret"
	const decodedSecret = "decoded-auth-secret"

	handler := NewHandler(resolverFunc(func(*http.Request) (*Plan, error) {
		return nil, errors.New("resolve failed with " + rawAuthorization + " and " + decodedSecret)
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
	var parsed map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("unmarshal body failed: %v", err)
	}
	if len(parsed) != 1 || parsed["error"] != "resolver: resolve proxy plan failed" {
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
		resolverFunc(func(*http.Request) (*Plan, error) {
			return &Plan{
				Target:   target,
				JoinPath: true,
			}, nil
		}),
		roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return nil, errors.New("dial failed for " + req.URL.String() + " with " + rawAuthorization)
		}),
		HandlerOptions{},
	)

	req := httptest.NewRequest(http.MethodGet, "http://proxy.local/v1/chat", nil)
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
	var parsed map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("unmarshal body failed: %v", err)
	}
	if len(parsed) != 1 || parsed["error"] != "upstream: request failed" {
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
		resolverFunc(func(*http.Request) (*Plan, error) {
			return &Plan{
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
