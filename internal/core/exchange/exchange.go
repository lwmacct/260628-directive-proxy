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

	stateMu        sync.Mutex
	phase          Phase
	current        *RoundTrip
	roundTripCount int
	directive      lifecycle.DirectivePrepared
	metadata       metadata.Set
	configured     bool
	maxRoundTrips  int
	maxElapsed     time.Duration

	lifecycleMu          sync.Mutex
	exchangeScope        *program.Scope
	exchangeProgram      *program.ScopeSet
	requestStarted       lifecycle.RequestStarted
	requestBodyEnded     bool
	responseStatus       int
	downstreamEnded      bool
	downstreamRoundTrip  *RoundTrip
	downstreamProjection program.StreamObserver

	completeOnce sync.Once
	completed    atomic.Bool
}

type RoundTrip struct {
	exchange  *Exchange
	number    int
	startedAt time.Time
	source    lifecycle.RoundTripStarted
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
		maxRoundTrips:  manager.maxRoundTrips,
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

func (current *Exchange) BeginRoundTrip(cancel context.CancelFunc) (*RoundTrip, error) {
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
		return nil, ErrRoundTripActive
	}
	if current.roundTripCount >= current.maxRoundTrips {
		current.stateMu.Unlock()
		return nil, ErrMaxRoundTrips
	}
	if !current.configured {
		current.stateMu.Unlock()
		return nil, ErrExchangeNotConfigured
	}
	current.roundTripCount++
	roundTrip := &RoundTrip{
		exchange:  current,
		number:    current.roundTripCount,
		startedAt: startedAt,
		cancel:    cancel,
		source:    roundTripStartedFromDirective(current.directive),
	}
	current.current = roundTrip
	current.phase = PhasePreparingRoundTrip
	current.stateMu.Unlock()
	return roundTrip, nil
}

func (current *Exchange) requestRecoveryRetry(expectedRoundTrip int) error {
	if current == nil || current.completed.Load() {
		return context.Canceled
	}
	var cancel context.CancelFunc
	var roundTrip *RoundTrip
	current.stateMu.Lock()
	roundTrip = current.current
	if roundTrip == nil || roundTrip.number != expectedRoundTrip {
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
	if roundTrip.number >= current.maxRoundTrips {
		current.stateMu.Unlock()
		return ErrMaxRoundTrips
	}
	if current.maxElapsed > 0 && time.Since(current.startedAt) >= current.maxElapsed {
		current.stateMu.Unlock()
		return ErrRecoveryBudgetExceeded
	}
	current.phase = PhaseRetryRequested
	cancel = roundTrip.cancel
	current.stateMu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

func (current *Exchange) ConfigureRecovery(policy *recovery.Policy, maxRoundTrips int, maxElapsed time.Duration) {
	if current == nil || policy == nil {
		return
	}
	current.stateMu.Lock()
	roundTrips := policy.Budget.MaxRoundTrips
	if maxRoundTrips > 0 && (roundTrips == 0 || roundTrips > maxRoundTrips) {
		roundTrips = maxRoundTrips
	}
	if roundTrips < 1 {
		roundTrips = 1
	}
	elapsed := policy.Budget.MaxElapsed
	if maxElapsed > 0 && (elapsed == 0 || elapsed > maxElapsed) {
		elapsed = maxElapsed
	}
	current.maxRoundTrips = roundTrips
	current.maxElapsed = elapsed
	current.stateMu.Unlock()
}

func (roundTrip *RoundTrip) BeginRecovery() bool {
	if roundTrip == nil || roundTrip.exchange == nil {
		return false
	}
	current := roundTrip.exchange
	current.stateMu.Lock()
	defer current.stateMu.Unlock()
	if current.current != roundTrip || current.completed.Load() || current.phase == PhaseStreamingResponse || current.phase == PhaseFinished {
		return false
	}
	current.phase = PhaseRecovering
	return true
}

func (roundTrip *RoundTrip) RequestRecoveryRetry() error {
	if roundTrip == nil || roundTrip.exchange == nil {
		return context.Canceled
	}
	return roundTrip.exchange.requestRecoveryRetry(roundTrip.number)
}

func (roundTrip *RoundTrip) RecoveryContext() RecoveryContext {
	if roundTrip == nil || roundTrip.exchange == nil {
		return RecoveryContext{}
	}
	current := roundTrip.exchange
	current.stateMu.Lock()
	defer current.stateMu.Unlock()
	elapsed := time.Since(current.startedAt)
	remaining := time.Duration(0)
	if current.maxElapsed > 0 && elapsed < current.maxElapsed {
		remaining = current.maxElapsed - elapsed
	}
	retryAllowed := roundTrip.number < current.maxRoundTrips && (current.maxElapsed == 0 || elapsed < current.maxElapsed)
	if (current.method == http.MethodPost || current.method == http.MethodPatch) && current.idempotencyKey == "" {
		retryAllowed = false
	}
	return RecoveryContext{
		TraceID: current.traceID, RoundTrip: roundTrip.number,
		MaxRoundTrips: current.maxRoundTrips, StartedAt: current.startedAt, Elapsed: elapsed, Remaining: remaining,
		NextRoundTrip: roundTrip.number + 1, RetryAllowed: retryAllowed, Metadata: current.metadata,
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
		roundTrip := current.current
		current.current = nil
		current.phase = PhaseFinished
		current.stateMu.Unlock()
		if roundTrip != nil {
			roundTrip.finishLifecycle(outcome, finishCause, nil, false)
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

func (current *Exchange) isCurrent(roundTrip *RoundTrip) bool {
	if current == nil || roundTrip == nil || current.completed.Load() {
		return false
	}
	current.stateMu.Lock()
	ok := current.current == roundTrip
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

func roundTripStartedFromDirective(value lifecycle.DirectivePrepared) lifecycle.RoundTripStarted {
	return lifecycle.RoundTripStarted{
		Mode: value.Mode, Backend: value.Backend, Endpoint: value.Endpoint, Resource: value.Resource,
		PayloadSHA256: value.PayloadSHA256, Target: cloneURL(value.Target),
	}
}
