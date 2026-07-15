package proxyrequestadapter

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/lwmacct/260628-directive-proxy/internal/core/bodymemory"
	"github.com/lwmacct/260628-directive-proxy/internal/core/observability"
	"github.com/lwmacct/260628-directive-proxy/internal/core/proxyrequest"
	"github.com/lwmacct/260628-directive-proxy/internal/core/requestmeta"
)

type ProxyRequestOptions struct {
	MaxAttempts      int
	CommandRetention time.Duration
	InstanceID       string
}

type proxyRequestSession struct {
	service               *ProxyRequestService
	ctx                   context.Context
	trace                 *observability.Trace
	requestStarted        observability.RequestStarted
	requestStartedEmitted bool
	attemptStarted        observability.AttemptStarted
	traceID               string
	identity              proxyrequest.Identity
	startedAt             time.Time
	method                string
	idempotencyKey        string
	requestURL            string
	targetURL             string
	metadata              requestmeta.Metadata
	metadataBound         bool
	attemptMeta           requestmeta.Metadata
	pluginSpecs           map[string][]byte
	retryResults          map[int]proxyrequest.RetryResult
	state                 proxyrequest.State
	attempt               int
	attemptAt             time.Time
	upstreamAt            time.Time
	cancelAttempt         func()

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
	io.ReadCloser
	session *proxyRequestSession
	attempt int
	done    atomic.Bool
}

func (s *proxyRequestSession) TraceID() string { return s.traceID }

func (s *proxyRequestSession) run() {
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

func (s *proxyRequestSession) observe(attempt int, value any) {
	if s != nil && s.trace != nil {
		s.trace.Observe(observability.Signal{Attempt: attempt, ObservedAt: time.Now(), Value: value})
	}
}

func (s *proxyRequestSession) RequestBodyAvailable(body *bodymemory.Body) {
	if s == nil || body == nil {
		return
	}
	s.observe(0, observability.RequestBodyAvailable{Body: body})
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
	s.observe(0, observability.RequestBodyEnded{Total: total, SHA256: digest, Complete: complete})
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
		s.pluginSpecs = nil
		s.attemptStarted = observability.AttemptStarted{Mode: mode, Backend: backend, Endpoint: endpoint, Key: key}
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
	if first {
		if len(bound) > 0 {
			s.observe(attempt, observability.MetadataBound{Metadata: bound})
		}
		return false
	}
	if changed {
		s.observe(attempt, observability.MetadataChanged{Bound: bound, Observed: normalized})
	}
	return changed
}

func (s *proxyRequestSession) ConfigureAttempt(attempt int, specs map[string][]byte) error {
	if s == nil {
		return nil
	}
	if s.service.pipeline == nil {
		if len(specs) > 0 {
			return fmt.Errorf("observability pipeline is unavailable")
		}
	} else if err := s.service.pipeline.ValidatePluginSpecs(specs); err != nil {
		return err
	}
	if s.trace != nil {
		if err := s.trace.ReplacePlugins(specs); err != nil {
			return err
		}
		if attempt == 1 && !s.requestStartedEmitted {
			s.trace.Observe(observability.Signal{Attempt: 0, ObservedAt: s.startedAt, Value: s.requestStarted})
			s.requestStartedEmitted = true
		}
		s.trace.Observe(observability.Signal{Attempt: attempt, ObservedAt: s.attemptAt, Value: s.attemptStarted})
	}
	configured := false
	s.invoke(func() {
		if s.attempt == attempt {
			s.pluginSpecs = clonePluginSpecs(specs)
			configured = true
		}
	})
	if !configured {
		return context.Canceled
	}
	return nil
}

func (s *proxyRequestSession) DirectiveResolved(attempt int, target *url.URL, duration time.Duration, payloadSHA256 string, targetChanged, planChanged bool) {
	if s == nil || target == nil {
		return
	}
	var metadata requestmeta.Metadata
	var pluginSpecs map[string][]byte
	s.invoke(func() {
		if s.attempt == attempt {
			s.targetURL = redactURL(target.String())
		}
		metadata = requestmeta.Clone(s.attemptMeta)
		pluginSpecs = clonePluginSpecs(s.pluginSpecs)
	})
	s.observe(attempt, observability.DirectiveResolved{
		Duration: duration, PayloadSHA256: payloadSHA256, Target: cloneURL(target), TargetChanged: targetChanged,
		PlanChanged: planChanged, Metadata: metadata, PluginSpecs: pluginSpecs,
	})
}

func (s *proxyRequestSession) DirectiveFailed(attempt int, duration time.Duration, code string) {
	if s != nil {
		s.observe(attempt, observability.DirectiveFailed{Duration: duration, Code: code})
	}
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
	s.observe(attempt, observability.UpstreamStarted{TargetURL: targetURL, Header: headers})
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

	outcome := "response_headers"
	if action == proxyrequest.AttemptRetry {
		outcome = "canceled_for_retry"
	} else if attemptErr != nil {
		outcome = "transport_error"
	} else if !responseStarted {
		outcome = "ended_without_response"
	}
	s.observe(attempt, observability.AttemptFinished{Outcome: outcome})
	return action
}

func (s *proxyRequestSession) ObserveUpstreamResponse(attempt int, response *http.Response) {
	if s == nil || response == nil || response.Body == nil {
		return
	}
	var metadata requestmeta.Metadata
	var pluginSpecs map[string][]byte
	s.invoke(func() {
		metadata = requestmeta.Clone(s.attemptMeta)
		pluginSpecs = clonePluginSpecs(s.pluginSpecs)
	})
	s.observe(attempt, observability.UpstreamResponseStarted{
		StatusCode: response.StatusCode, Header: response.Header, AttemptMetadata: metadata, PluginSpecs: pluginSpecs,
	})
	response.Body = &observedResponseBody{ReadCloser: response.Body, session: s, attempt: attempt}
}

func (s *proxyRequestSession) Complete() {
	if s == nil {
		return
	}
	s.completeOnce.Do(func() {
		attempt := s.currentAttempt()
		s.RequestBodyEnd(0, "", false)
		s.finishDownstream()
		outcome := "completed"
		if s.ctx.Err() != nil {
			outcome = "client_canceled"
		}
		s.bodyMu.Lock()
		status := s.responseStatus
		s.bodyMu.Unlock()
		s.observe(attempt, observability.RequestCompleted{
			Outcome: outcome, StatusCode: status, Duration: time.Since(s.startedAt),
		})
		s.stop(func() {
			s.service.remove(s)
			s.cancelAttempt = nil
		})
		if s.trace != nil {
			s.trace.Close()
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
	s.observe(s.currentAttempt(), observability.DownstreamResponseStarted{StatusCode: status, Header: headers})
}

func (s *proxyRequestSession) responseBodyChunk(data []byte) {
	if len(data) > 0 {
		s.observe(s.currentAttempt(), observability.DownstreamBodyChunk{Data: data})
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
	s.observe(s.currentAttempt(), observability.DownstreamBodyEnded{})
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

func (b *observedResponseBody) Read(data []byte) (int, error) {
	n, err := b.ReadCloser.Read(data)
	if n > 0 {
		b.session.observe(b.attempt, observability.UpstreamBodyChunk{Data: data[:n]})
	}
	if err != nil {
		b.finish(err)
	}
	return n, err
}

func (b *observedResponseBody) Close() error {
	err := b.ReadCloser.Close()
	cause := err
	if cause == nil {
		cause = io.ErrUnexpectedEOF
	}
	b.finish(cause)
	return err
}

func (b *observedResponseBody) finish(err error) {
	if b != nil && b.done.CompareAndSwap(false, true) {
		b.session.observe(b.attempt, observability.UpstreamBodyEnded{Cause: err})
	}
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

func clonePluginSpecs(in map[string][]byte) map[string][]byte {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string][]byte, len(in))
	for name, raw := range in {
		out[name] = append([]byte(nil), raw...)
	}
	return out
}

func cloneURL(in *url.URL) *url.URL {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}
