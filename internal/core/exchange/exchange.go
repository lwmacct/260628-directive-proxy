package exchange

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
	"github.com/lwmacct/260628-directive-proxy/internal/core/requestmeta"
	"github.com/lwmacct/260628-directive-proxy/internal/core/retry"
)

const maxProjectedSSEEventBytes = 16 << 20

type Exchange struct {
	manager        *Manager
	ctx            context.Context
	moduleRuntime  *module.Runtime
	run            *module.Run
	traceID        string
	identity       retry.Identity
	startedAt      time.Time
	method         string
	idempotencyKey string
	requestURL     string

	stateMu       sync.Mutex
	phase         Phase
	current       *Attempt
	attemptCount  int
	metadata      requestmeta.Metadata
	metadataBound bool
	targetURL     string
	retryResults  map[int]RetryResult

	lifecycleMu          sync.Mutex
	requestScope         *module.Scope
	requestStarted       module.RequestStarted
	requestConfigured    bool
	requestBodyEnded     bool
	responseStatus       int
	downstreamEnded      bool
	downstreamAttempt    *Attempt
	downstreamProjection *module.Projection

	completeOnce sync.Once
	completed    atomic.Bool
}

type Attempt struct {
	exchange   *Exchange
	number     int
	startedAt  time.Time
	upstreamAt time.Time
	source     module.AttemptStarted
	cancel     context.CancelFunc
	metadata   requestmeta.Metadata

	scope      *module.Scope
	projection *module.Projection
	configured atomic.Bool
	closed     atomic.Bool
}

func newExchange(manager *Manager, req *http.Request, identity retry.Identity, startedAt time.Time) *Exchange {
	current := &Exchange{
		manager:        manager,
		ctx:            req.Context(),
		traceID:        newTraceID(),
		identity:       identity,
		startedAt:      startedAt,
		method:         req.Method,
		idempotencyKey: strings.TrimSpace(req.Header.Get("Idempotency-Key")),
		requestURL:     redactURL(requestURL(req)),
		phase:          PhaseWaitingBody,
		retryResults:   make(map[int]RetryResult),
		requestStarted: module.RequestStarted{Method: req.Method, URL: requestURL(req), Host: req.Host, Header: req.Header.Clone()},
	}
	if manager != nil && manager.moduleRuntime != nil {
		current.moduleRuntime = manager.moduleRuntime
		current.run = manager.moduleRuntime.StartRun(current.traceID)
	}
	return current
}

func (current *Exchange) TraceID() string {
	if current == nil {
		return ""
	}
	return current.traceID
}

func (current *Exchange) BeginBodyRead() {
	if current == nil {
		return
	}
	current.stateMu.Lock()
	if current.phase == PhaseWaitingBody {
		current.phase = PhaseReadingBody
	}
	current.stateMu.Unlock()
}

func (current *Exchange) BeginAttempt(cancel context.CancelFunc, source AttemptSource) (*Attempt, error) {
	if current == nil || current.completed.Load() {
		return nil, context.Canceled
	}
	if err := current.ctx.Err(); err != nil {
		return nil, err
	}
	startedAt := time.Now().UTC()
	current.stateMu.Lock()
	if current.completed.Load() || current.phase == PhaseFinished {
		current.stateMu.Unlock()
		return nil, context.Canceled
	}
	if current.current != nil && !current.current.closed.Load() {
		current.stateMu.Unlock()
		return nil, ErrAttemptActive
	}
	if current.attemptCount >= current.manager.maxAttempts {
		current.stateMu.Unlock()
		return nil, ErrMaxAttempts
	}
	current.attemptCount++
	attempt := &Attempt{
		exchange:  current,
		number:    current.attemptCount,
		startedAt: startedAt,
		cancel:    cancel,
		source: module.AttemptStarted{
			Mode: source.Mode, Backend: source.Backend, Endpoint: source.Endpoint, Key: source.Key,
		},
	}
	current.current = attempt
	current.phase = PhaseResolving
	current.stateMu.Unlock()
	return attempt, nil
}

func (current *Exchange) Snapshot() (Snapshot, bool) {
	if current == nil || current.completed.Load() {
		return Snapshot{}, false
	}
	current.stateMu.Lock()
	item := current.snapshotLocked()
	current.stateMu.Unlock()
	return item, true
}

func (current *Exchange) snapshotLocked() Snapshot {
	item := Snapshot{
		TraceID: current.traceID, HasRetryID: current.identity.Valid(), Metadata: requestmeta.Clone(current.metadata),
		Phase: current.phase, Method: current.method, URL: current.requestURL, TargetURL: current.targetURL,
		StartedAt: current.startedAt, Attempt: current.attemptCount, MaxAttempts: current.manager.maxAttempts,
	}
	if current.current != nil {
		item.Attempt = current.current.number
		item.AttemptStartedAt = current.current.startedAt
		item.UpstreamStartedAt = current.current.upstreamAt
	}
	return item
}

func (current *Exchange) requestRetry(expectedAttempt int, trigger Trigger) (RetryResult, error) {
	if current == nil || current.completed.Load() {
		return RetryResult{}, ErrNotFound
	}
	var cancel context.CancelFunc
	var attempt *Attempt
	accepted := false
	current.stateMu.Lock()
	if previous, ok := current.retryResults[expectedAttempt]; ok {
		current.stateMu.Unlock()
		return previous, nil
	}
	attempt = current.current
	if attempt == nil || attempt.number != expectedAttempt {
		current.stateMu.Unlock()
		return RetryResult{}, ErrAttemptChanged
	}
	if current.phase == PhaseRetryRequested {
		result := RetryResult{Exchange: current.snapshotLocked(), NextAttempt: attempt.number + 1}
		current.stateMu.Unlock()
		return result, nil
	}
	if current.phase != PhaseAwaitingResponse {
		current.stateMu.Unlock()
		return RetryResult{}, ErrRetryNotReady
	}
	if (current.method == http.MethodPost || current.method == http.MethodPatch) && current.idempotencyKey == "" {
		current.stateMu.Unlock()
		return RetryResult{}, ErrIdempotencyKeyRequired
	}
	if attempt.number >= current.manager.maxAttempts {
		current.stateMu.Unlock()
		return RetryResult{}, ErrMaxAttempts
	}
	current.phase = PhaseRetryRequested
	cancel = attempt.cancel
	result := RetryResult{Exchange: current.snapshotLocked(), NextAttempt: attempt.number + 1}
	current.retryResults[expectedAttempt] = result
	accepted = true
	current.stateMu.Unlock()
	if accepted {
		attempt.emitRetryRequested(trigger, result.NextAttempt)
		if cancel != nil {
			cancel()
		}
	}
	return result, nil
}

func (current *Exchange) terminalData() (retry.Identity, map[int]RetryResult) {
	current.stateMu.Lock()
	defer current.stateMu.Unlock()
	results := make(map[int]RetryResult, len(current.retryResults))
	for attempt, result := range current.retryResults {
		results[attempt] = result
	}
	return current.identity, results
}

func (current *Exchange) Complete() {
	if current == nil {
		return
	}
	current.completeOnce.Do(func() {
		current.RequestBodyEnd(0, "", false)
		current.finishDownstream()
		outcome := "completed"
		finishCause := module.FinishCompleted
		if current.ctx.Err() != nil {
			outcome = "client_canceled"
			finishCause = module.FinishCanceled
		}
		current.stateMu.Lock()
		attempt := current.current
		current.current = nil
		current.phase = PhaseFinished
		current.stateMu.Unlock()
		if attempt != nil {
			attempt.finishLifecycle(outcome, finishCause, nil, false)
		}
		current.lifecycleMu.Lock()
		status := current.responseStatus
		if current.requestScope != nil {
			_ = current.requestScope.RequestFinished(current.ctx, module.RequestFinished{
				Outcome: outcome, StatusCode: status, Duration: time.Since(current.startedAt),
			})
			_ = current.requestScope.Finish(context.WithoutCancel(current.ctx), finishCause)
			current.requestScope = nil
		}
		current.lifecycleMu.Unlock()
		current.completed.Store(true)
		current.manager.remove(current)
		current.closeRun()
	})
}

func (current *Exchange) closeRun() {
	if current != nil && current.run != nil {
		current.run.Close()
	}
}

func (current *Exchange) isCurrent(attempt *Attempt) bool {
	if current == nil || attempt == nil || current.completed.Load() {
		return false
	}
	current.stateMu.Lock()
	ok := current.current == attempt
	current.stateMu.Unlock()
	return ok
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
