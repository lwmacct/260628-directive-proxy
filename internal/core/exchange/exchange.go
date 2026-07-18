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

	"github.com/lwmacct/260628-directive-proxy/internal/core/lifecycle"
	"github.com/lwmacct/260628-directive-proxy/internal/core/metadata"
	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
	"github.com/lwmacct/260628-directive-proxy/internal/core/program"
	"github.com/lwmacct/260628-directive-proxy/internal/core/recovery"
)

const maxProjectedSSEEventBytes = 16 << 20

type Exchange struct {
	manager        *Manager
	ctx            context.Context
	programRuntime *program.Runtime
	run            *program.Run
	traceID        string
	startedAt      time.Time
	method         string
	idempotencyKey string

	stateMu      sync.Mutex
	phase        Phase
	current      *Attempt
	attemptCount int
	directive    lifecycle.DirectivePrepared
	metadata     metadata.Set
	configured   bool
	maxAttempts  int
	maxElapsed   time.Duration

	lifecycleMu          sync.Mutex
	exchangeScope        *program.Scope
	exchangeProgram      *program.ScopeSet
	requestStarted       lifecycle.RequestStarted
	requestBodyEnded     bool
	responseStatus       int
	downstreamEnded      bool
	downstreamAttempt    *Attempt
	downstreamProjection program.StreamObserver

	completeOnce sync.Once
	completed    atomic.Bool
}

type Attempt struct {
	exchange  *Exchange
	number    int
	startedAt time.Time
	source    lifecycle.AttemptStarted
	cancel    context.CancelFunc

	scope       *program.Scope
	program     *program.ScopeSet
	projection  program.StreamObserver
	scopeOpened atomic.Bool
	closed      atomic.Bool
}

func newExchange(manager *Manager, req *http.Request, startedAt time.Time) *Exchange {
	current := &Exchange{
		manager:        manager,
		ctx:            req.Context(),
		traceID:        newTraceID(),
		startedAt:      startedAt,
		method:         req.Method,
		idempotencyKey: strings.TrimSpace(req.Header.Get("Idempotency-Key")),
		phase:          PhaseStartingBody,
		maxAttempts:    manager.maxAttempts,
		requestStarted: lifecycle.RequestStarted{Method: req.Method, URL: requestURL(req), Host: req.Host, Header: req.Header.Clone()},
	}
	if manager != nil && manager.programRuntime != nil {
		current.programRuntime = manager.programRuntime
	}
	return current
}

func (current *Exchange) TraceID() string {
	if current == nil {
		return ""
	}
	return current.traceID
}

func (current *Exchange) BeginBodyStream() {
	if current == nil {
		return
	}
	current.stateMu.Lock()
	if current.phase == PhaseStartingBody {
		current.phase = PhaseStreamingRequest
	}
	current.stateMu.Unlock()
}

func (current *Exchange) BeginAttempt(cancel context.CancelFunc) (*Attempt, error) {
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
	if current.attemptCount >= current.maxAttempts {
		current.stateMu.Unlock()
		return nil, ErrMaxAttempts
	}
	if !current.configured {
		current.stateMu.Unlock()
		return nil, ErrExchangeNotConfigured
	}
	current.attemptCount++
	attempt := &Attempt{
		exchange:  current,
		number:    current.attemptCount,
		startedAt: startedAt,
		cancel:    cancel,
		source:    attemptStartedFromDirective(current.directive),
	}
	current.current = attempt
	current.phase = PhasePreparingAttempt
	current.stateMu.Unlock()
	return attempt, nil
}

func (current *Exchange) requestRecoveryRetry(expectedAttempt int) error {
	if current == nil || current.completed.Load() {
		return context.Canceled
	}
	var cancel context.CancelFunc
	var attempt *Attempt
	current.stateMu.Lock()
	attempt = current.current
	if attempt == nil || attempt.number != expectedAttempt {
		current.stateMu.Unlock()
		return context.Canceled
	}
	if current.phase == PhaseRetryRequested {
		current.stateMu.Unlock()
		return nil
	}
	if current.phase != PhaseAwaitingResponse && current.phase != PhaseRecovering {
		current.stateMu.Unlock()
		return context.Canceled
	}
	if (current.method == http.MethodPost || current.method == http.MethodPatch) && current.idempotencyKey == "" {
		current.stateMu.Unlock()
		return ErrIdempotencyKeyRequired
	}
	if attempt.number >= current.maxAttempts {
		current.stateMu.Unlock()
		return ErrMaxAttempts
	}
	if current.maxElapsed > 0 && time.Since(current.startedAt) >= current.maxElapsed {
		current.stateMu.Unlock()
		return ErrRecoveryBudgetExceeded
	}
	current.phase = PhaseRetryRequested
	cancel = attempt.cancel
	current.stateMu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

func (current *Exchange) ConfigureRecovery(policy *recovery.Policy, maxAttempts int, maxElapsed time.Duration) {
	if current == nil || policy == nil {
		return
	}
	current.stateMu.Lock()
	attempts := policy.Budget.MaxAttempts
	if maxAttempts > 0 && (attempts == 0 || attempts > maxAttempts) {
		attempts = maxAttempts
	}
	if attempts < 1 {
		attempts = 1
	}
	elapsed := policy.Budget.MaxElapsed
	if maxElapsed > 0 && (elapsed == 0 || elapsed > maxElapsed) {
		elapsed = maxElapsed
	}
	current.maxAttempts = attempts
	current.maxElapsed = elapsed
	current.stateMu.Unlock()
}

func (attempt *Attempt) BeginRecovery() bool {
	if attempt == nil || attempt.exchange == nil {
		return false
	}
	current := attempt.exchange
	current.stateMu.Lock()
	defer current.stateMu.Unlock()
	if current.current != attempt || current.completed.Load() || current.phase == PhaseStreamingResponse || current.phase == PhaseFinished {
		return false
	}
	current.phase = PhaseRecovering
	return true
}

func (attempt *Attempt) RequestRecoveryRetry() error {
	if attempt == nil || attempt.exchange == nil {
		return context.Canceled
	}
	return attempt.exchange.requestRecoveryRetry(attempt.number)
}

func (attempt *Attempt) RecoveryContext() RecoveryContext {
	if attempt == nil || attempt.exchange == nil {
		return RecoveryContext{}
	}
	current := attempt.exchange
	current.stateMu.Lock()
	defer current.stateMu.Unlock()
	elapsed := time.Since(current.startedAt)
	remaining := time.Duration(0)
	if current.maxElapsed > 0 && elapsed < current.maxElapsed {
		remaining = current.maxElapsed - elapsed
	}
	retryAllowed := attempt.number < current.maxAttempts && (current.maxElapsed == 0 || elapsed < current.maxElapsed)
	if (current.method == http.MethodPost || current.method == http.MethodPatch) && current.idempotencyKey == "" {
		retryAllowed = false
	}
	return RecoveryContext{
		TraceID: current.traceID, Attempt: attempt.number,
		MaxAttempts: current.maxAttempts, StartedAt: current.startedAt, Elapsed: elapsed, Remaining: remaining,
		NextAttempt: attempt.number + 1, RetryAllowed: retryAllowed, Metadata: current.metadata,
	}
}

func (current *Exchange) Complete() {
	if current == nil {
		return
	}
	current.completeOnce.Do(func() {
		current.RequestBodyEnd(0, "", false)
		current.finishDownstream()
		outcome := lifecycle.OutcomeCompleted
		finishCause := module.FinishCompleted
		if current.ctx.Err() != nil {
			outcome = lifecycle.OutcomeClientCanceled
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
		if current.exchangeProgram != nil {
			_ = current.exchangeProgram.RequestFinished(current.ctx, lifecycle.RequestFinished{
				Outcome: outcome, StatusCode: status, Duration: time.Since(current.startedAt),
			})
		}
		if current.exchangeScope != nil {
			_ = current.exchangeScope.Finish(context.WithoutCancel(current.ctx), finishCause)
			current.exchangeScope = nil
			current.exchangeProgram = nil
		}
		current.lifecycleMu.Unlock()
		current.completed.Store(true)
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

func cloneURL(in *url.URL) *url.URL {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func attemptStartedFromDirective(value lifecycle.DirectivePrepared) lifecycle.AttemptStarted {
	return lifecycle.AttemptStarted{
		Mode: value.Mode, Backend: value.Backend, Endpoint: value.Endpoint, Resource: value.Resource,
		PayloadSHA256: value.PayloadSHA256, Target: cloneURL(value.Target),
	}
}
