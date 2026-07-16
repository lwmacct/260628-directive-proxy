package proxyrequestadapter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/lwmacct/260628-directive-proxy/internal/core/bodymemory"
	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
	"github.com/lwmacct/260628-directive-proxy/internal/core/proxyrequest"
	"github.com/lwmacct/260628-directive-proxy/internal/core/requestmeta"
)

const maxProjectedSSEEventBytes = 16 << 20

type ProxyRequestOptions struct {
	MaxAttempts      int
	CommandRetention time.Duration
}

type proxyRequestSession struct {
	service        *ProxyRequestService
	ctx            context.Context
	moduleRuntime  *module.Runtime
	run            *module.Run
	requestScope   *module.Scope
	attemptScope   *module.Scope
	requestStarted module.RequestStarted
	attemptStarted module.AttemptStarted
	traceID        string
	identity       proxyrequest.Identity
	startedAt      time.Time
	method         string
	idempotencyKey string
	requestURL     string
	targetURL      string
	metadata       requestmeta.Metadata
	metadataBound  bool
	attemptMeta    requestmeta.Metadata
	retryResults   map[int]proxyrequest.RetryResult
	state          proxyrequest.State
	attempt        int
	attemptAt      time.Time
	upstreamAt     time.Time
	cancelAttempt  func()

	moduleMu             sync.Mutex
	requestConfigured    bool
	attemptScopeAttempt  int
	upstreamProjection   *module.Projection
	downstreamProjection *module.Projection

	bodyMu           sync.Mutex
	requestBodyEnded bool
	responseStatus   int
	downstreamEnded  bool

	completeOnce sync.Once
	events       chan coordinatorEvent
	done         chan struct{}
}

type coordinatorEvent struct {
	run  func()
	stop bool
	done chan struct{}
}

type proxyResponseWriter struct {
	http.ResponseWriter
	session *proxyRequestSession
	wrote   bool
	status  int
}

type observedResponseBody struct {
	source      io.ReadCloser
	session     *proxyRequestSession
	attempt     int
	pending     []byte
	terminalErr error
	done        atomic.Bool
}

func (s *proxyRequestSession) TraceID() string { return s.traceID }

func (s *proxyRequestSession) runCoordinator() {
	defer close(s.done)
	for event := range s.events {
		event.run()
		close(event.done)
		if event.stop {
			return
		}
	}
}

func (s *proxyRequestSession) invoke(run func()) bool {
	if s == nil || run == nil {
		return false
	}
	event := coordinatorEvent{run: run, done: make(chan struct{})}
	select {
	case s.events <- event:
	case <-s.done:
		return false
	}
	select {
	case <-event.done:
		return true
	case <-s.done:
		return false
	}
}

func (s *proxyRequestSession) stop(run func()) {
	if s == nil || run == nil {
		return
	}
	event := coordinatorEvent{run: run, stop: true, done: make(chan struct{})}
	select {
	case s.events <- event:
		<-event.done
	case <-s.done:
	}
}

func (s *proxyRequestSession) WrapResponseWriter(w http.ResponseWriter) http.ResponseWriter {
	if s == nil || w == nil {
		return w
	}
	w.Header().Set("X-Dproxy-Trace-ID", s.traceID)
	return &proxyResponseWriter{ResponseWriter: w, session: s}
}

func (s *proxyRequestSession) ConfigureRequest(specs []module.Spec) error {
	if s == nil {
		return nil
	}
	s.moduleMu.Lock()
	defer s.moduleMu.Unlock()
	if s.requestConfigured {
		return fmt.Errorf("request module program is already configured")
	}
	s.requestConfigured = true
	if s.moduleRuntime == nil || s.run == nil {
		return nil
	}
	compiled, err := s.moduleRuntime.Compile(module.LifetimeRequest, specs)
	if err != nil {
		return err
	}
	scope, err := s.run.OpenScope(module.OpenContext{StartedAt: s.startedAt}, compiled)
	if err != nil {
		return err
	}
	s.requestScope = scope
	return scope.RequestStarted(s.ctx, s.requestStarted)
}

func (s *proxyRequestSession) RequestBodyAvailable(body *bodymemory.Body) {
	if s == nil || body == nil {
		return
	}
	s.moduleMu.Lock()
	defer s.moduleMu.Unlock()
	_ = s.dispatchScopesLocked(func(scope *module.Scope) error {
		return scope.RequestBodyAvailable(s.ctx, module.RequestBodyAvailable{Body: body})
	})
}

func (s *proxyRequestSession) BeginBodyRead() {
	if s == nil {
		return
	}
	s.invoke(func() {
		if s.state == proxyrequest.StateWaitingBodyMemory {
			s.state = proxyrequest.StateReadingBody
		}
	})
}

func (s *proxyRequestSession) RequestBodyEnd(total int64, digest string, complete bool) {
	if s == nil {
		return
	}
	s.bodyMu.Lock()
	if s.requestBodyEnded {
		s.bodyMu.Unlock()
		return
	}
	s.requestBodyEnded = true
	s.bodyMu.Unlock()
	s.moduleMu.Lock()
	defer s.moduleMu.Unlock()
	_ = s.dispatchScopesLocked(func(scope *module.Scope) error {
		return scope.RequestBodyEnded(s.ctx, module.RequestBodyEnded{Total: total, SHA256: digest, Complete: complete})
	})
}

func (s *proxyRequestSession) BeginAttempt(cancel func(), mode, backend, endpoint, key string) int {
	if s == nil {
		return 0
	}
	now := time.Now().UTC()
	attempt := 0
	s.invoke(func() {
		s.attempt++
		s.attemptAt = now
		s.upstreamAt = time.Time{}
		s.cancelAttempt = cancel
		s.attemptMeta = nil
		s.attemptStarted = module.AttemptStarted{Mode: mode, Backend: backend, Endpoint: endpoint, Key: key}
		s.state = proxyrequest.StateResolvingDirective
		attempt = s.attempt
	})
	return attempt
}

func (s *proxyRequestSession) BindMetadata(attempt int, observed requestmeta.Metadata) bool {
	if s == nil {
		return false
	}
	normalized, err := requestmeta.Normalize(observed)
	if err != nil {
		return false
	}
	var bound requestmeta.Metadata
	changed := false
	first := false
	if !s.invoke(func() {
		if s.attempt != attempt {
			return
		}
		s.attemptMeta = requestmeta.Clone(normalized)
		if !s.metadataBound {
			s.metadata = requestmeta.Clone(normalized)
			s.metadataBound = true
			first = true
		}
		bound = requestmeta.Clone(s.metadata)
		changed = !first && !requestmeta.Equal(bound, normalized)
	}) || bound == nil && len(normalized) > 0 {
		return false
	}
	s.moduleMu.Lock()
	defer s.moduleMu.Unlock()
	if first {
		if len(bound) > 0 {
			_ = s.dispatchScopesLocked(func(scope *module.Scope) error {
				return scope.MetadataBound(s.ctx, module.MetadataBound{Metadata: bound})
			})
		}
		return false
	}
	if changed {
		_ = s.dispatchScopesLocked(func(scope *module.Scope) error {
			return scope.MetadataChanged(s.ctx, module.MetadataChanged{Bound: bound, Observed: normalized})
		})
	}
	return changed
}

func (s *proxyRequestSession) ConfigureAttempt(attempt int, specs []module.Spec) error {
	if s == nil {
		return nil
	}
	current := false
	s.invoke(func() { current = s.attempt == attempt })
	if !current {
		return context.Canceled
	}
	if s.moduleRuntime == nil || s.run == nil {
		return nil
	}
	compiled, err := s.moduleRuntime.Compile(module.LifetimeAttempt, specs)
	if err != nil {
		return err
	}
	s.moduleMu.Lock()
	defer s.moduleMu.Unlock()
	if s.attemptScope != nil {
		_ = s.closeAttemptScopeLocked("replaced", module.FinishReplaced)
	}
	scope, err := s.run.OpenScope(module.OpenContext{Attempt: attempt, StartedAt: s.attemptAt}, compiled)
	if err != nil {
		return err
	}
	s.attemptScope = scope
	s.attemptScopeAttempt = attempt
	if s.requestScope != nil {
		s.requestScope.SetAttempt(attempt)
		if err := s.requestScope.AttemptStarted(s.ctx, s.attemptStarted); err != nil {
			return err
		}
	}
	return scope.AttemptStarted(s.ctx, s.attemptStarted)
}

func (s *proxyRequestSession) DirectiveResolved(attempt int, target *url.URL, duration time.Duration, payloadSHA256 string, targetChanged, planChanged bool) {
	if s == nil || target == nil {
		return
	}
	var metadata requestmeta.Metadata
	s.invoke(func() {
		if s.attempt == attempt {
			s.targetURL = redactURL(target.String())
		}
		metadata = requestmeta.Clone(s.attemptMeta)
	})
	s.moduleMu.Lock()
	defer s.moduleMu.Unlock()
	_ = s.dispatchScopesLocked(func(scope *module.Scope) error {
		return scope.DirectiveResolved(s.ctx, module.DirectiveResolved{
			Duration: duration, PayloadSHA256: payloadSHA256, Target: cloneURL(target), TargetChanged: targetChanged,
			PlanChanged: planChanged, Metadata: metadata,
		})
	})
}

func (s *proxyRequestSession) DirectiveFailed(attempt int, duration time.Duration, code string) {
	if s == nil {
		return
	}
	s.moduleMu.Lock()
	defer s.moduleMu.Unlock()
	_ = s.dispatchScopesLocked(func(scope *module.Scope) error {
		return scope.DirectiveFailed(s.ctx, module.DirectiveFailed{Duration: duration, Code: code})
	})
}

func (s *proxyRequestSession) MutateOutboundRequest(attempt int, request *http.Request) error {
	if s == nil || request == nil {
		return nil
	}
	s.moduleMu.Lock()
	defer s.moduleMu.Unlock()
	if s.attemptScopeAttempt != 0 && s.attemptScopeAttempt != attempt {
		return context.Canceled
	}
	if s.requestScope != nil {
		if err := s.requestScope.MutateOutboundRequest(request.Context(), request); err != nil {
			return err
		}
	}
	if s.attemptScope != nil {
		return s.attemptScope.MutateOutboundRequest(request.Context(), request)
	}
	return nil
}

func (s *proxyRequestSession) MutateOutboundBody(attempt int, data []byte) ([]byte, error) {
	if s == nil {
		return data, nil
	}
	draft := module.BodyDraft{Data: append([]byte(nil), data...)}
	s.moduleMu.Lock()
	defer s.moduleMu.Unlock()
	if s.attemptScopeAttempt != 0 && s.attemptScopeAttempt != attempt {
		return nil, context.Canceled
	}
	if s.requestScope != nil {
		if err := s.requestScope.MutateOutboundBody(s.ctx, &draft); err != nil {
			return nil, err
		}
	}
	if s.attemptScope != nil {
		if err := s.attemptScope.MutateOutboundBody(s.ctx, &draft); err != nil {
			return nil, err
		}
	}
	return draft.Data, nil
}

func (s *proxyRequestSession) MutateUpstreamResponse(attempt int, response *http.Response) error {
	if s == nil || response == nil {
		return nil
	}
	draft := module.ResponseDraft{Response: response}
	s.moduleMu.Lock()
	defer s.moduleMu.Unlock()
	if s.attemptScopeAttempt != 0 && s.attemptScopeAttempt != attempt {
		return context.Canceled
	}
	if s.requestScope != nil {
		if err := s.requestScope.MutateUpstreamResponse(s.ctx, &draft); err != nil {
			return err
		}
	}
	if s.attemptScope != nil {
		return s.attemptScope.MutateUpstreamResponse(s.ctx, &draft)
	}
	return nil
}

func (s *proxyRequestSession) BeginUpstream(attempt int, req *http.Request) bool {
	if s == nil {
		return false
	}
	now := time.Now().UTC()
	started := false
	targetURL := ""
	s.invoke(func() {
		if s.attempt != attempt || s.ctx.Err() != nil {
			return
		}
		s.upstreamAt = now
		s.state = proxyrequest.StateAwaitingResponse
		targetURL = s.targetURL
		started = true
	})
	if !started {
		return false
	}
	var headers http.Header
	if req != nil {
		headers = req.Header.Clone()
	}
	s.moduleMu.Lock()
	defer s.moduleMu.Unlock()
	_ = s.dispatchScopesLocked(func(scope *module.Scope) error {
		return scope.UpstreamStarted(s.ctx, module.UpstreamStarted{TargetURL: targetURL, Header: headers})
	})
	return true
}

func (s *proxyRequestSession) FinishAttempt(attempt int, responseStarted bool, attemptErr error) proxyrequest.AttemptAction {
	if s == nil {
		return proxyrequest.AttemptReturn
	}
	action := proxyrequest.AttemptReturn
	s.invoke(func() {
		state := s.state
		if attempt == s.attempt && state == proxyrequest.StateRetryRequested && s.ctx.Err() == nil {
			action = proxyrequest.AttemptRetry
		} else {
			s.service.remove(s)
		}
		s.cancelAttempt = nil
	})
	if responseStarted && attemptErr == nil && action == proxyrequest.AttemptReturn {
		return action
	}
	outcome := "ended_without_response"
	cause := module.FinishFailed
	if action == proxyrequest.AttemptRetry {
		outcome = "canceled_for_retry"
		cause = module.FinishReplaced
	} else if attemptErr != nil {
		outcome = "transport_error"
		if errorsIsCancellation(attemptErr) {
			cause = module.FinishCanceled
		}
	}
	s.moduleMu.Lock()
	_ = s.closeAttemptScopeLocked(outcome, cause)
	s.moduleMu.Unlock()
	return action
}

func (s *proxyRequestSession) ObserveUpstreamResponse(attempt int, response *http.Response) {
	if s == nil || response == nil || response.Body == nil {
		return
	}
	var metadata requestmeta.Metadata
	s.invoke(func() { metadata = requestmeta.Clone(s.attemptMeta) })
	s.moduleMu.Lock()
	s.upstreamProjection = module.NewProjection(
		module.ProjectionUpstream, response.Header.Get("Content-Type"), maxProjectedSSEEventBytes, s.requestScope, s.attemptScope,
	)
	_ = s.dispatchScopesLocked(func(scope *module.Scope) error {
		return scope.UpstreamResponseStarted(s.ctx, module.ResponseStarted{
			StatusCode: response.StatusCode, Header: response.Header.Clone(), Metadata: metadata,
		})
	})
	s.moduleMu.Unlock()
	if response.StatusCode != http.StatusSwitchingProtocols {
		response.Body = &observedResponseBody{source: response.Body, session: s, attempt: attempt}
	}
}

func (s *proxyRequestSession) Complete() {
	if s == nil {
		return
	}
	s.completeOnce.Do(func() {
		s.RequestBodyEnd(0, "", false)
		s.finishDownstream()
		outcome := "completed"
		finishCause := module.FinishCompleted
		if s.ctx.Err() != nil {
			outcome = "client_canceled"
			finishCause = module.FinishCanceled
		}
		s.bodyMu.Lock()
		status := s.responseStatus
		s.bodyMu.Unlock()
		s.moduleMu.Lock()
		if s.attemptScope != nil {
			_ = s.closeAttemptScopeLocked(outcome, finishCause)
		}
		if s.requestScope != nil {
			_ = s.requestScope.RequestFinished(s.ctx, module.RequestFinished{
				Outcome: outcome, StatusCode: status, Duration: time.Since(s.startedAt),
			})
			_ = s.requestScope.Finish(s.ctx, finishCause)
			s.requestScope = nil
		}
		s.moduleMu.Unlock()
		s.stop(func() {
			s.service.remove(s)
			s.cancelAttempt = nil
		})
		if s.run != nil {
			s.run.Close()
		}
	})
}

func (s *proxyRequestSession) currentAttempt() int {
	attempt := 0
	s.invoke(func() { attempt = s.attempt })
	return attempt
}

func (s *proxyRequestSession) responseHeaders(status int, headers http.Header) {
	s.bodyMu.Lock()
	s.responseStatus = status
	s.bodyMu.Unlock()
	s.moduleMu.Lock()
	s.downstreamProjection = module.NewProjection(
		module.ProjectionDownstream, headers.Get("Content-Type"), maxProjectedSSEEventBytes, s.requestScope, s.attemptScope,
	)
	_ = s.dispatchScopesLocked(func(scope *module.Scope) error {
		return scope.DownstreamResponseStarted(s.ctx, module.ResponseStarted{StatusCode: status, Header: headers.Clone()})
	})
	s.moduleMu.Unlock()
}

func (s *proxyRequestSession) responseBodyChunk(data []byte) {
	if len(data) == 0 {
		return
	}
	s.moduleMu.Lock()
	defer s.moduleMu.Unlock()
	_ = s.dispatchScopesLocked(func(scope *module.Scope) error {
		return scope.DownstreamBodyChunk(s.ctx, module.BodyChunk{Data: data})
	})
	if s.downstreamProjection != nil {
		_ = s.downstreamProjection.Feed(s.ctx, time.Now().UTC(), data)
	}
}

func (s *proxyRequestSession) finishDownstream() {
	s.bodyMu.Lock()
	if s.downstreamEnded {
		s.bodyMu.Unlock()
		return
	}
	s.downstreamEnded = true
	s.bodyMu.Unlock()
	s.moduleMu.Lock()
	defer s.moduleMu.Unlock()
	if s.downstreamProjection != nil {
		_ = s.downstreamProjection.Close(s.ctx, time.Now().UTC())
		s.downstreamProjection = nil
	}
	_ = s.dispatchScopesLocked(func(scope *module.Scope) error {
		return scope.DownstreamBodyEnded(s.ctx, module.BodyEnded{})
	})
}

func (s *proxyRequestSession) observeRetryRequested(attempt int, value module.RetryRequested) {
	if s == nil {
		return
	}
	s.moduleMu.Lock()
	defer s.moduleMu.Unlock()
	if s.requestScope != nil {
		s.requestScope.SetAttempt(attempt)
	}
	_ = s.dispatchScopesLocked(func(scope *module.Scope) error { return scope.RetryRequested(s.ctx, value) })
}

func (s *proxyRequestSession) processUpstreamBodyChunk(attempt int, data []byte) ([]byte, error) {
	s.moduleMu.Lock()
	defer s.moduleMu.Unlock()
	if s.attemptScopeAttempt != 0 && s.attemptScopeAttempt != attempt {
		return nil, context.Canceled
	}
	if err := s.dispatchScopesLocked(func(scope *module.Scope) error {
		return scope.UpstreamBodyChunk(s.ctx, module.BodyChunk{Data: data})
	}); err != nil {
		return nil, err
	}
	draft := module.BodyDraft{Data: append([]byte(nil), data...)}
	if s.requestScope != nil {
		if err := s.requestScope.MutateUpstreamBodyChunk(s.ctx, &draft); err != nil {
			return nil, err
		}
	}
	if s.attemptScope != nil {
		if err := s.attemptScope.MutateUpstreamBodyChunk(s.ctx, &draft); err != nil {
			return nil, err
		}
	}
	if s.upstreamProjection != nil {
		if err := s.upstreamProjection.Feed(s.ctx, time.Now().UTC(), draft.Data); err != nil {
			return nil, err
		}
	}
	return draft.Data, nil
}

func (s *proxyRequestSession) finishUpstream(attempt int, cause error) {
	if s == nil {
		return
	}
	s.moduleMu.Lock()
	defer s.moduleMu.Unlock()
	if s.attemptScopeAttempt != attempt {
		return
	}
	if s.upstreamProjection != nil {
		_ = s.upstreamProjection.Close(s.ctx, time.Now().UTC())
		s.upstreamProjection = nil
	}
	_ = s.dispatchScopesLocked(func(scope *module.Scope) error {
		return scope.UpstreamBodyEnded(s.ctx, module.BodyEnded{Cause: cause})
	})
	outcome := "completed"
	finishCause := module.FinishCompleted
	if cause != nil && cause != io.EOF {
		outcome = "interrupted"
		finishCause = module.FinishFailed
		if errorsIsCancellation(cause) {
			finishCause = module.FinishCanceled
		}
	}
	_ = s.closeAttemptScopeLocked(outcome, finishCause)
}

func (s *proxyRequestSession) closeAttemptScopeLocked(outcome string, cause module.FinishCause) error {
	if s.attemptScope == nil {
		return nil
	}
	scope := s.attemptScope
	if s.upstreamProjection != nil {
		_ = s.upstreamProjection.Close(s.ctx, time.Now().UTC())
		s.upstreamProjection = nil
	}
	var result error
	if s.requestScope != nil {
		result = s.requestScope.AttemptFinished(s.ctx, module.AttemptFinished{Outcome: outcome})
	}
	if err := scope.AttemptFinished(s.ctx, module.AttemptFinished{Outcome: outcome}); err != nil && result == nil {
		result = err
	}
	if err := scope.Finish(s.ctx, cause); err != nil && result == nil {
		result = err
	}
	s.attemptScope = nil
	s.attemptScopeAttempt = 0
	return result
}

func (s *proxyRequestSession) dispatchScopesLocked(run func(*module.Scope) error) error {
	if run == nil {
		return nil
	}
	if s.requestScope != nil {
		if err := run(s.requestScope); err != nil {
			return err
		}
	}
	if s.attemptScope != nil {
		return run(s.attemptScope)
	}
	return nil
}

func (w *proxyResponseWriter) WriteHeader(status int) {
	if w.wrote {
		return
	}
	if status >= 100 && status < 200 && status != http.StatusSwitchingProtocols {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	w.wrote = true
	w.status = status
	w.ResponseWriter.WriteHeader(status)
	w.session.responseHeaders(status, w.Header())
}

func (w *proxyResponseWriter) Write(data []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	written, err := w.ResponseWriter.Write(data)
	if written > 0 {
		w.session.responseBodyChunk(data[:written])
	}
	return written, err
}

func (w *proxyResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (b *observedResponseBody) Read(target []byte) (int, error) {
	if len(target) == 0 {
		return 0, nil
	}
	for {
		if len(b.pending) > 0 {
			n := copy(target, b.pending)
			b.pending = b.pending[n:]
			return n, nil
		}
		if b.terminalErr != nil {
			return 0, b.terminalErr
		}
		buffer := make([]byte, max(len(target), 32<<10))
		n, readErr := b.source.Read(buffer)
		if n > 0 {
			processed, err := b.session.processUpstreamBodyChunk(b.attempt, buffer[:n])
			if err != nil {
				b.finish(err)
				return 0, err
			}
			b.pending = processed
		}
		if readErr != nil {
			b.terminalErr = readErr
			b.finish(readErr)
		}
	}
}

func (b *observedResponseBody) Close() error {
	err := b.source.Close()
	cause := err
	if cause == nil {
		cause = io.ErrUnexpectedEOF
	}
	b.finish(cause)
	return err
}

func (b *observedResponseBody) finish(err error) {
	if b != nil && b.done.CompareAndSwap(false, true) {
		b.session.finishUpstream(b.attempt, err)
	}
}

func errorsIsCancellation(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func newTraceID() string {
	id, err := uuid.NewV7()
	if err != nil {
		panic(fmt.Sprintf("generate trace UUIDv7: %v", err))
	}
	return id.String()
}

func requestURL(req *http.Request) string {
	if req.URL == nil {
		return ""
	}
	value := *req.URL
	if value.Scheme == "" {
		value.Scheme = "http"
	}
	if value.Host == "" {
		value.Host = req.Host
	}
	return value.String()
}

func redactURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	query := parsed.Query()
	for name := range query {
		query[name] = []string{"<redacted>"}
	}
	parsed.RawQuery = query.Encode()
	return parsed.Redacted()
}

func cloneURL(in *url.URL) *url.URL {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}
