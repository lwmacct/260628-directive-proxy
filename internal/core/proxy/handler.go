package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
)

type Handler struct {
	resolver Resolver
	proxy    *httputil.ReverseProxy
	observer Observer
	next     http.Handler
}

type HandlerOptions struct {
	Observer Observer
	// Next receives requests for which Resolver returns ErrNoMatch.
	Next http.Handler
}

type Observer interface {
	Start(*http.Request) Observation
}

type Observation interface {
	WrapRequest(*http.Request) *http.Request
	WrapResponseWriter(http.ResponseWriter) http.ResponseWriter
	SetTargetURL(*url.URL)
	SetOutboundRequest(*http.Request)
	Finish()
}

type observationContextKey struct{}

func NewHandler(resolver Resolver, transport http.RoundTripper, opts HandlerOptions) *Handler {
	proxy := &httputil.ReverseProxy{
		// Flush every write so SSE/NDJSON style responses are forwarded promptly.
		FlushInterval: -1,
		Rewrite: func(r *httputil.ProxyRequest) {
			d, _ := PlanFromContext(r.In.Context())
			applyRewrite(r, d)
			if d != nil && d.Proxy != nil {
				r.Out = withRequestProxy(r.Out, d.Proxy)
			}
			if observation, ok := r.In.Context().Value(observationContextKey{}).(Observation); ok {
				observation.SetOutboundRequest(r.Out)
			}
		},
		ErrorHandler: handleProxyError,
		ErrorLog:     slog.NewLogLogger(slog.Default().Handler(), slog.LevelWarn),
		Transport:    transport,
	}
	return &Handler{
		resolver: resolver,
		proxy:    proxy,
		observer: opts.Observer,
		next:     opts.Next,
	}
}

func handleProxyError(w http.ResponseWriter, r *http.Request, err error) {
	if isRequestCanceled(r) {
		slog.Debug("proxy request canceled", "error", err, "path", requestPath(r))
		return
	}
	slog.Error("proxy error", "error", err, "path", requestPath(r))
	WriteProxyErrorJSON(w, http.StatusBadGateway, "upstream: request failed")
}

func isRequestCanceled(r *http.Request) bool {
	return r != nil && errors.Is(r.Context().Err(), context.Canceled)
}

func requestPath(r *http.Request) string {
	if r == nil || r.URL == nil {
		return ""
	}
	return r.URL.Path
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.proxy == nil || h.resolver == nil {
		http.NotFound(w, r)
		return
	}
	d, err := h.resolver.Resolve(r)
	if errors.Is(err, ErrNoMatch) {
		if h.next != nil {
			h.next.ServeHTTP(w, r)
			return
		}
		http.NotFound(w, r)
		return
	}

	var observation Observation
	if h.observer != nil {
		observation = h.observer.Start(r)
		if observation != nil {
			r = observation.WrapRequest(r)
			w = observation.WrapResponseWriter(w)
			defer observation.Finish()
		}
	}

	if err != nil {
		if errors.Is(err, ErrInvalidDirective) {
			WriteProxyErrorJSON(w, http.StatusBadRequest, "directive: invalid proxy directive payload")
			return
		}
		slog.Error("resolve proxy plan failed", "error", err, "path", r.URL.Path)
		WriteProxyErrorJSON(w, http.StatusInternalServerError, "resolver: resolve proxy plan failed")
		return
	}
	if d == nil || d.Target == nil {
		WriteProxyErrorJSON(w, http.StatusInternalServerError, "resolver: resolve proxy plan failed")
		return
	}
	if observation != nil {
		observation.SetTargetURL(BuildOutboundURL(d.Target, r.URL, d.JoinPath))
	}
	ctx := ContextWithPlan(r.Context(), d)
	if observation != nil {
		ctx = context.WithValue(ctx, observationContextKey{}, observation)
	}
	h.proxy.ServeHTTP(w, r.WithContext(ctx))
}
func WriteProxyErrorJSON(w http.ResponseWriter, status int, message string) {
	writeProxyErrorJSONBody(w, status, proxyErrorJSONBody(message))
}

func writeProxyErrorJSONBody(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Del("Location")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func proxyErrorJSONBody(message string) []byte {
	body := map[string]string{"error": message}
	data, err := json.Marshal(body)
	if err != nil {
		return []byte("{}\n")
	}
	return append(data, '\n')
}
