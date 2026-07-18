package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lwmacct/260718-go-pkg-clientip/pkg/clientip"
	"github.com/lwmacct/260718-go-pkg-ipallow/pkg/ipallow"

	"github.com/lwmacct/260628-directive-proxy/internal/config"
)

func TestDirectiveSourceAccessUsesConfiguredClientIPHeaders(t *testing.T) {
	cfg := config.DefaultConfig().Server.Proxy.Directive.SourceAccess
	cfg.Rules = []ipallow.Rule{{Value: "198.51.100.7"}}
	cfg.TrustedProxies = []string{"192.0.2.0/24"}
	cfg.Headers = []clientip.Header{clientip.HeaderXRealIP}
	access, err := newDirectiveSourceAccess(context.Background(), cfg)
	if err != nil {
		t.Fatalf("configure source access: %v", err)
	}
	t.Cleanup(access.Close)

	handler := access.RequireAccess(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodPost, "http://proxy.local/", nil)
	request.RemoteAddr = "192.0.2.1:1234"
	request.Header.Set("Forwarded", `for="unterminated`)
	request.Header.Set("X-Real-IP", "198.51.100.7")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("configured X-Real-IP was not used: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestDirectiveSourceAccessMapsClosedMatcher(t *testing.T) {
	cfg := config.DefaultConfig().Server.Proxy.Directive.SourceAccess
	access, err := newDirectiveSourceAccess(context.Background(), cfg)
	if err != nil {
		t.Fatalf("configure source access: %v", err)
	}
	access.Close()
	handler := access.RequireAccess(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("closed matcher allowed request")
	}))
	request := httptest.NewRequest(http.MethodPost, "http://proxy.local/", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), `"code":"engine_closed"`) {
		t.Fatalf("unexpected closed matcher response: status=%d body=%s", response.Code, response.Body.String())
	}
}
