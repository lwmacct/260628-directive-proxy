package server

import (
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/config"
	"github.com/lwmacct/260628-directive-proxy/internal/core/directive"
	"github.com/lwmacct/260628-directive-proxy/internal/core/proxy"
)

const (
	publicAPIPrefix = "/api/public"
	adminAPIPrefix  = "/api/admin"
)

func newHTTPServer(cfg *config.Server, rt *runtime) *http.Server {
	httpCfg := cfg.HTTP
	srv := &http.Server{
		Addr:              httpCfg.Listen,
		Handler:           newHTTPHandler(cfg, rt),
		ReadHeaderTimeout: 10 * time.Second,
	}

	if rt.tls == nil || rt.tls.config == nil {
		return srv
	}
	srv.TLSConfig = rt.tls.config
	return srv
}

func newHTTPHandler(cfg *config.Server, rt *runtime) http.Handler {
	runtimeMetrics := ensureRuntimeMetrics(rt)
	health := newHealthHandler(rt.programRuntime, rt.eventOutput, rt.bodyStore)
	metrics := &metricsHandler{set: runtimeMetrics.MetricsSet()}
	fallback := newFallbackHTTPHandler(rt)
	directiveProxy := newProxyHandler(cfg, rt.directiveRemotes, rt.programRuntime, rt.recoveryCompiler, rt.exchangeFactory, rt.bodyStore, rt.proxyTransport, runtimeMetrics)
	if !cfg.Proxy.Directive.SourceAccess.Enabled {
		return routeHTTPRequests(rt, health, metrics, directiveProxy, fallback)
	}
	var protectedDirective http.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		proxy.WriteProxyErrorJSON(w, http.StatusServiceUnavailable, "source_access_unavailable", "directive: source access unavailable")
	})
	if rt.sourceAccess != nil {
		protectedDirective = rt.sourceAccess.RequireAccess(directiveProxy)
	}
	return routeHTTPRequests(rt, health, metrics, protectedDirective, fallback)
}

func ensureRuntimeMetrics(rt *runtime) *runtimeMetrics {
	if rt.metrics != nil {
		return rt.metrics
	}
	rt.metrics = newRuntimeMetrics()
	if rt.bodyStore != nil {
		rt.bodyStore.RegisterMetrics(rt.metrics.MetricsSet())
	}
	if rt.eventOutput != nil {
		rt.eventOutput.RegisterMetrics(rt.metrics.MetricsSet())
	} else {
		rt.metrics.RegisterDisabledEventOutput()
	}
	return rt.metrics
}

func routeHTTPRequests(rt *runtime, health, metrics, directiveHandler, fallback http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case pathWithin(r.URL.Path, adminAPIPrefix), pathWithin(r.URL.Path, publicAPIPrefix):
			http.NotFound(w, r)
		case r.URL.Path == "/health":
			health.ServeHTTP(w, r)
		case r.URL.Path == "/metrics":
			metrics.ServeHTTP(w, r)
		case rt != nil && rt.adminAuth != nil && pathWithin(r.URL.Path, rt.adminAuth.PathPrefix()):
			fallback.ServeHTTP(w, r)
		case directive.MatchesRequest(r):
			directiveHandler.ServeHTTP(w, r)
		default:
			fallback.ServeHTTP(w, r)
		}
	})
}

func pathWithin(requestPath, prefix string) bool {
	return requestPath == prefix || strings.HasPrefix(requestPath, prefix+"/")
}

func newFallbackHTTPHandler(rt *runtime) http.Handler {
	mux := http.NewServeMux()
	if rt != nil && rt.adminAuth != nil {
		mux.Handle(rt.adminAuth.PathPrefix()+"/", noStore(rt.adminAuth.Handler()))
	}
	if webRoot := strings.TrimSpace(os.Getenv("WEB_ROOT")); webRoot != "" {
		mux.Handle("/", spaFileServer(webRoot))
	}
	return mux
}

func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func spaFileServer(root string) http.Handler {
	fs := http.Dir(root)
	fileServer := http.FileServer(fs)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}
		if fileExists(fs, r.URL.Path) {
			fileServer.ServeHTTP(w, r)
			return
		}
		fallback := r.Clone(r.Context())
		fallback.URL.Path = "/"
		fileServer.ServeHTTP(w, fallback)
	})
}

func fileExists(fs http.FileSystem, urlPath string) bool {
	name := strings.TrimPrefix(path.Clean(urlPath), "/")
	if name == "." {
		name = ""
	}
	file, err := fs.Open(name)
	if err != nil {
		return false
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	return err == nil && !info.IsDir()
}
