package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/config"
	"github.com/lwmacct/260628-llm-relay-dproxy/internal/handler"
)

const httpAPIPrefix = "/api"

func newControlHTTPServer(cfg *config.Config, rt *runtime) (*http.Server, error) {
	httpCfg := cfg.Server.HTTP
	return newHTTPServer(httpCfg.Listen, newControlHTTPHandler(cfg, rt), cfg, rt), nil
}

func newProxyHTTPServer(cfg *config.Config, rt *runtime) (*http.Server, error) {
	return newHTTPServer(cfg.Proxy.Listen, rt.proxy, cfg, rt), nil
}

func newHTTPServer(addr string, handler http.Handler, cfg *config.Config, rt *runtime) *http.Server {
	httpCfg := cfg.Server.HTTP
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       httpCfg.ReadTimeout,
		WriteTimeout:      httpCfg.WriteTimeout,
		IdleTimeout:       httpCfg.IdleTimeout,
	}

	if rt.tls == nil || rt.tls.config == nil {
		return srv
	}
	srv.TLSConfig = rt.tls.config
	return srv
}

func newControlHTTPHandler(cfg *config.Config, rt *runtime) http.Handler {
	mux := http.NewServeMux()
	api := handler.NewEndpoint(handler.Config{}, handler.Services{Exchanges: rt.recorder}).Handler()
	mux.Handle(httpAPIPrefix+"/", http.StripPrefix(httpAPIPrefix, limitRequestBody(api, cfg.Server.HTTP.MaxAPIBodyBytes)))
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":    "ok",
			"timestamp": time.Now().UTC(),
		})
	})
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
