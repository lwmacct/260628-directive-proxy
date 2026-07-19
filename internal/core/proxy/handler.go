package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/bodystore"
	"github.com/lwmacct/260628-directive-proxy/internal/core/exchange"
)

type exchangeStarter interface {
	Start(*http.Request) *exchange.Exchange
}

type Handler struct {
	resolver           Resolver
	proxy              *httputil.ReverseProxy
	exchangeFactory    exchangeStarter
	trackBeforeResolve bool
	bodyStore          *bodystore.Controller
	bodyReadTimeout    time.Duration
	bodyMaxBytes       int64
	bodyQueueWait      time.Duration
	bodyChunkBytes     int
	next               http.Handler
}

type HandlerOptions struct {
	ExchangeFactory    exchangeStarter
	TrackBeforeResolve bool
	BodyStore          *bodystore.Controller
	BodyReadTimeout    time.Duration
	BodyMaxBytes       int64
	BodyQueueWait      time.Duration
	BodyChunkBytes     int
	// Next receives requests for which Resolver returns ErrNoMatch.
	Next http.Handler
}

func NewHandler(resolver Resolver, transport http.RoundTripper, opts HandlerOptions) *Handler {
	if _, ok := transport.(interface{ orchestratesPreparedRequests() }); !ok {
		wrapped, err := NewRecoveryTransport(transport, RecoveryTransportOptions{})
		if err == nil {
			transport = wrapped
		}
	}
	proxy := &httputil.ReverseProxy{
		// Flush every write so SSE/NDJSON style responses are forwarded promptly.
		FlushInterval: -1,
		// RecoveryTransport rebuilds every outbound round trip from the immutable
		// inbound template after resolving the directive once.
		Rewrite:        func(*httputil.ProxyRequest) {},
		ModifyResponse: modifyResponse,
		ErrorHandler:   handleProxyError,
		ErrorLog:       slog.NewLogLogger(slog.Default().Handler(), slog.LevelWarn),
		Transport:      transport,
	}
	return &Handler{
		resolver:           resolver,
		proxy:              proxy,
		exchangeFactory:    opts.ExchangeFactory,
		trackBeforeResolve: opts.TrackBeforeResolve,
		bodyStore:          opts.BodyStore,
		bodyReadTimeout:    opts.BodyReadTimeout,
		bodyMaxBytes:       opts.BodyMaxBytes,
		bodyQueueWait:      opts.BodyQueueWait,
		bodyChunkBytes:     opts.BodyChunkBytes,
		next:               opts.Next,
	}
}

func handleProxyError(w http.ResponseWriter, r *http.Request, err error) {
	if isRequestCanceled(r) {
		slog.Debug("proxy request canceled", "error", err, "path", requestPath(r))
		return
	}
	if errors.Is(err, bodystore.ErrBodyTooLarge) {
		WriteProxyErrorJSON(w, http.StatusRequestEntityTooLarge, "request_body_too_large", "proxy: request body exceeds replay store limit")
		return
	}
	if errors.Is(err, bodystore.ErrStoreCapacity) {
		w.Header().Set("Retry-After", "1")
		WriteProxyErrorJSON(w, http.StatusServiceUnavailable, "body_store_capacity", "proxy: request body replay store capacity is exhausted")
		return
	}
	if errors.Is(err, bodystore.ErrQueueFull) {
		w.Header().Set("Retry-After", "1")
		WriteProxyErrorJSON(w, http.StatusServiceUnavailable, "body_store_queue_full", "proxy: request admission queue is full")
		return
	}
	if errors.Is(err, bodystore.ErrQueueTimeout) {
		w.Header().Set("Retry-After", "1")
		WriteProxyErrorJSON(w, http.StatusServiceUnavailable, "body_store_queue_timeout", "proxy: request admission queue wait timed out")
		return
	}
	if errors.Is(err, ErrBodyStoreUnavailable) {
		WriteProxyErrorJSON(w, http.StatusServiceUnavailable, "body_store_unavailable", "proxy: request body replay store is unavailable")
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
	if errors.Is(err, ErrRecoveryFailed) {
		WriteProxyErrorJSON(w, http.StatusBadGateway, "recovery_failed", "recovery: controller failed the upstream round trip")
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
	var current *exchange.Exchange
	if h.trackBeforeResolve && h.exchangeFactory != nil {
		current = h.exchangeFactory.Start(r)
		if current != nil {
			r = r.WithContext(exchange.ContextWithExchange(r.Context(), current))
			w = current.WrapResponseWriter(w)
			defer current.Complete()
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
	if current == nil && h.exchangeFactory != nil {
		current = h.exchangeFactory.Start(r)
		if current != nil {
			r = r.WithContext(exchange.ContextWithExchange(r.Context(), current))
			w = current.WrapResponseWriter(w)
			defer current.Complete()
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
	if prepared == nil {
		slog.Error("resolver returned nil prepared directive", "path", r.URL.Path)
		WriteProxyErrorJSON(w, http.StatusInternalServerError, "resolver_failed", "resolver: resolve proxy plan failed")
		return
	}
	plan := prepared.Plan()
	if plan == nil || plan.Target == nil {
		slog.Error("prepared directive has no target", "path", r.URL.Path)
		WriteProxyErrorJSON(w, http.StatusInternalServerError, "resolver_failed", "resolver: resolve proxy plan failed")
		return
	}
	source := prepared.Source()
	if current != nil {
		if configureErr := current.Configure(exchange.Configuration{
			Directive: exchange.DirectiveInfo{
				Mode: source.Mode, Backend: source.Backend, Endpoint: source.Endpoint, Resource: source.Resource,
				PayloadSHA256: source.PayloadSHA256, Duration: source.Duration, Target: plan.Target,
			},
			Metadata: prepared.Metadata(), Program: prepared.Program(),
		}); configureErr != nil {
			moduleErr := error(ErrInvalidDirective)
			if source.Mode == "remote" {
				moduleErr = ErrRemoteDirectiveInvalid
			}
			writeDirectiveError(w, moduleErr)
			return
		}
	}
	body, bodyErr := h.startRequestBody(w, r, current, prepared.BodyPolicy())
	if bodyErr != nil {
		handleProxyError(w, r, bodyErr)
		return
	}
	defer func() { _ = body.Close() }()
	template := NewRequestTemplate(r)
	ctx := contextWithPreparedRequest(r.Context(), prepared, template, body)
	h.proxy.ServeHTTP(w, r.WithContext(ctx))
}

func (h *Handler) startRequestBody(w http.ResponseWriter, r *http.Request, current *exchange.Exchange, policy *BodyPolicy) (*bodystore.Store, error) {
	observer := bodystore.Observer{}
	if current != nil {
		observer.Chunk = func(_ int64, data []byte) error {
			if err := current.RequestBodyChunk(data); err != nil {
				return fmt.Errorf("%w: request body chunk: %v", ErrModuleFailed, err)
			}
			return nil
		}
		observer.End = func(result bodystore.Result) {
			current.RequestBodyEnd(result.Total, result.SHA256, result.Complete)
		}
	}
	if r == nil || r.Body == nil || r.Body == http.NoBody {
		return bodystore.Empty(observer), nil
	}
	if h.bodyStore == nil {
		return nil, ErrBodyStoreUnavailable
	}
	maxBodyBytes, queueWait, readTimeout, chunkBytes := h.bodyMaxBytes, h.bodyQueueWait, h.bodyReadTimeout, h.bodyChunkBytes
	if policy != nil {
		if policy.MaxBodyBytes >= 0 {
			maxBodyBytes = policy.MaxBodyBytes
		}
		if policy.QueueWait >= 0 {
			queueWait = policy.QueueWait
		}
		if policy.ReadTimeout >= 0 {
			readTimeout = policy.ReadTimeout
		}
		if policy.ChunkBytes >= 0 {
			chunkBytes = policy.ChunkBytes
		}
	}
	reservation, err := h.bodyStore.Admit(r.Context(), r.ContentLength, maxBodyBytes, queueWait, chunkBytes)
	if err != nil {
		return nil, err
	}
	// Start the read timeout only after admission; install it before launching
	// ingestion so the first blocked read is covered as well.
	if readTimeout > 0 {
		_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(readTimeout))
	}
	if current != nil {
		current.BeginBodyStream()
	}
	body, err := h.bodyStore.StreamWithReservation(r.Context(), r.Body, r.ContentLength, observer, bodystore.StreamOptions{MaxBodyBytes: maxBodyBytes, QueueWait: queueWait, ChunkBytes: chunkBytes}, reservation)
	if err != nil {
		reservation.Close()
		return nil, err
	}
	r.Body = http.NoBody
	return body, nil
}

func writeDirectiveError(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, ErrInvalidDirective):
		WriteProxyErrorJSON(w, http.StatusBadRequest, "invalid_directive", "directive: invalid proxy directive payload")
	case errors.Is(err, ErrDirectiveUnauthorized):
		w.Header().Set("WWW-Authenticate", "Bearer")
		WriteProxyErrorJSON(w, http.StatusUnauthorized, "directive_unauthorized", "directive: token authentication failed")
	case errors.Is(err, ErrDirectiveTokenTooLarge):
		WriteProxyErrorJSON(w, http.StatusRequestHeaderFieldsTooLarge, "directive_token_too_large", "directive: token is too large")
	case errors.Is(err, ErrDirectiveNotFound):
		WriteProxyErrorJSON(w, http.StatusNotFound, "directive_not_found", "directive: reference not found")
	case errors.Is(err, ErrRemoteDirectiveUnavailable):
		WriteProxyErrorJSON(w, http.StatusServiceUnavailable, "remote_unavailable", "directive: remote resolver unavailable")
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
