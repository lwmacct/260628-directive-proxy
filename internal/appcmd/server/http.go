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
	runtimeMetrics := ensureRuntimeMetrics(rt, cfg.Metrics.Prefix)
	health := newHealthHandler(rt.programRuntime, rt.eventOutput, rt.bodyStore)
	metrics := &metricsHandler{set: runtimeMetrics.MetricsSet()}
	fallback := newFallbackHTTPHandler()
	directiveProxy := newProxyHandler(cfg, rt.directiveRemotes, rt.programRuntime, rt.recoveryCompiler, rt.exchangeFactory, rt.bodyStore, rt.proxyTransport, runtimeMetrics)
	if !cfg.Proxy.Directive.SourceAccess.Enabled {
		return routeHTTPRequests(health, metrics, directiveProxy, fallback)
	}
	var protectedDirective http.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		proxy.WriteProxyErrorJSON(w, http.StatusServiceUnavailable, "source_access_unavailable", "directive: source access unavailable")
	})
	if rt.sourceAccess != nil {
		protectedDirective = rt.sourceAccess.RequireAccess(directiveProxy)
	}
	return routeHTTPRequests(health, metrics, protectedDirective, fallback)
}

func ensureRuntimeMetrics(rt *runtime, prefix string) *runtimeMetrics {
	if rt.metrics != nil {
		return rt.metrics
	}
	rt.metrics = newRuntimeMetrics(prefix)
	if rt.bodyStore != nil {
		rt.bodyStore.RegisterMetrics(rt.metrics.MetricsSet(), rt.metrics.Prefix())
	}
	if rt.eventOutput != nil {
		rt.eventOutput.RegisterMetrics(rt.metrics.MetricsSet(), rt.metrics.Prefix())
	} else {
		rt.metrics.RegisterDisabledEventOutput()
	}
	return rt.metrics
}

func routeHTTPRequests(health, metrics, directiveHandler, fallback http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/health":
			health.ServeHTTP(w, r)
		case r.URL.Path == "/metrics":
			metrics.ServeHTTP(w, r)
		case directive.MatchesRequest(r):
			directiveHandler.ServeHTTP(w, r)
		default:
			fallback.ServeHTTP(w, r)
		}
	})
}

func newFallbackHTTPHandler() http.Handler {
	mux := http.NewServeMux()
	if webRoot := strings.TrimSpace(os.Getenv("WEB_ROOT")); webRoot != "" {
		mux.Handle("/", spaFileServer(webRoot))
	}
	return mux
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
