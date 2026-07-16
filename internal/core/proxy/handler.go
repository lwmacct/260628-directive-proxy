package proxy

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/bodymemory"
	"github.com/lwmacct/260628-directive-proxy/internal/core/proxyrequest"
)

type Handler struct {
	resolver           Resolver
	proxy              *httputil.ReverseProxy
	tracker            proxyrequest.Tracker
	trackBeforeResolve bool
	bodyMemory         *bodymemory.Controller
	bodyReadTimeout    time.Duration
	next               http.Handler
}

type HandlerOptions struct {
	Tracker            proxyrequest.Tracker
	TrackBeforeResolve bool
	BodyMemory         *bodymemory.Controller
	BodyReadTimeout    time.Duration
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
		Rewrite:        func(*httputil.ProxyRequest) {},
		ModifyResponse: modifyResponse,
		ErrorHandler:   handleProxyError,
		ErrorLog:       slog.NewLogLogger(slog.Default().Handler(), slog.LevelWarn),
		Transport:      transport,
	}
	return &Handler{
		resolver:           resolver,
		proxy:              proxy,
		tracker:            opts.Tracker,
		trackBeforeResolve: opts.TrackBeforeResolve,
		bodyMemory:         opts.BodyMemory,
		bodyReadTimeout:    opts.BodyReadTimeout,
		next:               opts.Next,
	}
}

func handleProxyError(w http.ResponseWriter, r *http.Request, err error) {
	if isRequestCanceled(r) {
		slog.Debug("proxy request canceled", "error", err, "path", requestPath(r))
		return
	}
	if errors.Is(err, bodymemory.ErrBodyTooLarge) {
		WriteProxyErrorJSON(w, http.StatusRequestEntityTooLarge, "request_body_too_large", "proxy: request body exceeds memory limit")
		return
	}
	if errors.Is(err, bodymemory.ErrQueueFull) {
		w.Header().Set("Retry-After", "1")
		WriteProxyErrorJSON(w, http.StatusServiceUnavailable, "body_memory_queue_full", "proxy: request body memory queue is full")
		return
	}
	if errors.Is(err, bodymemory.ErrWaitTimeout) {
		w.Header().Set("Retry-After", "1")
		WriteProxyErrorJSON(w, http.StatusServiceUnavailable, "body_memory_wait_timeout", "proxy: request body memory wait timed out")
		return
	}
	if errors.Is(err, ErrContentLengthRequired) {
		WriteProxyErrorJSON(w, http.StatusLengthRequired, "content_length_required", "proxy: Content-Length is required for request bodies")
		return
	}
	if errors.Is(err, ErrBodyMemoryUnavailable) {
		WriteProxyErrorJSON(w, http.StatusServiceUnavailable, "body_memory_unavailable", "proxy: request body memory is unavailable")
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
	if errors.Is(err, ErrModuleFailed) {
		WriteProxyErrorJSON(w, http.StatusInternalServerError, "module_failed", "module: request lifecycle execution failed")
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
	identity, identityErr := proxyrequest.TakeIdentity(r)
	if identityErr != nil {
		WriteProxyErrorJSON(w, http.StatusBadRequest, "invalid_retry_identity", "proxy: Dproxy-Retry-ID must be a canonical UUIDv7")
		return
	}
	var session proxyrequest.Session
	if h.trackBeforeResolve && h.tracker != nil {
		session = h.tracker.Start(r, identity)
		if session == nil && identity.Valid() {
			WriteProxyErrorJSON(w, http.StatusConflict, "duplicate_retry_id", "proxy: retry ID is already active")
			return
		}
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
		session = h.tracker.Start(r, identity)
		if session == nil && identity.Valid() {
			WriteProxyErrorJSON(w, http.StatusConflict, "duplicate_retry_id", "proxy: retry ID is already active")
			return
		}
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
	if session != nil {
		if configureErr := session.ConfigureRequest(prepared.RequestProgram()); configureErr != nil {
			moduleErr := error(ErrInvalidDirective)
			if prepared.Kind() == "remote" {
				moduleErr = ErrRemoteDirectiveInvalid
			}
			writeDirectiveError(w, moduleErr)
			return
		}
	}
	body, bodyErr := h.readRequestBody(w, r, session)
	if bodyErr != nil {
		handleProxyError(w, r, bodyErr)
		return
	}
	defer body.Release()
	template := NewRequestTemplate(r)
	ctx := contextWithPreparedRequest(r.Context(), prepared, template, body)
	h.proxy.ServeHTTP(w, r.WithContext(ctx))
}

func (h *Handler) readRequestBody(w http.ResponseWriter, r *http.Request, session proxyrequest.Session) (*bodymemory.Body, error) {
	if r == nil || r.Body == nil || r.Body == http.NoBody {
		if session != nil {
			digest := sha256.Sum256(nil)
			session.RequestBodyEnd(0, fmt.Sprintf("%x", digest), true)
		}
		return bodymemory.NewBody(nil, nil), nil
	}
	if r.ContentLength < 0 {
		return nil, ErrContentLengthRequired
	}
	if h.bodyMemory == nil {
		return nil, ErrBodyMemoryUnavailable
	}
	reservation, err := h.bodyMemory.Reserve(r.Context(), r.ContentLength)
	if err != nil {
		return nil, err
	}
	completed := false
	defer func() {
		if !completed {
			reservation.Close()
		}
	}()
	if h.bodyReadTimeout > 0 {
		_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(h.bodyReadTimeout))
	}
	if session != nil {
		session.BeginBodyRead()
	}
	data := make([]byte, int(r.ContentLength))
	if _, err = io.ReadFull(r.Body, data); err != nil {
		if session != nil {
			session.RequestBodyEnd(0, "", false)
		}
		return nil, fmt.Errorf("read request body: %w", err)
	}
	_ = r.Body.Close()
	r.Body = http.NoBody
	body := bodymemory.NewBody(data, reservation)
	completed = true
	if session != nil {
		session.RequestBodyAvailable(body)
		session.RequestBodyEnd(int64(len(data)), fmt.Sprintf("%x", body.Digest()), true)
	}
	return body, nil
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
