package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	miniredisServer "github.com/alicebob/miniredis/v2/server"
	"github.com/lwmacct/260711-go-pkg-httpauth/pkg/httpauth/statictoken"

	proxyrequestadapter "github.com/lwmacct/260628-directive-proxy/internal/adapter/proxyrequest"
	"github.com/lwmacct/260628-directive-proxy/internal/config"
	"github.com/lwmacct/260628-directive-proxy/internal/core/directive"
	"github.com/lwmacct/260628-directive-proxy/internal/core/proxy"
	"github.com/lwmacct/260628-directive-proxy/internal/types"
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

func TestHTTPServerRoutesAdminAndProxyRequestsOnOneListener(t *testing.T) {
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
	setTestRetryID(proxyReq, 10)
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

func TestHTTPServerAllowsRequesterRetryByRetryIDWithoutAdminAuthentication(t *testing.T) {
	cfg := config.DefaultConfig()
	adminToken := "dpctl.10.admin." + strings.Repeat("Z", 32)
	digest, err := statictoken.Digest(types.AdminTokenNamespace, adminToken)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Server.HTTP.Auth.ExternalURLs = []string{"http://localhost"}
	cfg.Server.HTTP.Auth.Session.Keys[0].Secret = base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("p", 32)))
	cfg.Server.HTTP.Auth.Token.Credentials = map[string]statictoken.Credential{
		"admin": {Name: "Administrator", TokenSHA256: digest},
	}
	adminAuth, err := newAdminAuth(t.Context(), cfg.Server.HTTP)
	if err != nil {
		t.Fatal(err)
	}
	tracker := proxyrequestadapter.NewProxyRequestService(proxyrequestadapter.ProxyRequestOptions{
		MaxAttempts: 3,
	}, nil)
	base := proxy.NewProxyAwareTransport(http.DefaultTransport.(*http.Transport))
	retryTransport, err := proxy.NewRetryTransport(base, proxy.RetryTransportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	firstStarted := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, readErr := io.ReadAll(r.Body)
		if readErr != nil || string(body) != "payload" {
			t.Errorf("unexpected upstream body: body=%q err=%v", body, readErr)
		}
		if value := r.Header.Get("X-Dproxy-Request-ID"); value != "" {
			t.Errorf("request metadata leaked upstream: %q", value)
		}
		if calls.Add(1) == 1 {
			close(firstStarted)
			<-r.Context().Done()
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, "retried")
	}))
	defer upstream.Close()
	token, err := directive.Encode(directive.Payload{
		Target: directive.TargetSection{URL: upstream.URL},
		Headers: &directive.HeaderSection{Ops: []directive.HeaderOp{{
			Op: "=", Name: "X-Dproxy-Request-ID", Values: []string{"client-request-1"},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	rt := &runtime{requests: tracker, bodyMemory: newTestBodyMemory(cfg.Proxy.BodyMemory), proxyTransport: retryTransport, adminAuth: adminAuth}
	handler := newHTTPServer(&cfg, rt).Handler
	proxyReq := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/resources", strings.NewReader("payload"))
	proxyReq.Header.Set("Authorization", "Bearer "+token)
	proxyReq.Header.Set("Idempotency-Key", "request-3")
	retryID := setTestRetryID(proxyReq, 3)
	proxyRecorder := httptest.NewRecorder()
	proxyDone := make(chan struct{})
	go func() {
		handler.ServeHTTP(proxyRecorder, proxyReq)
		close(proxyDone)
	}()

	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first upstream attempt did not start")
	}
	retryReq := httptest.NewRequest(http.MethodPut, "http://control.local/api/public/retry", nil)
	retryReq.Header.Set("Dproxy-Retry-ID", retryID)
	retryReq.Header.Set("If-Match", `"attempt:1"`)
	retryRecorder := httptest.NewRecorder()
	handler.ServeHTTP(retryRecorder, retryReq)
	if retryRecorder.Code != http.StatusAccepted {
		t.Fatalf("unexpected retry response: status=%d body=%s", retryRecorder.Code, retryRecorder.Body.String())
	}
	var retryBody struct {
		TraceID string `json:"trace_id"`
	}
	if err := json.Unmarshal(retryRecorder.Body.Bytes(), &retryBody); err != nil || retryBody.TraceID == "" {
		t.Fatalf("unexpected retry response body: body=%s err=%v", retryRecorder.Body.String(), err)
	}
	select {
	case <-proxyDone:
	case <-time.After(2 * time.Second):
		t.Fatal("retried proxy request did not complete")
	}
	if proxyRecorder.Code != http.StatusCreated || proxyRecorder.Body.String() != "retried" || calls.Load() != 2 {
		t.Fatalf("unexpected retried response: status=%d body=%q calls=%d", proxyRecorder.Code, proxyRecorder.Body.String(), calls.Load())
	}
	if proxyRecorder.Header().Get("X-Dproxy-Trace-ID") != retryBody.TraceID {
		t.Fatalf("trace ID response header mismatch: %#v", proxyRecorder.Header())
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
	if err := redisServer.Set("team-a/service-a", `{"target":{"url":"`+upstream.URL+`"},"headers":{"ops":[{"op":"=","name":"X-Directive-Source","values":["redis"]}]}}`); err != nil {
		t.Fatalf("seed Redis directive: %v", err)
	}
	token, err := directive.EncodeRemote(directive.RemoteSpec{
		Type: directive.RemoteTypeRedis,
		URL:  "redis://" + redisServer.Addr() + "/0",
		Key:  "team-a/service-a",
	})
	if err != nil {
		t.Fatalf("encode redis token failed: %v", err)
	}
	rt := newTestRuntimeWithSourceAccess(t, cfg, runtime{directiveReader: reader})
	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/resources", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Authorization", "Bearer "+token)
	setTestRetryID(req, 11)
	recorder := httptest.NewRecorder()

	newHTTPServer(&cfg, rt).Handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent || upstreamSource != "redis" {
		t.Fatalf("unexpected proxy result: status=%d source=%q body=%s", recorder.Code, upstreamSource, recorder.Body.String())
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
			body.Protocol != "dproxy.resolve.v1" || body.Key != "team-a/service-a" {
			t.Errorf("unexpected resolver request: headers=%#v body=%#v", r.Header, body)
		}
		_, _ = io.WriteString(w, `{"target":{"url":"`+upstream.URL+`"},"headers":{"ops":[{"op":"=","name":"X-Directive-Source","values":["http"]}]}}`)
	}))
	defer resolver.Close()
	token, err := directive.EncodeRemote(directive.RemoteSpec{
		Type: directive.RemoteTypeHTTP,
		URL:  resolver.URL,
		Key:  "team-a/service-a",
		Headers: map[string]string{
			"Authorization": "Bearer policy-token",
		},
	})
	if err != nil {
		t.Fatalf("encode HTTP token failed: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/resources", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Authorization", "Bearer "+token)
	setTestRetryID(req, 12)
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

	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/resources", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Authorization", "Bearer dproxy.11.payload")
	setTestRetryID(req, 13)
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
	setTestRetryID(req, 14)
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
	rt := &runtime{}
	srv := newHTTPServer(&cfg, rt)

	healthReq := httptest.NewRequest(http.MethodGet, "http://control.local/health", nil)
	healthRecorder := httptest.NewRecorder()
	srv.Handler.ServeHTTP(healthRecorder, healthReq)
	if healthRecorder.Code != http.StatusOK {
		t.Fatalf("health must remain public, got %d", healthRecorder.Code)
	}
}

func TestDirectiveSourceAccessIsDisabledByDefault(t *testing.T) {
	cfg := config.DefaultConfig()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	token, err := directive.Encode(directive.Payload{Target: directive.TargetSection{URL: upstream.URL}})
	if err != nil {
		t.Fatalf("encode directive: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/resources", nil)
	req.RemoteAddr = "198.51.100.7:1234"
	req.Header.Set("Authorization", "Bearer "+token)
	setTestRetryID(req, 15)
	recorder := httptest.NewRecorder()

	newHTTPServer(&cfg, &runtime{}).Handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("disabled source access blocked directive: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestDirectiveSourceAccessRejectsBeforeTokenDecode(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Proxy.Directive.SourceAccess.Enabled = true
	rt := newTestRuntimeWithSourceAccess(t, cfg, runtime{})
	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/resources", nil)
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
	cfg.Proxy.Directive.SourceAccess.Enabled = true
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
	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/resources", nil)
	req.RemoteAddr = "192.0.2.1:1234"
	req.Header.Set("X-Forwarded-For", "198.51.100.7")
	req.Header.Set("Authorization", "Bearer "+token)
	setTestRetryID(req, 16)
	recorder := httptest.NewRecorder()

	newHTTPServer(&cfg, newTestRuntimeWithSourceAccess(t, cfg, runtime{})).Handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("unexpected trusted proxy result: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestDirectiveSourceAccessRejectsMalformedTrustedProxyHeader(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Proxy.Directive.SourceAccess.Enabled = true
	cfg.Proxy.Directive.SourceAccess.AllowedSources = []string{"198.51.100.7"}
	cfg.Proxy.Directive.SourceAccess.TrustedProxies = []string{"192.0.2.0/24"}
	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/resources", nil)
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
	cfg.Proxy.Directive.SourceAccess.Enabled = true
	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/resources", nil)
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
	if value.bodyMemory == nil {
		value.bodyMemory = newTestBodyMemory(cfg.Proxy.BodyMemory)
	}
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
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://control.local/auth/session", nil))

	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("unexpected Cache-Control: %q", got)
	}
}

func TestTokenAuthProtectsAdminAPI(t *testing.T) {
	token := "dpctl.10.admin." + strings.Repeat("Y", 32)
	digest, err := statictoken.Digest(types.AdminTokenNamespace, token)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.Server.HTTP.Auth.ExternalURLs = []string{"http://localhost"}
	cfg.Server.HTTP.Auth.Session.Keys[0].Secret = base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))
	cfg.Server.HTTP.Auth.Token.Credentials = map[string]statictoken.Credential{
		"admin": {Name: "Administrator", TokenSHA256: digest},
	}
	auth, err := newAdminAuth(t.Context(), cfg.Server.HTTP)
	if err != nil {
		t.Fatalf("configure access token auth: %v", err)
	}
	rt := &runtime{adminAuth: auth}
	handler := newHTTPServer(&cfg, rt).Handler

	authSession := httptest.NewRecorder()
	handler.ServeHTTP(authSession, httptest.NewRequest(http.MethodGet, "http://localhost/auth/session", nil))
	if authSession.Code != http.StatusOK || authSession.Header().Get("Cache-Control") != "no-store" ||
		!strings.Contains(authSession.Body.String(), `"id":"token"`) || !strings.Contains(authSession.Body.String(), `"status":"signed-out"`) {
		t.Fatalf("unexpected auth session: status=%d body=%s", authSession.Code, authSession.Body.String())
	}

	unauthenticated := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "http://localhost/api/admin/proxy-requests", nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unexpected unauthenticated status: %d", unauthenticated.Code)
	}

	loginRequest := httptest.NewRequest(http.MethodPost, "http://localhost/auth/login/token", strings.NewReader(`{"token":"`+token+`"}`))
	loginRequest.Header.Set("Origin", "http://localhost")
	login := httptest.NewRecorder()
	handler.ServeHTTP(login, loginRequest)
	if login.Code != http.StatusNoContent || login.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unexpected login: status=%d body=%s", login.Code, login.Body.String())
	}

	protectedRequest := httptest.NewRequest(http.MethodGet, "http://localhost/api/admin/proxy-requests", nil)
	protectedRequest.AddCookie(login.Result().Cookies()[0])
	protected := httptest.NewRecorder()
	handler.ServeHTTP(protected, protectedRequest)
	if protected.Code != http.StatusOK {
		t.Fatalf("unexpected authenticated status: %d body=%s", protected.Code, protected.Body.String())
	}

	bearerRequest := httptest.NewRequest(http.MethodGet, "http://localhost/api/admin/proxy-requests", nil)
	bearerRequest.Header.Set("Authorization", "Bearer "+token)
	bearer := httptest.NewRecorder()
	handler.ServeHTTP(bearer, bearerRequest)
	if bearer.Code != http.StatusOK {
		t.Fatalf("unexpected bearer status: %d body=%s", bearer.Code, bearer.Body.String())
	}
}
