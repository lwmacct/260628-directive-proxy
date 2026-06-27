package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/config"
	"github.com/lwmacct/260628-llm-relay-dproxy/internal/handler"
)

const httpAPIPrefix = "/api"

func newHTTPServer(cfg *config.Config, rt *runtime) (*http.Server, error) {
	httpCfg := cfg.Server.HTTP
	srv := &http.Server{
		Addr:              httpCfg.Listen,
		Handler:           newHTTPHandler(cfg, rt),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       httpCfg.ReadTimeout,
		WriteTimeout:      httpCfg.WriteTimeout,
		IdleTimeout:       httpCfg.IdleTimeout,
	}

	if rt.tls == nil || rt.tls.config == nil {
		return srv, nil
	}
	srv.TLSConfig = rt.tls.config
	return srv, nil
}

func newHTTPHandler(cfg *config.Config, rt *runtime) http.Handler {
	mux := http.NewServeMux()
	api := handler.NewEndpoint(handler.Config{}, handler.Services{}).Handler()
	mux.Handle(httpAPIPrefix+"/", http.StripPrefix(httpAPIPrefix, limitRequestBody(api, cfg.Server.HTTP.MaxAPIBodyBytes)))
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":    "ok",
			"timestamp": time.Now().UTC(),
		})
	})

	proxyPrefix := cfg.Proxy.PathPrefix
	proxyHandler := rt.proxy
	if proxyPrefix == "/" {
		mux.Handle("/", proxyHandler)
		return mux
	}
	mux.Handle(proxyPrefix+"/", http.StripPrefix(proxyPrefix, proxyHandler))
	if proxyPrefix != "/" {
		mux.Handle(proxyPrefix, http.RedirectHandler(proxyPrefix+"/", http.StatusTemporaryRedirect))
	}
	return mux
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
