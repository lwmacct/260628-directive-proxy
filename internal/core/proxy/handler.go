package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httputil"
)

type Handler struct {
	resolver Resolver
	proxy    *httputil.ReverseProxy
	idGen    Generator
}

type HandlerOptions struct {
	IDGenerator Generator
}

func NewHandler(resolver Resolver, transport http.RoundTripper, opts HandlerOptions) *Handler {
	if opts.IDGenerator == nil {
		opts.IDGenerator = NewGenerator()
	}
	proxy := &httputil.ReverseProxy{
		// Flush every write so SSE/NDJSON style responses are forwarded promptly.
		FlushInterval: -1,
		Rewrite: func(r *httputil.ProxyRequest) {
			d, _ := PlanFromContext(r.In.Context())
			applyRewrite(r, d)
			if d != nil && d.Proxy != nil {
				r.Out = withRequestProxy(r.Out, d.Proxy)
			}
		},
		ErrorHandler: handleProxyError,
		ErrorLog:     slog.NewLogLogger(slog.Default().Handler(), slog.LevelWarn),
		Transport:    transport,
	}
	return &Handler{
		resolver: resolver,
		proxy:    proxy,
		idGen:    opts.IDGenerator,
	}
}

func handleProxyError(w http.ResponseWriter, r *http.Request, err error) {
	if isRequestCanceled(r) {
		slog.Debug("proxy request canceled", "error", err, "path", requestPath(r))
		return
	}
	slog.Error("proxy error", "error", err, "path", requestPath(r))
	WriteProxyErrorJSON(w, http.StatusBadGateway, "upstream: request failed", requestIDFromRequest(r))
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
	r = h.ensureRequestID(w, r)

	d, err := h.resolver.Resolve(r)
	if err != nil {
		if errors.Is(err, ErrInvalidDirective) {
			WriteProxyErrorJSON(w, http.StatusBadRequest, "directive: invalid proxy directive payload", requestIDFromRequest(r))
			return
		}
		if errors.Is(err, ErrInvalidPlan) {
			WriteProxyErrorJSON(w, http.StatusBadRequest, "directive: missing directive token", requestIDFromRequest(r))
			return
		}
		slog.Error("resolve proxy plan failed", "error", err, "path", r.URL.Path)
		WriteProxyErrorJSON(w, http.StatusInternalServerError, "resolver: resolve proxy plan failed", requestIDFromRequest(r))
		return
	}
	if d == nil || d.Target == nil {
		WriteProxyErrorJSON(w, http.StatusInternalServerError, "resolver: resolve proxy plan failed", requestIDFromRequest(r))
		return
	}

	h.ServeHTTPWithPlan(w, r, d)
}

func (h *Handler) ServeHTTPWithPlan(w http.ResponseWriter, r *http.Request, d *Plan) {
	if h == nil || h.proxy == nil {
		http.NotFound(w, r)
		return
	}
	r = h.ensureRequestID(w, r)
	if d == nil || d.Target == nil {
		WriteProxyErrorJSON(w, http.StatusInternalServerError, "resolver: resolve proxy plan failed", requestIDFromRequest(r))
		return
	}
	ctx := ContextWithPlan(r.Context(), d)
	h.proxy.ServeHTTP(w, r.WithContext(ctx))
}

func (h *Handler) ensureRequestID(w http.ResponseWriter, r *http.Request) *http.Request {
	ctx, requestID := Ensure(r.Context(), h.idGen)
	w.Header().Set(ClientRequestIDHeader, requestID)
	if ctx == r.Context() {
		return r
	}
	return r.WithContext(ctx)
}

func requestIDFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	if requestID, ok := FromContext(r.Context()); ok {
		return requestID
	}
	return ""
}

func WriteInvalidDirectiveJSON(w http.ResponseWriter, requestID string) {
	WriteProxyErrorJSON(w, http.StatusBadRequest, "directive: invalid proxy directive payload", requestID)
}

func WriteProxyErrorJSON(w http.ResponseWriter, status int, message string, requestID string) {
	writeProxyErrorJSONBody(w, status, proxyErrorJSONBody(message, requestID))
}

func writeProxyErrorJSONBody(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Del("Location")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func proxyErrorJSONBody(message string, requestID string) []byte {
	body := map[string]string{"error": message}
	if requestID != "" {
		body["request_id"] = requestID
	}
	data, err := json.Marshal(body)
	if err != nil {
		return []byte("{}\n")
	}
	return append(data, '\n')
}
