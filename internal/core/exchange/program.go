package exchange

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/lifecycle"
	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
	"github.com/lwmacct/260628-directive-proxy/internal/core/program"
)

func (current *Exchange) Configure(configuration Configuration) error {
	if current == nil || configuration.Directive.Target == nil {
		return ErrDirectiveInvalid
	}
	fields, err := configuration.Metadata.WithTraceID(current.traceID)
	if err != nil {
		return ErrDirectiveInvalid
	}
	info := configuration.Directive
	value := lifecycle.DirectivePrepared{
		Mode: info.Mode, Backend: info.Backend, Endpoint: info.Endpoint, Resource: info.Resource,
		Duration: info.Duration, PayloadSHA256: info.PayloadSHA256, Target: cloneURL(info.Target),
	}
	current.stateMu.Lock()
	if current.configured {
		current.stateMu.Unlock()
		return ErrExchangeConfigured
	}
	if current.completed.Load() || current.phase == PhaseFinished || current.roundTripCount > 0 {
		current.stateMu.Unlock()
		return context.Canceled
	}
	current.directive = value
	current.metadata = fields
	current.configured = true
	current.stateMu.Unlock()

	current.lifecycleMu.Lock()
	defer current.lifecycleMu.Unlock()
	if configuration.Program == nil {
		return nil
	}
	if current.programRuntime == nil {
		return ErrProgramRuntimeUnavailable
	}
	run, err := current.programRuntime.StartRun(current.traceID, configuration.Program, fields)
	if err != nil {
		return err
	}
	scope, err := run.OpenExchange(module.OpenContext{StartedAt: current.startedAt})
	if err != nil {
		run.Close()
		return err
	}
	current.run = run
	current.exchangeScope = scope
	current.exchangeProgram = program.NewScopeSet(scope)
	if current.exchangeProgram == nil {
		return nil
	}
	if err := current.exchangeProgram.RequestStarted(current.ctx, current.requestStarted); err != nil {
		return err
	}
	return current.exchangeProgram.DirectivePrepared(current.ctx, value)
}

func (current *Exchange) RequestBodyChunk(data []byte) error {
	if current == nil || len(data) == 0 {
		return nil
	}
	current.lifecycleMu.Lock()
	defer current.lifecycleMu.Unlock()
	if current.exchangeProgram != nil {
		return current.exchangeProgram.RequestBodyChunk(current.ctx, lifecycle.BodyChunk{Data: data})
	}
	return nil
}

func (current *Exchange) RequestBodyEnd(total int64, digest string, complete bool) {
	if current == nil {
		return
	}
	current.lifecycleMu.Lock()
	defer current.lifecycleMu.Unlock()
	if current.requestBodyEnded {
		return
	}
	current.requestBodyEnded = true
	if current.exchangeProgram != nil {
		_ = current.exchangeProgram.RequestBodyEnded(current.ctx, lifecycle.RequestBodyEnded{Total: total, SHA256: digest, Complete: complete})
	}
}

func (roundTrip *RoundTrip) Number() int {
	if roundTrip == nil {
		return 0
	}
	return roundTrip.number
}

func (roundTrip *RoundTrip) OpenScope() error {
	if roundTrip == nil || roundTrip.exchange == nil || !roundTrip.exchange.isCurrent(roundTrip) {
		return context.Canceled
	}
	if !roundTrip.scopeOpened.CompareAndSwap(false, true) {
		return ErrRoundTripScopeOpened
	}
	current := roundTrip.exchange
	if current.run == nil {
		return nil
	}
	scope, err := current.run.OpenRoundTrip(module.OpenContext{RoundTrip: roundTrip.number, StartedAt: roundTrip.startedAt})
	if err != nil {
		return err
	}
	current.lifecycleMu.Lock()
	defer current.lifecycleMu.Unlock()
	if roundTrip.closed.Load() || !current.isCurrent(roundTrip) {
		_ = scope.Finish(context.Background(), module.FinishCanceled)
		return context.Canceled
	}
	roundTrip.scope = scope
	roundTrip.program = program.NewScopeSet(current.exchangeScope, scope)
	if roundTrip.program == nil {
		return nil
	}
	roundTrip.program.SetRoundTrip(roundTrip.number)
	return roundTrip.program.RoundTripStarted(current.ctx, roundTrip.source)
}

func (roundTrip *RoundTrip) MutateOutboundRequest(request *http.Request) error {
	if roundTrip == nil || roundTrip.exchange == nil || request == nil || roundTrip.closed.Load() || !roundTrip.exchange.isCurrent(roundTrip) {
		return context.Canceled
	}
	current := roundTrip.exchange
	current.lifecycleMu.Lock()
	defer current.lifecycleMu.Unlock()
	if active := current.activeProgram(roundTrip); active != nil {
		return active.MutateOutboundRequest(request.Context(), request)
	}
	return nil
}

func (roundTrip *RoundTrip) MutateOutboundBodyChunk(data []byte) ([]byte, error) {
	if roundTrip == nil || roundTrip.exchange == nil || roundTrip.closed.Load() || !roundTrip.exchange.isCurrent(roundTrip) {
		return nil, context.Canceled
	}
	current := roundTrip.exchange
	draft := lifecycle.BodyDraft{Data: append([]byte(nil), data...)}
	current.lifecycleMu.Lock()
	defer current.lifecycleMu.Unlock()
	if active := current.activeProgram(roundTrip); active != nil {
		if err := active.MutateOutboundBodyChunk(current.ctx, &draft); err != nil {
			return nil, err
		}
	}
	return draft.Data, nil
}

func (roundTrip *RoundTrip) HasOutboundBodyMutators() bool {
	if roundTrip == nil || roundTrip.exchange == nil {
		return false
	}
	current := roundTrip.exchange
	current.lifecycleMu.Lock()
	defer current.lifecycleMu.Unlock()
	active := current.activeProgram(roundTrip)
	return active != nil && active.HasOutboundBodyMutators()
}

func (roundTrip *RoundTrip) MutateUpstreamResponse(response *http.Response) error {
	if roundTrip == nil || roundTrip.exchange == nil || response == nil || roundTrip.closed.Load() || !roundTrip.exchange.isCurrent(roundTrip) {
		return context.Canceled
	}
	current := roundTrip.exchange
	draft := lifecycle.ResponseDraft{Response: response}
	current.lifecycleMu.Lock()
	defer current.lifecycleMu.Unlock()
	if active := current.activeProgram(roundTrip); active != nil {
		return active.MutateUpstreamResponse(current.ctx, &draft)
	}
	return nil
}

func (roundTrip *RoundTrip) BeginUpstream(req *http.Request) bool {
	if roundTrip == nil || roundTrip.exchange == nil {
		return false
	}
	current := roundTrip.exchange
	current.stateMu.Lock()
	if current.current != roundTrip || current.ctx.Err() != nil {
		current.stateMu.Unlock()
		return false
	}
	current.phase = PhaseAwaitingResponse
	current.stateMu.Unlock()
	var headers http.Header
	targetURL := ""
	if req != nil {
		headers = req.Header.Clone()
		if req.URL != nil {
			targetURL = req.URL.String()
		}
	}
	current.lifecycleMu.Lock()
	defer current.lifecycleMu.Unlock()
	_ = current.dispatchLocked(roundTrip, func(active *program.ScopeSet) error {
		return active.UpstreamStarted(current.ctx, lifecycle.UpstreamStarted{TargetURL: targetURL, Header: headers})
	})
	return true
}

func (roundTrip *RoundTrip) FinishRoundTrip(responseStarted bool, roundTripErr error) Decision {
	if roundTrip == nil || roundTrip.exchange == nil {
		return DecisionReturn
	}
	current := roundTrip.exchange
	decision := DecisionReturn
	closeLifecycle := true
	current.stateMu.Lock()
	if current.current != roundTrip {
		current.stateMu.Unlock()
		return DecisionReturn
	}
	if current.phase == PhaseRetryRequested && current.ctx.Err() == nil {
		decision = DecisionRetry
		current.current = nil
	} else if responseStarted && roundTripErr == nil {
		current.phase = PhaseStreamingResponse
		closeLifecycle = false
	} else {
		current.current = nil
		current.phase = PhaseFinished
	}
	roundTrip.cancel = nil
	current.stateMu.Unlock()
	if !closeLifecycle {
		return decision
	}
	outcome := lifecycle.OutcomeEndedWithoutResponse
	cause := module.FinishFailed
	if decision == DecisionRetry {
		outcome = lifecycle.OutcomeCanceledForRetry
		cause = module.FinishReplaced
	} else if roundTripErr != nil {
		outcome = lifecycle.OutcomeTransportError
		if errorsIsCancellation(roundTripErr) {
			cause = module.FinishCanceled
		}
	}
	roundTrip.finishLifecycle(outcome, cause, nil, false)
	return decision
}

func (roundTrip *RoundTrip) RecoveryStarted(value lifecycle.RecoveryStarted) {
	if roundTrip == nil || roundTrip.exchange == nil {
		return
	}
	current := roundTrip.exchange
	current.lifecycleMu.Lock()
	defer current.lifecycleMu.Unlock()
	_ = current.dispatchLocked(roundTrip, func(active *program.ScopeSet) error {
		return active.RecoveryStarted(current.ctx, value)
	})
}

func (roundTrip *RoundTrip) RecoveryDecided(value lifecycle.RecoveryDecided) {
	if roundTrip == nil || roundTrip.exchange == nil {
		return
	}
	current := roundTrip.exchange
	current.lifecycleMu.Lock()
	defer current.lifecycleMu.Unlock()
	_ = current.dispatchLocked(roundTrip, func(active *program.ScopeSet) error {
		return active.RecoveryDecided(current.ctx, value)
	})
}

func (roundTrip *RoundTrip) RecoveryFinished(value lifecycle.RecoveryFinished) {
	if roundTrip == nil || roundTrip.exchange == nil {
		return
	}
	current := roundTrip.exchange
	current.lifecycleMu.Lock()
	defer current.lifecycleMu.Unlock()
	_ = current.dispatchLocked(roundTrip, func(active *program.ScopeSet) error {
		return active.RecoveryFinished(current.ctx, value)
	})
}

func (roundTrip *RoundTrip) finishLifecycle(outcome lifecycle.Outcome, cause module.FinishCause, bodyCause error, emitBodyEnd bool) {
	if roundTrip == nil || roundTrip.exchange == nil || !roundTrip.closed.CompareAndSwap(false, true) {
		return
	}
	current := roundTrip.exchange
	current.lifecycleMu.Lock()
	defer current.lifecycleMu.Unlock()
	if roundTrip.projection != nil {
		_ = roundTrip.projection.Finish(current.ctx, time.Now().UTC())
		roundTrip.projection = nil
	}
	if emitBodyEnd {
		_ = current.dispatchLocked(roundTrip, func(active *program.ScopeSet) error {
			return active.UpstreamBodyEnded(current.ctx, lifecycle.BodyEnded{Cause: bodyCause})
		})
	}
	if active := current.activeProgram(roundTrip); active != nil {
		_ = active.RoundTripFinished(current.ctx, lifecycle.RoundTripFinished{Outcome: outcome})
	}
	if roundTrip.scope != nil {
		_ = roundTrip.scope.Finish(context.WithoutCancel(current.ctx), cause)
		roundTrip.scope = nil
	}
}

func (current *Exchange) activeProgram(roundTrip *RoundTrip) *program.ScopeSet {
	if roundTrip != nil && roundTrip.program != nil {
		roundTrip.program.SetRoundTrip(roundTrip.number)
		return roundTrip.program
	}
	if current.exchangeProgram != nil && roundTrip != nil {
		current.exchangeProgram.SetRoundTrip(roundTrip.number)
	}
	return current.exchangeProgram
}

func (current *Exchange) dispatchLocked(roundTrip *RoundTrip, run func(*program.ScopeSet) error) error {
	if run == nil {
		return nil
	}
	if active := current.activeProgram(roundTrip); active != nil {
		return run(active)
	}
	return nil
}

func errorsIsCancellation(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func finishCauseForBody(cause error) (lifecycle.Outcome, module.FinishCause) {
	if cause == nil || errors.Is(cause, io.EOF) {
		return lifecycle.OutcomeCompleted, module.FinishCompleted
	}
	if errorsIsCancellation(cause) {
		return lifecycle.OutcomeInterrupted, module.FinishCanceled
	}
	return lifecycle.OutcomeInterrupted, module.FinishFailed
}
