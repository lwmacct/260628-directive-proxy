package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/config"
	"github.com/lwmacct/260628-llm-relay-dproxy/internal/core/proxy"
)

func TestHTTPServerRoutesControlAndProxyRequestsOnOneListener(t *testing.T) {
	cfg := config.DefaultConfig()
	var proxyPath string
	rt := &runtime{
		proxy: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			proxyPath = r.URL.Path
			w.WriteHeader(http.StatusAccepted)
		}),
	}
	srv := newHTTPServer(&cfg, rt)

	if srv.Addr != ":23198" {
		t.Fatalf("unexpected http listen: %q", srv.Addr)
	}

	healthReq := httptest.NewRequest(http.MethodGet, "http://control.local/api/health", nil)
	healthRecorder := httptest.NewRecorder()
	srv.Handler.ServeHTTP(healthRecorder, healthReq)
	if healthRecorder.Code != http.StatusOK {
		t.Fatalf("unexpected health status: %d", healthRecorder.Code)
	}

	proxyReq := httptest.NewRequest(http.MethodPost, "http://service.local/api/chat/completions", nil)
	proxyReq.Header.Set("Authorization", "Bearer dproxy.10.payload")
	proxyRecorder := httptest.NewRecorder()
	srv.Handler.ServeHTTP(proxyRecorder, proxyReq)
	if proxyRecorder.Code != http.StatusAccepted {
		t.Fatalf("unexpected proxy status: %d", proxyRecorder.Code)
	}
	if proxyPath != "/api/chat/completions" {
		t.Fatalf("proxy path was modified: %q", proxyPath)
	}

	ordinaryBearerReq := httptest.NewRequest(http.MethodGet, "http://service.local/api/health", nil)
	ordinaryBearerReq.Header.Set("Authorization", "Bearer upstream-token")
	ordinaryBearerRecorder := httptest.NewRecorder()
	srv.Handler.ServeHTTP(ordinaryBearerRecorder, ordinaryBearerReq)
	if ordinaryBearerRecorder.Code != http.StatusOK {
		t.Fatalf("ordinary bearer request must use control handler, got %d", ordinaryBearerRecorder.Code)
	}
}

func TestControlHTTPServerListsProxyExchangesWhenCaptureDisabled(t *testing.T) {
	cfg := config.DefaultConfig()
	rt := &runtime{recorder: proxy.NewExchangeRecorder(proxy.DefaultExchangeCapacity, proxy.DefaultExchangeMaxBodyBytes)}
	srv := newHTTPServer(&cfg, rt)

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
	srv := newHTTPServer(&cfg, rt)

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

func TestHTTPServerReturnsProxyErrorForUnsupportedDProxyToken(t *testing.T) {
	cfg := config.DefaultConfig()
	rt := &runtime{proxy: newProxyHandler(&cfg, nil)}
	srv := newHTTPServer(&cfg, rt)

	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer dproxy.11.payload")
	recorder := httptest.NewRecorder()
	srv.Handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("unexpected proxy status: %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "invalid proxy directive payload") {
		t.Fatalf("unexpected proxy error: %s", recorder.Body.String())
	}
}

func TestHTTPServerDoesNotApplyControlBodyLimitToProxyRequests(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.HTTP.MaxAPIBodyBytes = 1
	var proxyBody string
	rt := &runtime{
		proxy: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
				return
			}
			proxyBody = string(body)
			w.WriteHeader(http.StatusAccepted)
		}),
	}
	srv := newHTTPServer(&cfg, rt)

	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/api/upload", strings.NewReader("payload"))
	req.Header.Set("Authorization", "Bearer dproxy.10.payload")
	recorder := httptest.NewRecorder()
	srv.Handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("unexpected proxy status: %d body=%s", recorder.Code, recorder.Body.String())
	}
	if proxyBody != "payload" {
		t.Fatalf("unexpected proxy body: %q", proxyBody)
	}
}
