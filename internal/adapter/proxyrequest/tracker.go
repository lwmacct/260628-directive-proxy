package proxyrequestadapter

import (
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/observability"
	"github.com/lwmacct/260628-directive-proxy/internal/core/proxyrequest"
	"github.com/lwmacct/260628-directive-proxy/internal/core/requestmeta"
)

type ProxyRequestService struct {
	mu          sync.RWMutex
	active      map[string]*proxyRequestSession
	retryAfter  time.Duration
	maxAttempts int
	maxActive   int
	pipeline    *observability.Pipeline
	instanceID  string
}

func NewProxyRequestService(opts ProxyRequestOptions, pipeline *observability.Pipeline) *ProxyRequestService {
	if opts.RetryAfter < 0 {
		opts.RetryAfter = 0
	}
	if opts.MaxAttempts < 1 {
		opts.MaxAttempts = 1
	}
	if opts.MaxActiveRequests <= 0 {
		opts.MaxActiveRequests = 4096
	}
	return &ProxyRequestService{
		active:      make(map[string]*proxyRequestSession),
		retryAfter:  opts.RetryAfter,
		maxAttempts: opts.MaxAttempts,
		maxActive:   opts.MaxActiveRequests,
		pipeline:    pipeline,
		instanceID:  strings.TrimSpace(opts.InstanceID),
	}
}

func (s *ProxyRequestService) Start(req *http.Request) proxyrequest.Session {
	if s == nil || req == nil {
		return nil
	}
	now := time.Now().UTC()
	session := &proxyRequestSession{
		service:    s,
		ctx:        req.Context(),
		traceID:    newTraceID(),
		startedAt:  now,
		method:     req.Method,
		requestURL: safeControlURL(requestURL(req)),
	}
	if s.pipeline != nil {
		session.trace = s.pipeline.StartTrace(observability.TraceContext{TraceID: session.traceID, InstanceID: s.instanceID})
	}
	session.observe(0, observability.RequestStarted{Method: session.method, URL: requestURL(req), Host: req.Host, Header: req.Header.Clone()})
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
	session.observe(expectedAttempt, observability.RetryRequested{Trigger: string(trigger), NextAttempt: result.NextAttempt})
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
	matched.observe(expectedAttempt, observability.RetryRequested{Trigger: string(trigger), NextAttempt: result.NextAttempt, SelectorMetadata: selectorMetadata})
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
