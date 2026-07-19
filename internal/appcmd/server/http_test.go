package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	miniredisServer "github.com/alicebob/miniredis/v2/server"
	"github.com/lwmacct/260711-go-pkg-authme/pkg/authme/adapters/statictoken"

	"github.com/lwmacct/260628-directive-proxy/internal/adapter/directivehttp"
	"github.com/lwmacct/260628-directive-proxy/internal/config"
	"github.com/lwmacct/260628-directive-proxy/internal/core/directive"
	"github.com/lwmacct/260628-directive-proxy/internal/core/exchange"
	"github.com/lwmacct/260718-go-pkg-ipallow/pkg/ipallow"
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

func testServerDirectiveMetadata() map[string]string {
	return map[string]string{"user_key": "uk_server_test"}
}

func TestHTTPServerRoutesControlAndProxyRequestsOnOneListener(t *testing.T) {
	cfg := newTestServerConfig()
	var proxyPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyPath = r.URL.Path
		w.WriteHeader(http.StatusAccepted)
	}))
	defer upstream.Close()
	token, err := directive.Encode(testDirectiveSecret, directive.Payload{
		Metadata: testServerDirectiveMetadata(),
		Target:   directive.TargetSection{BaseURL: upstream.URL},
	})
	if err != nil {
		t.Fatalf("encode directive failed: %v", err)
	}
	rt := newTestRuntimeWithSourceAccess(t, cfg, runtime{})
	srv := newHTTPServer(&cfg, rt)

	if srv.Addr != ":23198" {
		t.Fatalf("unexpected http listen: %q", srv.Addr)
	}
	if srv.ReadHeaderTimeout != 10*time.Second || srv.ReadTimeout != 0 || srv.WriteTimeout != 0 ||
		srv.IdleTimeout != 0 || srv.MaxHeaderBytes != 0 {
		t.Fatalf("unexpected HTTP server limits: read_header=%s read=%s write=%s idle=%s headers=%d",
			srv.ReadHeaderTimeout, srv.ReadTimeout, srv.WriteTimeout, srv.IdleTimeout, srv.MaxHeaderBytes)
	}

	rootHealthReq := httptest.NewRequest(http.MethodGet, "http://control.local/health", nil)
	rootHealthRecorder := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rootHealthRecorder, rootHealthReq)
	if rootHealthRecorder.Code != http.StatusOK {
		t.Fatalf("unexpected root health status: %d", rootHealthRecorder.Code)
	}

	proxyReq := httptest.NewRequest(http.MethodPost, "http://service.local/api/resources", nil)
	proxyReq.RemoteAddr = "127.0.0.1:1234"
	proxyReq.Header.Set("Authorization", "Bearer "+token)
	proxyReq.Header.Set("Idempotency-Key", "request-3")
	proxyRecorder := httptest.NewRecorder()
	srv.Handler.ServeHTTP(proxyRecorder, proxyReq)
	if proxyRecorder.Code != http.StatusAccepted {
		t.Fatalf("unexpected proxy status: %d", proxyRecorder.Code)
	}
	if proxyPath != "/api/resources" {
		t.Fatalf("proxy path was modified: %q", proxyPath)
	}
	reservedReq := httptest.NewRequest(http.MethodPost, "http://service.local/api/public/unknown", nil)
	reservedReq.Header.Set("Authorization", "Bearer "+token)
	reservedRecorder := httptest.NewRecorder()
	srv.Handler.ServeHTTP(reservedRecorder, reservedReq)
	if reservedRecorder.Code != http.StatusNotFound || proxyPath != "/api/resources" {
		t.Fatalf("reserved public API path reached data plane: status=%d proxy_path=%q", reservedRecorder.Code, proxyPath)
	}
	ordinaryBearerReq := httptest.NewRequest(http.MethodGet, "http://service.local/health", nil)
	ordinaryBearerReq.Header.Set("Authorization", "Bearer upstream-token")
	ordinaryBearerRecorder := httptest.NewRecorder()
	srv.Handler.ServeHTTP(ordinaryBearerRecorder, ordinaryBearerReq)
	if ordinaryBearerRecorder.Code != http.StatusOK {
		t.Fatalf("ordinary bearer request must use fallback handler, got %d", ordinaryBearerRecorder.Code)
	}
}

func TestHTTPServerResolvesRedisDirectiveEndToEnd(t *testing.T) {
	cfg := newTestServerConfig()
	redisServer := miniredis.RunT(t)
	enableRedisJSON(t, redisServer)
	remotes := newTestDirectiveRemotes(t, cfg)

	var upstreamSource string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamSource = r.Header.Get("X-Directive-Source")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	if err := redisServer.Set("team-a/service-a", `{"metadata":{"user_key":"uk_redis"},"target":{"base_url":"`+upstream.URL+`"},"headers":{"mutations":[{"side":"request","action":"set","name":"X-Directive-Source","values":["redis"]}]}}`); err != nil {
		t.Fatalf("seed Redis directive: %v", err)
	}
	token, err := directive.EncodeRemote(testDirectiveSecret, directive.RemoteSpec{
		Redis: &directive.RedisRemoteSpec{URL: "redis://" + redisServer.Addr() + "/0", Key: "team-a/service-a"},
	})
	if err != nil {
		t.Fatalf("encode redis token failed: %v", err)
	}
	rt := newTestRuntimeWithSourceAccess(t, cfg, runtime{directiveRemotes: remotes})
	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/resources", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()

	newHTTPServer(&cfg, rt).Handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent || upstreamSource != "redis" {
		t.Fatalf("unexpected proxy result: status=%d source=%q body=%s", recorder.Code, upstreamSource, recorder.Body.String())
	}
}

func TestHTTPServerResolvesFileDirectiveEndToEnd(t *testing.T) {
	cfg := newTestServerConfig()
	cfg.Proxy.Directive.Remote.File.Root = t.TempDir()
	path := filepath.Join(cfg.Proxy.Directive.Remote.File.Root, "team-a", "services")
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	var upstreamSource string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		upstreamSource = request.Header.Get("X-Directive-Source")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	payload := `{"metadata":{"user_key":"uk_file"},"target":{"base_url":"` + upstream.URL + `"},"headers":{"mutations":[{"side":"request","action":"set","name":"X-Directive-Source","values":["file"]}]}}`
	if err := os.WriteFile(filepath.Join(path, "primary.json"), []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	remotes := newTestDirectiveRemotes(t, cfg)
	token, err := directive.EncodeRemote(testDirectiveSecret, directive.RemoteSpec{File: &directive.FileRemoteSpec{Path: "team-a/services/primary.json"}})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/resources", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	newHTTPServer(&cfg, newTestRuntimeWithSourceAccess(t, cfg, runtime{directiveRemotes: remotes})).Handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent || upstreamSource != "file" {
		t.Fatalf("unexpected file resolver result: status=%d source=%q body=%s", recorder.Code, upstreamSource, recorder.Body.String())
	}
}

func TestHTTPServerResolvesHTTPDirectiveEndToEnd(t *testing.T) {
	cfg := newTestServerConfig()
	remotes := newTestDirectiveRemotes(t, cfg)
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
		}
		if r.Header.Get("Authorization") != "Bearer policy-token" || json.NewDecoder(r.Body).Decode(&body) != nil ||
			body.Protocol != directivehttp.Protocol || r.URL.Path != "/team-a/service-a" {
			t.Errorf("unexpected resolver request: headers=%#v body=%#v", r.Header, body)
		}
		_, _ = io.WriteString(w, `{"metadata":{"user_key":"uk_http"},"target":{"base_url":"`+upstream.URL+`"},"headers":{"mutations":[{"side":"request","action":"set","name":"X-Directive-Source","values":["http"]}]}}`)
	}))
	defer resolver.Close()
	token, err := directive.EncodeRemote(testDirectiveSecret, directive.RemoteSpec{
		HTTP: &directive.HTTPRemoteSpec{
			URL: resolver.URL + "/team-a/service-a",
			Headers: &directive.HeaderPolicy{Mutations: []directive.HeaderMutation{{
				Side: directive.HeaderSideRequest, Action: directive.HeaderActionSet, Name: "Authorization", Values: []string{"Bearer policy-token"},
			}},
			},
		},
	})
	if err != nil {
		t.Fatalf("encode HTTP token failed: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/resources", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	newHTTPServer(&cfg, newTestRuntimeWithSourceAccess(t, cfg, runtime{directiveRemotes: remotes})).Handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("unexpected HTTP resolver result: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func newTestDirectiveRemotes(t *testing.T, cfg config.Server) *directiveRemotes {
	t.Helper()
	remotes := newDirectiveRemotes(cfg.Proxy.Directive.Remote, cfg.Proxy.Transport)
	t.Cleanup(func() { _ = remotes.Close() })
	return remotes
}

func TestHTTPServerReturnsProxyErrorForUnsupportedDPToken(t *testing.T) {
	cfg := config.DefaultConfig().Server
	rt := newTestRuntimeWithSourceAccess(t, cfg, runtime{})
	srv := newHTTPServer(&cfg, rt)

	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/resources", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Authorization", "Bearer dp.999.inline.payload")
	recorder := httptest.NewRecorder()
	srv.Handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("unexpected proxy status: %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "invalid proxy directive payload") {
		t.Fatalf("unexpected proxy error: %s", recorder.Body.String())
	}
}

func TestHTTPServerRejectsWrongDirectiveTokenSecret(t *testing.T) {
	cfg := config.DefaultConfig().Server
	token, err := directive.Encode("wrong-directive-secret", directive.Payload{Metadata: testServerDirectiveMetadata(), Target: directive.TargetSection{BaseURL: "https://api.example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/resources", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()

	newHTTPServer(&cfg, newTestRuntimeWithSourceAccess(t, cfg, runtime{})).Handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized || recorder.Header().Get("WWW-Authenticate") != "Bearer" ||
		!strings.Contains(recorder.Body.String(), `"code":"directive_unauthorized"`) {
		t.Fatalf("unexpected directive authorization response: status=%d headers=%#v body=%s", recorder.Code, recorder.Header(), recorder.Body.String())
	}
}

func TestHTTPServerForwardsProxyRequestBody(t *testing.T) {
	cfg := newTestServerConfig()
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
	token, err := directive.Encode(testDirectiveSecret, directive.Payload{
		Metadata: testServerDirectiveMetadata(),
		Target:   directive.TargetSection{BaseURL: upstream.URL},
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
	cfg := config.DefaultConfig().Server
	rt := &runtime{}
	srv := newHTTPServer(&cfg, rt)

	healthReq := httptest.NewRequest(http.MethodGet, "http://control.local/health", nil)
	healthRecorder := httptest.NewRecorder()
	srv.Handler.ServeHTTP(healthRecorder, healthReq)
	if healthRecorder.Code != http.StatusOK {
		t.Fatalf("health must remain public, got %d", healthRecorder.Code)
	}
}

func TestHTTPServerMetricsIsPublicAndTakesPrecedenceOverDirectiveAuth(t *testing.T) {
	cfg := config.DefaultConfig().Server
	cfg.Proxy.Directive.SourceAccess.Enabled = true
	handler := newHTTPServer(&cfg, &runtime{bodyStore: newTestBodyStore(cfg.Proxy.BodyStore)}).Handler

	request := httptest.NewRequest(http.MethodGet, "http://control.local/metrics", nil)
	request.Header.Set("Authorization", "Bearer this-is-a-directive-token")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected metrics status: %d body=%s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != metricsContentType {
		t.Fatalf("unexpected metrics content type: %q", got)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("unexpected metrics cache policy: %q", got)
	}
	body := recorder.Body.String()
	for _, metric := range []string{
		"directive_proxy_body_store_memory_limit_bytes",
		"directive_proxy_event_output_enabled 0",
		"go_goroutines",
	} {
		if !strings.Contains(body, metric) {
			t.Fatalf("metrics output is missing %q: %s", metric, body)
		}
	}
}

func TestHTTPServerMetricsRejectsNonGet(t *testing.T) {
	cfg := config.DefaultConfig().Server
	recorder := httptest.NewRecorder()
	newHTTPServer(&cfg, &runtime{}).Handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "http://control.local/metrics", nil))
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("unexpected non-GET metrics status: %d", recorder.Code)
	}
	if got := recorder.Header().Get("Allow"); got != http.MethodGet {
		t.Fatalf("unexpected metrics Allow header: %q", got)
	}
}

func TestHTTPServerMetricsTrackProxyRequestAndRoundTrip(t *testing.T) {
	cfg := newTestServerConfig()
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil || string(body) != "request" {
			t.Errorf("unexpected upstream request body: %q err=%v", body, err)
		}
		writer.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(writer, "response")
	}))
	defer upstream.Close()
	token, err := directive.Encode(testDirectiveSecret, directive.Payload{
		Metadata: testServerDirectiveMetadata(),
		Target:   directive.TargetSection{BaseURL: upstream.URL},
	})
	if err != nil {
		t.Fatal(err)
	}
	runtimeMetrics := newRuntimeMetrics()
	store := newTestBodyStore(cfg.Proxy.BodyStore)
	store.RegisterMetrics(runtimeMetrics.MetricsSet())
	runtimeMetrics.RegisterDisabledEventOutput()
	rt := newTestRuntimeWithSourceAccess(t, cfg, runtime{
		metrics:         runtimeMetrics,
		bodyStore:       store,
		exchangeFactory: exchange.NewManager(exchange.ManagerOptions{MaxRoundTrips: 2, Metrics: runtimeMetrics}, nil),
	})
	handler := newHTTPServer(&cfg, rt).Handler
	request := httptest.NewRequest(http.MethodPost, "http://proxy.local/resource", strings.NewReader("request"))
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || response.Body.String() != "response" {
		t.Fatalf("unexpected proxy response: status=%d body=%q", response.Code, response.Body.String())
	}

	scrape := httptest.NewRecorder()
	handler.ServeHTTP(scrape, httptest.NewRequest(http.MethodGet, "http://control.local/metrics", nil))
	for _, metric := range []string{
		`directive_proxy_requests_total{outcome="success"} 1`,
		`directive_proxy_responses_total{status_class="2xx"} 1`,
		"directive_proxy_request_body_bytes_total 7",
		"directive_proxy_response_body_bytes_total 8",
		"directive_proxy_request_duration_seconds_count 1",
		"directive_proxy_round_trips_total 1",
		"directive_proxy_round_trip_duration_seconds_count 1",
		"directive_proxy_body_store_admitted_total 1",
	} {
		if !strings.Contains(scrape.Body.String(), metric) {
			t.Fatalf("metrics output is missing %q: %s", metric, scrape.Body.String())
		}
	}
}

func TestHTTPServerServesDirectiveWorkbenchSPA(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("directive-workbench"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "app.js"), []byte("asset"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WEB_ROOT", root)
	cfg := config.DefaultConfig().Server
	handler := newHTTPServer(&cfg, &runtime{}).Handler

	spa := httptest.NewRecorder()
	handler.ServeHTTP(spa, httptest.NewRequest(http.MethodGet, "http://control.local/console/auth-console", nil))
	if spa.Code != http.StatusOK || spa.Body.String() != "directive-workbench" {
		t.Fatalf("unexpected SPA fallback: status=%d body=%q", spa.Code, spa.Body.String())
	}

	asset := httptest.NewRecorder()
	handler.ServeHTTP(asset, httptest.NewRequest(http.MethodGet, "http://control.local/app.js", nil))
	if asset.Code != http.StatusOK || asset.Body.String() != "asset" {
		t.Fatalf("unexpected static asset: status=%d body=%q", asset.Code, asset.Body.String())
	}
}

func TestDirectiveSourceAccessIsDisabledByDefault(t *testing.T) {
	cfg := newTestServerConfig()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	token, err := directive.Encode(testDirectiveSecret, directive.Payload{Metadata: testServerDirectiveMetadata(), Target: directive.TargetSection{BaseURL: upstream.URL}})
	if err != nil {
		t.Fatalf("encode directive: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/resources", nil)
	req.RemoteAddr = "198.51.100.7:1234"
	req.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()

	newHTTPServer(&cfg, &runtime{}).Handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("disabled source access blocked directive: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestDirectiveSourceAccessRejectsBeforeTokenDecode(t *testing.T) {
	cfg := config.DefaultConfig().Server
	cfg.Proxy.Directive.SourceAccess.Enabled = true
	rt := newTestRuntimeWithSourceAccess(t, cfg, runtime{})
	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/resources", nil)
	req.RemoteAddr = "198.51.100.7:1234"
	req.Header.Set("Authorization", "Bearer dp.999.inline.payload")
	recorder := httptest.NewRecorder()

	newHTTPServer(&cfg, rt).Handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), `"code":"source_not_allowed"`) {
		t.Fatalf("unexpected source denial: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestDirectiveSourceAccessUsesTrustedProxyChain(t *testing.T) {
	cfg := newTestServerConfig()
	cfg.Proxy.Directive.SourceAccess.Enabled = true
	cfg.Proxy.Directive.SourceAccess.Rules = []ipallow.Rule{{Value: "198.51.100.7"}}
	cfg.Proxy.Directive.SourceAccess.TrustedProxies = []string{"192.0.2.0/24"}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	token, err := directive.Encode(testDirectiveSecret, directive.Payload{Metadata: testServerDirectiveMetadata(), Target: directive.TargetSection{BaseURL: upstream.URL}})
	if err != nil {
		t.Fatalf("encode directive: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/resources", nil)
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
	cfg := config.DefaultConfig().Server
	cfg.Proxy.Directive.SourceAccess.Enabled = true
	cfg.Proxy.Directive.SourceAccess.Rules = []ipallow.Rule{{Value: "198.51.100.7"}}
	cfg.Proxy.Directive.SourceAccess.TrustedProxies = []string{"192.0.2.0/24"}
	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/resources", nil)
	req.RemoteAddr = "192.0.2.1:1234"
	req.Header.Set("Forwarded", `for="unterminated`)
	req.Header.Set("X-Forwarded-For", "198.51.100.7")
	req.Header.Set("Authorization", "Bearer dp.999.inline.payload")
	recorder := httptest.NewRecorder()

	newHTTPServer(&cfg, newTestRuntimeWithSourceAccess(t, cfg, runtime{})).Handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), `"code":"source_invalid"`) {
		t.Fatalf("unexpected invalid source response: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestDirectiveSourceAccessFailsClosedWhenRuntimeIsUnavailable(t *testing.T) {
	cfg := config.DefaultConfig().Server
	cfg.Proxy.Directive.SourceAccess.Enabled = true
	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/resources", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Authorization", "Bearer dp.999.inline.payload")
	recorder := httptest.NewRecorder()

	newHTTPServer(&cfg, &runtime{}).Handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusServiceUnavailable || !strings.Contains(recorder.Body.String(), `"code":"source_access_unavailable"`) {
		t.Fatalf("unexpected unavailable source access response: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func newTestRuntimeWithSourceAccess(t *testing.T, cfg config.Server, value runtime) *runtime {
	t.Helper()
	access, err := newDirectiveSourceAccess(context.Background(), cfg.Proxy.Directive.SourceAccess)
	if err != nil {
		t.Fatalf("configure test source access: %v", err)
	}
	t.Cleanup(access.Close)
	value.sourceAccess = access
	if value.bodyStore == nil {
		value.bodyStore = newTestBodyStore(cfg.Proxy.BodyStore)
	}
	return &value
}

func TestRuntimeCloseClosesSourceMatcher(t *testing.T) {
	cfg := config.DefaultConfig().Server
	rt := newTestRuntimeWithSourceAccess(t, cfg, runtime{})
	matcher := rt.sourceAccess.matcher
	policy, err := ipallow.Compile([]ipallow.Rule{{Value: "127.0.0.1"}})
	if err != nil {
		t.Fatalf("compile test policy: %v", err)
	}
	if err := rt.Close(context.Background()); err != nil {
		t.Fatalf("close runtime: %v", err)
	}
	result, matchErr := matcher.Match(context.Background(), policy, netip.MustParseAddr("127.0.0.1"))
	if !errors.Is(matchErr, ipallow.ErrMatcherClosed) || result.Reason != ipallow.ReasonMatcherClosed || rt.sourceAccess != nil {
		t.Fatalf("source matcher remained available after close: result=%#v err=%v", result, matchErr)
	}
}

func TestNoStoreDisablesCaching(t *testing.T) {
	handler := noStore(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://control.local/authme/session", nil))

	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("unexpected Cache-Control: %q", got)
	}
}

func TestTokenAuthSession(t *testing.T) {
	token := "admin-token/with.punctuation"
	cfg := config.DefaultConfig().Server
	cfg.HTTP.AuthMe.Origins = []string{"http://localhost"}
	cfg.HTTP.AuthMe.Session.Keys[0].Secret = base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))
	cfg.HTTP.AuthMe.StaticToken.Credentials = []statictoken.Credential{{ID: "admin", Name: "Administrator", Token: token}}
	auth, err := newAdminAuth(t.Context(), cfg.HTTP)
	if err != nil {
		t.Fatalf("configure access token auth: %v", err)
	}
	rt := &runtime{adminAuth: auth}
	handler := newHTTPServer(&cfg, rt).Handler

	authSession := httptest.NewRecorder()
	handler.ServeHTTP(authSession, httptest.NewRequest(http.MethodGet, "http://localhost/authme/session", nil))
	if authSession.Code != http.StatusOK || authSession.Header().Get("Cache-Control") != "no-store" ||
		!strings.Contains(authSession.Body.String(), `"id":"token"`) || !strings.Contains(authSession.Body.String(), `"status":"signed-out"`) {
		t.Fatalf("unexpected auth session: status=%d body=%s", authSession.Code, authSession.Body.String())
	}

	loginRequest := httptest.NewRequest(http.MethodPost, "http://localhost/authme/login/token", strings.NewReader(`{"token":"`+token+`"}`))
	loginRequest.Header.Set("Origin", "http://localhost")
	login := httptest.NewRecorder()
	handler.ServeHTTP(login, loginRequest)
	if login.Code != http.StatusNoContent || login.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unexpected login: status=%d body=%s", login.Code, login.Body.String())
	}

	authenticatedRequest := httptest.NewRequest(http.MethodGet, "http://localhost/authme/session", nil)
	authenticatedRequest.AddCookie(login.Result().Cookies()[0])
	authenticated := httptest.NewRecorder()
	handler.ServeHTTP(authenticated, authenticatedRequest)
	if authenticated.Code != http.StatusOK || !strings.Contains(authenticated.Body.String(), `"status":"authenticated"`) {
		t.Fatalf("unexpected authenticated session: status=%d body=%s", authenticated.Code, authenticated.Body.String())
	}
}
