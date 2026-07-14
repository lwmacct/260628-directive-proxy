package proxyrequestadapter

import (
	"crypto/sha256"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/capture"
	"github.com/lwmacct/260628-directive-proxy/internal/core/proxyrequest"
	"github.com/lwmacct/260628-directive-proxy/internal/core/requestmeta"
)

type ProxyRequestService struct {
	mu          sync.RWMutex
	active      map[string]*proxyRequestSession
	retryAfter  time.Duration
	maxAttempts int
	maxActive   int
	sink        capture.Sink
	policy      capturePolicy
	instanceID  string
	lastLogNano atomic.Int64
}

func NewProxyRequestService(opts ProxyRequestOptions, sink capture.Sink) *ProxyRequestService {
	if opts.RetryAfter < 0 {
		opts.RetryAfter = 0
	}
	if opts.MaxAttempts < 1 {
		opts.MaxAttempts = 1
	}
	if opts.MaxActiveRequests <= 0 {
		opts.MaxActiveRequests = 4096
	}
	if opts.BodyChunkBytes <= 0 {
		opts.BodyChunkBytes = 32 << 10
	}
	if opts.MaxSSEEventBytes <= 0 {
		opts.MaxSSEEventBytes = 1 << 20
	}
	if len(opts.RedactHeaders) == 0 {
		opts.RedactHeaders = []string{"authorization", "proxy-authorization", "cookie", "set-cookie", "x-api-key", "api-key"}
	}
	if len(opts.RedactQuery) == 0 {
		opts.RedactQuery = []string{"access_token", "api_key", "apikey", "key", "token"}
	}
	if sink == nil {
		sink = capture.DiscardSink{}
	}
	return &ProxyRequestService{
		active:      make(map[string]*proxyRequestSession),
		retryAfter:  opts.RetryAfter,
		maxAttempts: opts.MaxAttempts,
		maxActive:   opts.MaxActiveRequests,
		sink:        sink,
		instanceID:  strings.TrimSpace(opts.InstanceID),
		policy: capturePolicy{
			bodyChunkBytes:   opts.BodyChunkBytes,
			maxSSEEventBytes: opts.MaxSSEEventBytes,
			redactHeaders:    normalizePatterns(opts.RedactHeaders),
			redactQuery:      normalizePatterns(opts.RedactQuery),
		},
	}
}

func (s *ProxyRequestService) Start(req *http.Request) proxyrequest.Session {
	if s == nil || req == nil {
		return nil
	}
	now := time.Now().UTC()
	session := &proxyRequestSession{
		service:          s,
		ctx:              req.Context(),
		traceID:          newTraceID(),
		startedAt:        now,
		method:           req.Method,
		requestURL:       redactURL(requestURL(req), s.policy.redactQuery),
		responseBodyHash: sha256.New(),
	}
	session.emit("lifecycle", "request.started", 0, map[string]any{
		"method": session.method,
		"url":    session.requestURL,
		"host":   req.Host,
	})
	session.emit("request.headers", "request.headers", 0, map[string]any{
		"headers": redactHTTPHeaders(req.Header, s.policy.redactHeaders),
	})
	return session
}

func (s *ProxyRequestService) ListActive() []proxyrequest.ActiveRequest {
	if s == nil {
		return []proxyrequest.ActiveRequest{}
	}
	s.mu.RLock()
	items := make([]proxyrequest.ActiveRequest, 0, len(s.active))
	for _, session := range s.active {
		items = append(items, s.activeLocked(session))
	}
	s.mu.RUnlock()
	sort.Slice(items, func(i, j int) bool { return items[i].AttemptStartedAt.Before(items[j].AttemptStartedAt) })
	return items
}

func (s *ProxyRequestService) GetActive(traceID string) (proxyrequest.ActiveRequest, bool) {
	if s == nil {
		return proxyrequest.ActiveRequest{}, false
	}
	s.mu.RLock()
	session, ok := s.active[traceID]
	if !ok {
		s.mu.RUnlock()
		return proxyrequest.ActiveRequest{}, false
	}
	item := s.activeLocked(session)
	s.mu.RUnlock()
	return item, true
}

func (s *ProxyRequestService) RetryByTraceID(traceID string, expectedAttempt int, trigger proxyrequest.RetryTrigger) (proxyrequest.RetryResult, error) {
	if s == nil {
		return proxyrequest.RetryResult{}, proxyrequest.ErrNotFound
	}
	s.mu.Lock()
	session, ok := s.active[traceID]
	if !ok {
		s.mu.Unlock()
		return proxyrequest.RetryResult{}, proxyrequest.ErrNotFound
	}
	result, cancel, err := s.requestRetryLocked(session, expectedAttempt)
	s.mu.Unlock()
	if err != nil {
		return proxyrequest.RetryResult{}, err
	}
	session.emit("lifecycle", "retry.requested", expectedAttempt, map[string]any{
		"trigger":      string(trigger),
		"attempt":      expectedAttempt,
		"next_attempt": result.NextAttempt,
	})
	if cancel != nil {
		cancel()
	}
	return result, nil
}

func (s *ProxyRequestService) RetryByMetadata(selector requestmeta.Selector, expectedAttempt int, trigger proxyrequest.RetryTrigger) (proxyrequest.RetryResult, error) {
	if s == nil {
		return proxyrequest.RetryResult{}, proxyrequest.ErrNotFound
	}
	normalized, err := requestmeta.NormalizeSelector(selector)
	if err != nil {
		return proxyrequest.RetryResult{}, proxyrequest.ErrInvalidMetadata
	}
	s.mu.Lock()
	var matched *proxyRequestSession
	for _, session := range s.active {
		if !requestmeta.Matches(session.metadata, normalized) {
			continue
		}
		if matched != nil {
			s.mu.Unlock()
			return proxyrequest.RetryResult{}, proxyrequest.ErrAmbiguous
		}
		matched = session
	}
	if matched == nil {
		s.mu.Unlock()
		return proxyrequest.RetryResult{}, proxyrequest.ErrNotFound
	}
	result, cancel, retryErr := s.requestRetryLocked(matched, expectedAttempt)
	s.mu.Unlock()
	if retryErr != nil {
		return proxyrequest.RetryResult{}, retryErr
	}
	selectorMetadata := make(requestmeta.Metadata, len(normalized))
	for name, value := range normalized {
		selectorMetadata[name] = []string{value}
	}
	matched.emit("lifecycle", "retry.requested", expectedAttempt, map[string]any{
		"trigger":           string(trigger),
		"selector_metadata": redactMetadata(selectorMetadata, s.policy.redactHeaders),
		"attempt":           expectedAttempt,
		"next_attempt":      result.NextAttempt,
	})
	if cancel != nil {
		cancel()
	}
	return result, nil
}

func (s *ProxyRequestService) requestRetryLocked(session *proxyRequestSession, expectedAttempt int) (proxyrequest.RetryResult, func(), error) {
	if session.attempt != expectedAttempt {
		return proxyrequest.RetryResult{}, nil, proxyrequest.ErrAttemptChanged
	}
	if session.state == proxyrequest.StateRetryRequested {
		return proxyrequest.RetryResult{}, nil, proxyrequest.ErrRetryInProgress
	}
	if session.state != proxyrequest.StateAwaitingResponse {
		return proxyrequest.RetryResult{}, nil, proxyrequest.ErrRetryNotReady
	}
	if session.attempt >= s.maxAttempts {
		return proxyrequest.RetryResult{}, nil, proxyrequest.ErrMaxAttempts
	}
	if time.Now().Before(session.upstreamAt.Add(s.retryAfter)) {
		return proxyrequest.RetryResult{}, nil, proxyrequest.ErrRetryNotReady
	}
	session.state = proxyrequest.StateRetryRequested
	cancel := session.cancelAttempt
	attempt := session.attempt
	nextAttempt := attempt + 1
	item := s.activeLocked(session)
	return proxyrequest.RetryResult{Request: item, NextAttempt: nextAttempt}, cancel, nil
}

func (s *ProxyRequestService) activeLocked(session *proxyRequestSession) proxyrequest.ActiveRequest {
	retryableAt := time.Time{}
	if !session.upstreamAt.IsZero() {
		retryableAt = session.upstreamAt.Add(s.retryAfter)
	}
	return proxyrequest.ActiveRequest{
		TraceID:           session.traceID,
		Metadata:          requestmeta.Clone(session.metadata),
		State:             session.state,
		Method:            session.method,
		URL:               session.requestURL,
		TargetURL:         session.targetURL,
		StartedAt:         session.startedAt,
		Attempt:           session.attempt,
		AttemptStartedAt:  session.attemptAt,
		UpstreamStartedAt: session.upstreamAt,
		RetryableAt:       retryableAt,
		MaxAttempts:       s.maxAttempts,
	}
}

func (s *ProxyRequestService) logCaptureError(err error) {
	now := time.Now().UnixNano()
	previous := s.lastLogNano.Load()
	if previous != 0 && time.Duration(now-previous) < 10*time.Second {
		return
	}
	if s.lastLogNano.CompareAndSwap(previous, now) {
		slog.Warn("capture event delivery failed", "error", err)
	}
}
