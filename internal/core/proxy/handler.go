package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httputil"

	"github.com/lwmacct/260628-directive-proxy/internal/core/proxyrequest"
)

type Handler struct {
	resolver           Resolver
	proxy              *httputil.ReverseProxy
	tracker            proxyrequest.Tracker
	trackBeforeResolve bool
	next               http.Handler
}

type HandlerOptions struct {
	Tracker            proxyrequest.Tracker
	TrackBeforeResolve bool
	// Next receives requests for which Resolver returns ErrNoMatch.
	Next http.Handler
}

func NewHandler(resolver Resolver, transport http.RoundTripper, opts HandlerOptions) *Handler {
	if _, ok := transport.(interface{ orchestratesPreparedRequests() }); !ok {
		wrapped, err := NewRetryTransport(transport, RetryTransportOptions{})
		if err == nil {
			transport = wrapped
		}
	}
	proxy := &httputil.ReverseProxy{
		// Flush every write so SSE/NDJSON style responses are forwarded promptly.
		FlushInterval: -1,
		// RetryTransport rebuilds every outbound attempt from the immutable
		// inbound template after resolving that attempt's directive.
		Rewrite:      func(*httputil.ProxyRequest) {},
		ErrorHandler: handleProxyError,
		ErrorLog:     slog.NewLogLogger(slog.Default().Handler(), slog.LevelWarn),
		Transport:    transport,
	}
	return &Handler{
		resolver:           resolver,
		proxy:              proxy,
		tracker:            opts.Tracker,
		trackBeforeResolve: opts.TrackBeforeResolve,
		next:               opts.Next,
	}
}

func handleProxyError(w http.ResponseWriter, r *http.Request, err error) {
	if isRequestCanceled(r) {
		slog.Debug("proxy request canceled", "error", err, "path", requestPath(r))
		return
	}
	if errors.Is(err, ErrReplayBodyTooLarge) {
		WriteProxyErrorJSON(w, http.StatusRequestEntityTooLarge, "request_body_too_large", "proxy: request body exceeds retry replay limit")
		return
	}
	if errors.Is(err, ErrReplayBudgetFull) {
		WriteProxyErrorJSON(w, http.StatusServiceUnavailable, "retry_capacity_unavailable", "proxy: retry replay capacity is unavailable")
		return
	}
	if errors.Is(err, ErrActiveCapacity) {
		WriteProxyErrorJSON(w, http.StatusServiceUnavailable, "active_request_capacity_unavailable", "proxy: active request capacity is unavailable")
		return
	}
	if errors.Is(err, ErrResolverFailed) {
		WriteProxyErrorJSON(w, http.StatusInternalServerError, "resolver_failed", "resolver: resolve proxy plan failed")
		return
	}
	if writeDirectiveError(w, err) {
		return
	}
	slog.Error("proxy error", "error", err, "path", requestPath(r))
	WriteProxyErrorJSON(w, http.StatusBadGateway, "upstream_request_failed", "upstream: request failed")
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
	var session proxyrequest.Session
	if h.trackBeforeResolve && h.tracker != nil {
		session = h.tracker.Start(r)
		if session != nil {
			r = r.WithContext(proxyrequest.ContextWithSession(r.Context(), session))
			w = session.WrapResponseWriter(w)
			defer session.Complete()
		}
	}
	prepared, err := h.resolver.Prepare(r)
	if errors.Is(err, ErrNoMatch) {
		if h.next != nil {
			h.next.ServeHTTP(w, r)
			return
		}
		http.NotFound(w, r)
		return
	}
	if session == nil && h.tracker != nil {
		session = h.tracker.Start(r)
		if session != nil {
			r = r.WithContext(proxyrequest.ContextWithSession(r.Context(), session))
			w = session.WrapResponseWriter(w)
			defer session.Complete()
		}
	}

	if err != nil {
		if isRequestCanceled(r) {
			return
		}
		if writeDirectiveError(w, err) {
			return
		}
		slog.Error("resolve proxy plan failed", "error", err, "path", r.URL.Path)
		WriteProxyErrorJSON(w, http.StatusInternalServerError, "resolver_failed", "resolver: resolve proxy plan failed")
		return
	}
	template := NewRequestTemplate(r)
	ctx := contextWithPreparedRequest(r.Context(), prepared, template)
	h.proxy.ServeHTTP(w, r.WithContext(ctx))
}

func writeDirectiveError(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, ErrInvalidDirective):
		WriteProxyErrorJSON(w, http.StatusBadRequest, "invalid_directive", "directive: invalid proxy directive payload")
	case errors.Is(err, ErrDirectiveTokenTooLarge):
		WriteProxyErrorJSON(w, http.StatusRequestHeaderFieldsTooLarge, "directive_token_too_large", "directive: token is too large")
	case errors.Is(err, ErrDirectiveNotFound):
		WriteProxyErrorJSON(w, http.StatusNotFound, "directive_not_found", "directive: reference not found")
	case errors.Is(err, ErrRemoteDirectiveUnavailable):
		WriteProxyErrorJSON(w, http.StatusServiceUnavailable, "remote_unavailable", "directive: remote resolver unavailable")
	case errors.Is(err, ErrDirectiveMetadataTooLarge):
		WriteProxyErrorJSON(w, http.StatusRequestHeaderFieldsTooLarge, "request_metadata_too_large", "directive: request metadata is too large")
	case errors.Is(err, ErrRemoteDirectiveInvalid):
		WriteProxyErrorJSON(w, http.StatusBadGateway, "remote_response_invalid", "directive: remote payload is invalid")
	default:
		return false
	}
	return true
}
func WriteProxyErrorJSON(w http.ResponseWriter, status int, code, message string) {
	writeProxyErrorJSONBody(w, status, proxyErrorJSONBody(code, message))
}

func writeProxyErrorJSONBody(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Del("Location")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func proxyErrorJSONBody(code, message string) []byte {
	body := struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}{}
	body.Error.Code = code
	body.Error.Message = message
	data, err := json.Marshal(body)
	if err != nil {
		return []byte("{}\n")
	}
	return append(data, '\n')
}
