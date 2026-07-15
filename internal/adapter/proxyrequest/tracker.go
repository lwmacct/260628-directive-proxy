package proxyrequestadapter

import (
	"crypto/subtle"
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
	mu                sync.RWMutex
	active            map[string]*proxyRequestSession
	byRetryID         map[[32]byte]*proxyRequestSession
	terminalByTrace   map[string]retryTombstone
	terminalByRetryID map[[32]byte]retryTombstone
	maxAttempts       int
	commandRetention  time.Duration
	pipeline          *observability.Pipeline
}

type retryTombstone struct {
	digest  [32]byte
	results map[int]proxyrequest.RetryResult
	expires time.Time
}

func NewProxyRequestService(opts ProxyRequestOptions, pipeline *observability.Pipeline) *ProxyRequestService {
	if opts.MaxAttempts < 1 {
		opts.MaxAttempts = 1
	}
	if opts.CommandRetention <= 0 {
		opts.CommandRetention = time.Minute
	}
	return &ProxyRequestService{
		active:            make(map[string]*proxyRequestSession),
		byRetryID:         make(map[[32]byte]*proxyRequestSession),
		terminalByTrace:   make(map[string]retryTombstone),
		terminalByRetryID: make(map[[32]byte]retryTombstone),
		maxAttempts:       opts.MaxAttempts,
		commandRetention:  opts.CommandRetention,
		pipeline:          pipeline,
	}
}

func (s *ProxyRequestService) Start(req *http.Request, identity proxyrequest.Identity) proxyrequest.Session {
	if s == nil || req == nil {
		return nil
	}
	now := time.Now().UTC()
	session := &proxyRequestSession{
		service:        s,
		ctx:            req.Context(),
		traceID:        newTraceID(),
		identity:       identity,
		startedAt:      now,
		method:         req.Method,
		idempotencyKey: strings.TrimSpace(req.Header.Get("Idempotency-Key")),
		requestURL:     redactURL(requestURL(req)),
		state:          proxyrequest.StateWaitingBodyMemory,
		retryResults:   make(map[int]proxyrequest.RetryResult),
		events:         make(chan coordinatorEvent),
		done:           make(chan struct{}),
		requestStarted: observability.RequestStarted{Method: req.Method, URL: requestURL(req), Host: req.Host, Header: req.Header.Clone()},
	}
	s.mu.Lock()
	s.pruneTerminalLocked(now)
	if identity.Valid() {
		_, terminalExists := s.terminalByRetryID[identity.Digest()]
		if _, exists := s.byRetryID[identity.Digest()]; exists || terminalExists {
			s.mu.Unlock()
			return nil
		}
		s.byRetryID[identity.Digest()] = session
	}
	s.active[session.traceID] = session
	s.mu.Unlock()
	go session.run()
	if s.pipeline != nil {
		session.trace = s.pipeline.StartRequestTrace(observability.TraceContext{TraceID: session.traceID})
	}
	return session
}

func (s *ProxyRequestService) ListActive() []proxyrequest.ActiveRequest {
	if s == nil {
		return []proxyrequest.ActiveRequest{}
	}
	s.mu.RLock()
	sessions := make([]*proxyRequestSession, 0, len(s.active))
	for _, session := range s.active {
		sessions = append(sessions, session)
	}
	s.mu.RUnlock()
	items := make([]proxyrequest.ActiveRequest, 0, len(sessions))
	for _, session := range sessions {
		if item, ok := session.snapshot(); ok {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		left := items[i].AttemptStartedAt
		if left.IsZero() {
			left = items[i].StartedAt
		}
		right := items[j].AttemptStartedAt
		if right.IsZero() {
			right = items[j].StartedAt
		}
		return left.Before(right)
	})
	return items
}

func (s *ProxyRequestService) GetActive(traceID string) (proxyrequest.ActiveRequest, bool) {
	if s == nil {
		return proxyrequest.ActiveRequest{}, false
	}
	s.mu.RLock()
	session, ok := s.active[traceID]
	s.mu.RUnlock()
	if !ok {
		return proxyrequest.ActiveRequest{}, false
	}
	return session.snapshot()
}

func (s *ProxyRequestService) RetryByTraceID(traceID string, expectedAttempt int, trigger proxyrequest.RetryTrigger) (proxyrequest.RetryResult, error) {
	if s == nil {
		return proxyrequest.RetryResult{}, proxyrequest.ErrNotFound
	}
	s.mu.RLock()
	session, ok := s.active[traceID]
	if !ok {
		if tombstone, exists := s.terminalByTrace[traceID]; exists && time.Now().Before(tombstone.expires) {
			result, found := tombstone.results[expectedAttempt]
			s.mu.RUnlock()
			if found {
				return result, nil
			}
			return proxyrequest.RetryResult{}, proxyrequest.ErrAttemptChanged
		}
	}
	s.mu.RUnlock()
	if !ok {
		return proxyrequest.RetryResult{}, proxyrequest.ErrNotFound
	}
	return session.requestRetry(expectedAttempt, trigger)
}

func (s *ProxyRequestService) RetryByRetryID(digest [32]byte, expectedAttempt int, trigger proxyrequest.RetryTrigger) (proxyrequest.RetryResult, error) {
	if s == nil {
		return proxyrequest.RetryResult{}, proxyrequest.ErrNotFound
	}
	s.mu.RLock()
	session, ok := s.byRetryID[digest]
	if !ok {
		if tombstone, exists := s.terminalByRetryID[digest]; exists && time.Now().Before(tombstone.expires) {
			result, found := tombstone.results[expectedAttempt]
			s.mu.RUnlock()
			if found {
				return result, nil
			}
			return proxyrequest.RetryResult{}, proxyrequest.ErrAttemptChanged
		}
	}
	s.mu.RUnlock()
	if !ok {
		return proxyrequest.RetryResult{}, proxyrequest.ErrNotFound
	}
	storedDigest := session.identity.Digest()
	if !session.identity.Valid() || subtle.ConstantTimeCompare(storedDigest[:], digest[:]) != 1 {
		return proxyrequest.RetryResult{}, proxyrequest.ErrNotFound
	}
	return session.requestRetry(expectedAttempt, trigger)
}

func (s *ProxyRequestService) remove(session *proxyRequestSession) {
	if s == nil || session == nil {
		return
	}
	s.mu.Lock()
	removed := false
	if s.active[session.traceID] == session {
		delete(s.active, session.traceID)
		removed = true
	}
	if session.identity.Valid() && s.byRetryID[session.identity.Digest()] == session {
		delete(s.byRetryID, session.identity.Digest())
	}
	if removed && (session.identity.Valid() || len(session.retryResults) > 0) {
		results := make(map[int]proxyrequest.RetryResult, len(session.retryResults))
		for attempt, result := range session.retryResults {
			results[attempt] = result
		}
		tombstone := retryTombstone{digest: session.identity.Digest(), results: results, expires: time.Now().Add(s.commandRetention)}
		if len(results) > 0 {
			s.terminalByTrace[session.traceID] = tombstone
		}
		if session.identity.Valid() {
			s.terminalByRetryID[session.identity.Digest()] = tombstone
		}
	}
	s.mu.Unlock()
}

func (s *ProxyRequestService) pruneTerminalLocked(now time.Time) {
	for key, tombstone := range s.terminalByTrace {
		if !now.Before(tombstone.expires) {
			delete(s.terminalByTrace, key)
		}
	}
	for key, tombstone := range s.terminalByRetryID {
		if !now.Before(tombstone.expires) {
			delete(s.terminalByRetryID, key)
		}
	}
}

func (s *proxyRequestSession) requestRetry(expectedAttempt int, trigger proxyrequest.RetryTrigger) (proxyrequest.RetryResult, error) {
	var result proxyrequest.RetryResult
	var cancel func()
	var retryErr error
	accepted := false
	if !s.invoke(func() {
		if previous, ok := s.retryResults[expectedAttempt]; ok {
			result = previous
			return
		}
		if s.attempt != expectedAttempt {
			retryErr = proxyrequest.ErrAttemptChanged
			return
		}
		if s.state == proxyrequest.StateRetryRequested {
			result = proxyrequest.RetryResult{Request: s.activeItem(), NextAttempt: s.attempt + 1}
			return
		}
		if s.state != proxyrequest.StateAwaitingResponse {
			retryErr = proxyrequest.ErrRetryNotReady
			return
		}
		if (s.method == http.MethodPost || s.method == http.MethodPatch) && s.idempotencyKey == "" {
			retryErr = proxyrequest.ErrIdempotencyKeyRequired
			return
		}
		if s.attempt >= s.service.maxAttempts {
			retryErr = proxyrequest.ErrMaxAttempts
			return
		}
		s.state = proxyrequest.StateRetryRequested
		accepted = true
		cancel = s.cancelAttempt
		result = proxyrequest.RetryResult{Request: s.activeItem(), NextAttempt: s.attempt + 1}
		s.retryResults[expectedAttempt] = result
	}) {
		return proxyrequest.RetryResult{}, proxyrequest.ErrNotFound
	}
	if retryErr != nil {
		return proxyrequest.RetryResult{}, retryErr
	}
	if accepted {
		s.observe(expectedAttempt, observability.RetryRequested{Trigger: string(trigger), NextAttempt: result.NextAttempt})
		if cancel != nil {
			cancel()
		}
	}
	return result, nil
}

func (s *proxyRequestSession) snapshot() (proxyrequest.ActiveRequest, bool) {
	var item proxyrequest.ActiveRequest
	ok := s.invoke(func() { item = s.activeItem() })
	return item, ok
}

func (s *proxyRequestSession) activeItem() proxyrequest.ActiveRequest {
	return proxyrequest.ActiveRequest{
		TraceID:           s.traceID,
		HasRetryID:        s.identity.Valid(),
		Metadata:          requestmeta.Clone(s.metadata),
		State:             s.state,
		Method:            s.method,
		URL:               s.requestURL,
		TargetURL:         s.targetURL,
		StartedAt:         s.startedAt,
		Attempt:           s.attempt,
		AttemptStartedAt:  s.attemptAt,
		UpstreamStartedAt: s.upstreamAt,
		MaxAttempts:       s.service.maxAttempts,
	}
}
