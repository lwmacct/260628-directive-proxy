package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	miniredisServer "github.com/alicebob/miniredis/v2/server"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/adapter/exchange/capture"
	"github.com/lwmacct/260628-llm-relay-dproxy/internal/config"
	"github.com/lwmacct/260628-llm-relay-dproxy/internal/core/directive"
	"github.com/lwmacct/260628-llm-relay-dproxy/internal/core/exchange"
	"github.com/lwmacct/260628-llm-relay-dproxy/internal/service"
	"github.com/lwmacct/260713-go-pkg-sourceaccess/pkg/sourceaccess"
)

func enableRedisJSON(t *testing.T, redisServer *miniredis.Miniredis) {
	t.Helper()
	if err := redisServer.Server().Register("JSON.GET", func(peer *miniredisServer.Peer, _ string, args []string) {
		if len(args) != 1 {
			peer.WriteError("ERR wrong number of arguments for 'json.get' command")
			return
		}
		value, err := redisServer.Get(args[0])
		if err != nil {
			peer.WriteNull()
			return
		}
		peer.WriteBulk(value)
	}); err != nil {
		t.Fatalf("register JSON.GET: %v", err)
	}
}

func TestHTTPServerRoutesControlAndProxyRequestsOnOneListener(t *testing.T) {
	cfg := config.DefaultConfig()
	var proxyPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyPath = r.URL.Path
		w.WriteHeader(http.StatusAccepted)
	}))
	defer upstream.Close()
	token, err := directive.Encode(directive.Payload{
		Target: directive.TargetSection{URL: upstream.URL},
	})
	if err != nil {
		t.Fatalf("encode directive failed: %v", err)
	}
	rt := newTestRuntimeWithSourceAccess(t, cfg, runtime{})
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
	rootHealthReq := httptest.NewRequest(http.MethodGet, "http://control.local/health", nil)
	rootHealthRecorder := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rootHealthRecorder, rootHealthReq)
	if rootHealthRecorder.Code != http.StatusOK {
		t.Fatalf("unexpected root health status: %d", rootHealthRecorder.Code)
	}

	proxyReq := httptest.NewRequest(http.MethodPost, "http://service.local/api/chat/completions", nil)
	proxyReq.RemoteAddr = "127.0.0.1:1234"
	proxyReq.Header.Set("Authorization", "Bearer "+token)
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

func TestHTTPServerListsProxyExchangesWhenCaptureDisabled(t *testing.T) {
	cfg := config.DefaultConfig()
	rt := &runtime{exchanges: service.NewExchangeService(exchange.DefaultCapacity, exchange.DefaultMaxBodyBytes)}
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

func TestHTTPServerUpdatesAndClearsProxyExchangeSettings(t *testing.T) {
	cfg := config.DefaultConfig()
	rt := &runtime{exchanges: service.NewExchangeService(exchange.DefaultCapacity, exchange.DefaultMaxBodyBytes)}
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

	rt.exchanges.Configure(true, 0, -1)
	rt.exchanges.Clear()
	clearReq := httptest.NewRequest(http.MethodDelete, "http://control.local/api/proxy-exchanges", nil)
	clearRecorder := httptest.NewRecorder()
	srv.Handler.ServeHTTP(clearRecorder, clearReq)
	if clearRecorder.Code != http.StatusOK {
		t.Fatalf("unexpected clear status: %d body=%s", clearRecorder.Code, clearRecorder.Body.String())
	}
}

func TestHTTPServerCapturesProxiedExchangeEndToEnd(t *testing.T) {
	cfg := config.DefaultConfig()
	exchanges := service.NewExchangeService(exchange.DefaultCapacity, exchange.DefaultMaxBodyBytes)
	exchanges.Configure(true, 0, -1)
	rt := newTestRuntimeWithSourceAccess(t, cfg, runtime{exchanges: exchanges, observer: capture.NewObserver(exchanges)})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read upstream body: %v", err)
		}
		if string(body) != "hello" {
			t.Errorf("unexpected upstream body: %q", body)
		}
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("world"))
	}))
	defer upstream.Close()
	token, err := directive.Encode(directive.Payload{Target: directive.TargetSection{URL: upstream.URL}})
	if err != nil {
		t.Fatalf("encode directive failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/chat", strings.NewReader("hello"))
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Forwarded", "for=client.example")
	response := httptest.NewRecorder()
	newHTTPServer(&cfg, rt).Handler.ServeHTTP(response, req)

	if response.Code != http.StatusCreated || response.Body.String() != "world" {
		t.Fatalf("unexpected proxy response: status=%d body=%q", response.Code, response.Body.String())
	}
	snapshot := exchanges.Snapshot(0)
	if len(snapshot.Items) != 1 {
		t.Fatalf("expected one captured exchange, got %#v", snapshot)
	}
	record := snapshot.Items[0]
	if record.RequestBody.Text != "hello" || record.ResponseBody.Text != "world" || record.StatusCode != http.StatusCreated {
		t.Fatalf("unexpected captured exchange: %#v", record)
	}
	if record.RequestHeaders["Authorization"][0] != "<redacted>" {
		t.Fatalf("authorization was not redacted: %#v", record.RequestHeaders)
	}
	if _, exists := record.OutboundRequestHeaders["Authorization"]; exists {
		t.Fatalf("directive authorization was forwarded: %#v", record.OutboundRequestHeaders)
	}
	if record.OutboundRequestHeaders["Forwarded"][0] != "for=client.example" {
		t.Fatalf("patch did not preserve forwarding header: %#v", record.OutboundRequestHeaders)
	}
	if record.DirectiveMode != "inline" || record.DirectiveBackend != "" || record.DirectiveKey != "" {
		t.Fatalf("unexpected inline directive metadata: %#v", record)
	}
}

func TestHTTPServerResolvesRedisDirectiveEndToEnd(t *testing.T) {
	cfg := config.DefaultConfig()
	redisServer := miniredis.RunT(t)
	enableRedisJSON(t, redisServer)
	reader := newTestDirectiveReader(t, cfg)

	var upstreamSource string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamSource = r.Header.Get("X-Directive-Source")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	redisServer.Set("team-a/openai", `{"target":{"url":"`+upstream.URL+`"},"headers":{"ops":[{"op":"=","name":"X-Directive-Source","values":["redis"]}]}}`)
	token, err := directive.EncodeRemote(directive.RemoteSpec{
		Type: directive.RemoteTypeRedis,
		URL:  "redis://" + redisServer.Addr() + "/0",
		Key:  "team-a/openai",
	})
	if err != nil {
		t.Fatalf("encode redis token failed: %v", err)
	}
	exchanges := service.NewExchangeService(exchange.DefaultCapacity, exchange.DefaultMaxBodyBytes)
	exchanges.Configure(true, 0, -1)
	rt := newTestRuntimeWithSourceAccess(t, cfg, runtime{exchanges: exchanges, observer: capture.NewObserver(exchanges), directiveReader: reader})
	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/chat", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()

	newHTTPServer(&cfg, rt).Handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent || upstreamSource != "redis" {
		t.Fatalf("unexpected proxy result: status=%d source=%q body=%s", recorder.Code, upstreamSource, recorder.Body.String())
	}
	record := exchanges.Snapshot(1).Items[0]
	if record.DirectiveMode != "remote" || record.DirectiveBackend != "redis" || record.DirectiveKey != "team-a/openai" ||
		record.DirectiveEndpoint != "redis://"+redisServer.Addr()+"/0" {
		t.Fatalf("unexpected redis directive metadata: %#v", record)
	}
}

func TestHTTPServerResolvesHTTPDirectiveEndToEnd(t *testing.T) {
	cfg := config.DefaultConfig()
	reader := newTestDirectiveReader(t, cfg)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Directive-Source") != "http" {
			t.Errorf("directive header was not applied: %#v", r.Header)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	resolver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Protocol string `json:"protocol"`
			Key      string `json:"key"`
		}
		if r.Header.Get("Authorization") != "Bearer policy-token" || json.NewDecoder(r.Body).Decode(&body) != nil ||
			body.Protocol != "dproxy.resolve.v1" || body.Key != "team-a/openai" {
			t.Errorf("unexpected resolver request: headers=%#v body=%#v", r.Header, body)
		}
		_, _ = io.WriteString(w, `{"target":{"url":"`+upstream.URL+`"},"headers":{"ops":[{"op":"=","name":"X-Directive-Source","values":["http"]}]}}`)
	}))
	defer resolver.Close()
	token, err := directive.EncodeRemote(directive.RemoteSpec{
		Type: directive.RemoteTypeHTTP,
		URL:  resolver.URL,
		Key:  "team-a/openai",
		Headers: map[string]string{
			"Authorization": "Bearer policy-token",
		},
	})
	if err != nil {
		t.Fatalf("encode HTTP token failed: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/chat", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	newHTTPServer(&cfg, newTestRuntimeWithSourceAccess(t, cfg, runtime{directiveReader: reader})).Handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("unexpected HTTP resolver result: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func newTestDirectiveReader(t *testing.T, cfg config.Config) *directiveRemoteReader {
	t.Helper()
	reader := newDirectiveRemoteReader(cfg.Proxy.Directive.Remote)
	t.Cleanup(func() { _ = reader.Close() })
	return reader
}

func TestHTTPServerReturnsProxyErrorForUnsupportedDProxyToken(t *testing.T) {
	cfg := config.DefaultConfig()
	rt := newTestRuntimeWithSourceAccess(t, cfg, runtime{})
	srv := newHTTPServer(&cfg, rt)

	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/chat/completions", nil)
	req.RemoteAddr = "127.0.0.1:1234"
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
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
			return
		}
		proxyBody = string(body)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer upstream.Close()
	token, err := directive.Encode(directive.Payload{
		Target: directive.TargetSection{URL: upstream.URL},
	})
	if err != nil {
		t.Fatalf("encode directive failed: %v", err)
	}
	rt := newTestRuntimeWithSourceAccess(t, cfg, runtime{})
	srv := newHTTPServer(&cfg, rt)

	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/api/upload", strings.NewReader("payload"))
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	srv.Handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("unexpected proxy status: %d body=%s", recorder.Code, recorder.Body.String())
	}
	if proxyBody != "payload" {
		t.Fatalf("unexpected proxy body: %q", proxyBody)
	}
}

func TestControlHealthRemainsPublicWithoutRuntimeAuthInRouteTests(t *testing.T) {
	cfg := config.DefaultConfig()
	rt := &runtime{exchanges: service.NewExchangeService(exchange.DefaultCapacity, exchange.DefaultMaxBodyBytes)}
	srv := newHTTPServer(&cfg, rt)

	healthReq := httptest.NewRequest(http.MethodGet, "http://control.local/health", nil)
	healthRecorder := httptest.NewRecorder()
	srv.Handler.ServeHTTP(healthRecorder, healthReq)
	if healthRecorder.Code != http.StatusOK {
		t.Fatalf("health must remain public, got %d", healthRecorder.Code)
	}
}

func TestDirectiveSourceAccessRejectsBeforeTokenDecode(t *testing.T) {
	cfg := config.DefaultConfig()
	rt := newTestRuntimeWithSourceAccess(t, cfg, runtime{})
	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/chat/completions", nil)
	req.RemoteAddr = "198.51.100.7:1234"
	req.Header.Set("Authorization", "Bearer dproxy.11.payload")
	recorder := httptest.NewRecorder()

	newHTTPServer(&cfg, rt).Handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), `"code":"source_not_allowed"`) {
		t.Fatalf("unexpected source denial: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestDirectiveSourceAccessUsesTrustedProxyChain(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Proxy.Directive.SourceAccess.AllowedSources = []string{"198.51.100.7"}
	cfg.Proxy.Directive.SourceAccess.TrustedProxies = []string{"192.0.2.0/24"}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	token, err := directive.Encode(directive.Payload{Target: directive.TargetSection{URL: upstream.URL}})
	if err != nil {
		t.Fatalf("encode directive: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/chat", nil)
	req.RemoteAddr = "192.0.2.1:1234"
	req.Header.Set("X-Forwarded-For", "198.51.100.7")
	req.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()

	newHTTPServer(&cfg, newTestRuntimeWithSourceAccess(t, cfg, runtime{})).Handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("unexpected trusted proxy result: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestDirectiveSourceAccessRejectsMalformedTrustedProxyHeader(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Proxy.Directive.SourceAccess.AllowedSources = []string{"198.51.100.7"}
	cfg.Proxy.Directive.SourceAccess.TrustedProxies = []string{"192.0.2.0/24"}
	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/chat", nil)
	req.RemoteAddr = "192.0.2.1:1234"
	req.Header.Set("Forwarded", `for="unterminated`)
	req.Header.Set("X-Forwarded-For", "198.51.100.7")
	req.Header.Set("Authorization", "Bearer dproxy.11.payload")
	recorder := httptest.NewRecorder()

	newHTTPServer(&cfg, newTestRuntimeWithSourceAccess(t, cfg, runtime{})).Handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), `"code":"source_invalid"`) {
		t.Fatalf("unexpected invalid source response: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestDirectiveSourceAccessFailsClosedWhenRuntimeIsUnavailable(t *testing.T) {
	cfg := config.DefaultConfig()
	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/chat", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Authorization", "Bearer dproxy.11.payload")
	recorder := httptest.NewRecorder()

	newHTTPServer(&cfg, &runtime{}).Handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusServiceUnavailable || !strings.Contains(recorder.Body.String(), `"code":"source_access_unavailable"`) {
		t.Fatalf("unexpected unavailable source access response: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func newTestRuntimeWithSourceAccess(t *testing.T, cfg config.Config, value runtime) *runtime {
	t.Helper()
	access, engine, err := newDirectiveSourceAccess(cfg.Proxy.Directive.SourceAccess)
	if err != nil {
		t.Fatalf("configure test source access: %v", err)
	}
	t.Cleanup(engine.Close)
	value.sourceAccess = access
	value.sourceEngine = engine
	return &value
}

func TestRuntimeCloseClosesSourceEngine(t *testing.T) {
	cfg := config.DefaultConfig()
	rt := newTestRuntimeWithSourceAccess(t, cfg, runtime{})
	engine := rt.sourceEngine
	policy, err := sourceaccess.CompileSources([]string{"127.0.0.1"})
	if err != nil {
		t.Fatalf("compile test policy: %v", err)
	}
	if err := rt.Close(context.Background()); err != nil {
		t.Fatalf("close runtime: %v", err)
	}
	result := engine.Evaluate(context.Background(), policy, netip.MustParseAddr("127.0.0.1"))
	if result.Decision.Reason != sourceaccess.ReasonEngineClosed || rt.sourceEngine != nil || rt.sourceAccess != nil {
		t.Fatalf("source engine remained available after close: %#v", result)
	}
}

func TestNoStoreDisablesCaching(t *testing.T) {
	handler := noStore(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://control.local/oidcauth/session", nil))

	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("unexpected Cache-Control: %q", got)
	}
}
