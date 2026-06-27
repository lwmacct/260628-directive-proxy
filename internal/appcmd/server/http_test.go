package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/config"
)

func TestNewHTTPHandlerMountsProxyAtRoot(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Proxy.PathPrefix = "/"

	rt := &runtime{
		proxy: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusTeapot)
		}),
	}

	handler := newHTTPHandler(&cfg, rt)
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/responses", nil)

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusTeapot {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
}
