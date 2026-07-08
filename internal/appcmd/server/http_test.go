package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/config"
)

func TestControlHTTPServerOnlyServesControlPlane(t *testing.T) {
	cfg := config.DefaultConfig()
	rt := &runtime{}
	srv, err := newControlHTTPServer(&cfg, rt)
	if err != nil {
		t.Fatalf("create control server failed: %v", err)
	}

	if srv.Addr != ":23198" {
		t.Fatalf("unexpected control listen: %q", srv.Addr)
	}

	healthReq := httptest.NewRequest(http.MethodGet, "http://control.local/api/health", nil)
	healthRecorder := httptest.NewRecorder()
	srv.Handler.ServeHTTP(healthRecorder, healthReq)
	if healthRecorder.Code != http.StatusOK {
		t.Fatalf("unexpected health status: %d", healthRecorder.Code)
	}

	proxyReq := httptest.NewRequest(http.MethodPost, "http://control.local/v1/chat/completions", nil)
	proxyRecorder := httptest.NewRecorder()
	srv.Handler.ServeHTTP(proxyRecorder, proxyReq)
	if proxyRecorder.Code != http.StatusNotFound {
		t.Fatalf("expected control server not to serve proxy paths, got %d", proxyRecorder.Code)
	}
}

func TestProxyHTTPServerServesRootDataPlane(t *testing.T) {
	cfg := config.DefaultConfig()
	rt := &runtime{
		proxy: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusAccepted)
		}),
	}
	srv, err := newProxyHTTPServer(&cfg, rt)
	if err != nil {
		t.Fatalf("create proxy server failed: %v", err)
	}

	if srv.Addr != ":23197" {
		t.Fatalf("unexpected proxy listen: %q", srv.Addr)
	}

	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/chat/completions", nil)
	recorder := httptest.NewRecorder()
	srv.Handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("unexpected proxy status: %d", recorder.Code)
	}
}
