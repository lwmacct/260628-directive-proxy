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
	"github.com/lwmacct/260628-directive-proxy/internal/handler"
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
		ReadTimeout:       httpCfg.ReadTimeout,
		WriteTimeout:      httpCfg.WriteTimeout,
		IdleTimeout:       httpCfg.IdleTimeout,
		MaxHeaderBytes:    httpCfg.MaxHeaderBytes,
	}

	if rt.tls == nil || rt.tls.config == nil {
		return srv
	}
	srv.TLSConfig = rt.tls.config
	return srv
}

func newHTTPHandler(cfg *config.Server, rt *runtime) http.Handler {
	services := handler.Services{ExchangeQuery: rt.exchanges, ExchangeCommands: rt.exchanges, Modules: rt.moduleRuntime, EventOutput: rt.eventOutput}
	publicAPI := limitRequestBody(handler.NewPublicEndpoint(services).Handler(), cfg.HTTP.MaxAPIBodyBytes)
	adminAPI := limitRequestBody(handler.NewAdminEndpoint(services).Handler(), cfg.HTTP.MaxAPIBodyBytes)
	if rt.adminAuth != nil {
		adminAPI = rt.adminAuth.RequireAccess(adminAPI)
	}
	fallback := newFallbackHTTPHandler(rt, handler.NewSystemEndpoint(services).Handler())
	directiveProxy := newProxyHandler(cfg, rt.directiveReader, rt.exchanges, rt.bodyMemory, rt.proxyTransport)
	if !cfg.Proxy.Directive.SourceAccess.Enabled {
		return routeHTTPRequests(publicAPI, adminAPI, directiveProxy, fallback)
	}
	var protectedDirective http.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		proxy.WriteProxyErrorJSON(w, http.StatusServiceUnavailable, "source_access_unavailable", "directive: source access unavailable")
	})
	if rt.sourceAccess != nil {
		protectedDirective = rt.sourceAccess.RequireAccess(directiveProxy)
	}
	return routeHTTPRequests(publicAPI, adminAPI, protectedDirective, fallback)
}

func routeHTTPRequests(publicAPI, adminAPI, directiveHandler, fallback http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case pathWithin(r.URL.Path, publicAPIPrefix):
			publicAPI.ServeHTTP(w, r)
		case pathWithin(r.URL.Path, adminAPIPrefix):
			adminAPI.ServeHTTP(w, r)
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

func newFallbackHTTPHandler(rt *runtime, systemAPI http.Handler) http.Handler {
	mux := http.NewServeMux()
	if rt.adminAuth != nil {
		mux.Handle(rt.adminAuth.PathPrefix()+"/", rt.adminAuth.Handler())
	}
	mux.Handle("/health", systemAPI)
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

func limitRequestBody(next http.Handler, maxBytes int64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if maxBytes > 0 && shouldLimitRequestBody(r) {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
		}
		next.ServeHTTP(w, r)
	})
}

func shouldLimitRequestBody(r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Body == nil || r.Body == http.NoBody {
		return false
	}
	for _, value := range r.Header.Values("Upgrade") {
		if strings.EqualFold(strings.TrimSpace(value), "websocket") {
			return false
		}
	}
	return true
}
