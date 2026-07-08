package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/config"
	"github.com/lwmacct/260628-llm-relay-dproxy/internal/core/proxy"
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

func TestControlHTTPServerListsProxyExchangesWhenCaptureDisabled(t *testing.T) {
	cfg := config.DefaultConfig()
	rt := &runtime{recorder: proxy.NewExchangeRecorder(proxy.DefaultExchangeCapacity, proxy.DefaultExchangeMaxBodyBytes)}
	srv, err := newControlHTTPServer(&cfg, rt)
	if err != nil {
		t.Fatalf("create control server failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "http://control.local/api/proxy-exchanges", nil)
	recorder := httptest.NewRecorder()
	srv.Handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
	var body struct {
		Enabled bool  `json:"enabled"`
		Items   []any `json:"items"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body failed: %v", err)
	}
	if body.Enabled || len(body.Items) != 0 {
		t.Fatalf("unexpected response body: %#v", body)
	}
}

func TestControlHTTPServerUpdatesAndClearsProxyExchangeSettings(t *testing.T) {
	cfg := config.DefaultConfig()
	rt := &runtime{recorder: proxy.NewExchangeRecorder(proxy.DefaultExchangeCapacity, proxy.DefaultExchangeMaxBodyBytes)}
	srv, err := newControlHTTPServer(&cfg, rt)
	if err != nil {
		t.Fatalf("create control server failed: %v", err)
	}

	updateReq := httptest.NewRequest(
		http.MethodPut,
		"http://control.local/api/proxy-exchanges/settings",
		strings.NewReader(`{"enabled":true,"capacity":3,"max_body_bytes":128}`),
	)
	updateReq.Header.Set("Content-Type", "application/json")
	updateRecorder := httptest.NewRecorder()
	srv.Handler.ServeHTTP(updateRecorder, updateReq)

	if updateRecorder.Code != http.StatusOK {
		t.Fatalf("unexpected settings status: %d body=%s", updateRecorder.Code, updateRecorder.Body.String())
	}
	var updateBody struct {
		Enabled      bool  `json:"enabled"`
		Capacity     int   `json:"capacity"`
		MaxBodyBytes int64 `json:"max_body_bytes"`
	}
	if err := json.Unmarshal(updateRecorder.Body.Bytes(), &updateBody); err != nil {
		t.Fatalf("unmarshal settings body failed: %v", err)
	}
	if !updateBody.Enabled || updateBody.Capacity != 3 || updateBody.MaxBodyBytes != 128 {
		t.Fatalf("unexpected settings body: %#v", updateBody)
	}

	rt.recorder.Configure(true, 0, -1)
	rt.recorder.Clear()
	clearReq := httptest.NewRequest(http.MethodDelete, "http://control.local/api/proxy-exchanges", nil)
	clearRecorder := httptest.NewRecorder()
	srv.Handler.ServeHTTP(clearRecorder, clearReq)
	if clearRecorder.Code != http.StatusOK {
		t.Fatalf("unexpected clear status: %d body=%s", clearRecorder.Code, clearRecorder.Body.String())
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
