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
	"github.com/lwmacct/260628-directive-proxy/internal/core/requestmeta"
)

func (current *Exchange) ConfigureProgram(executable *program.Executable) error {
	if current == nil {
		return nil
	}
	current.lifecycleMu.Lock()
	defer current.lifecycleMu.Unlock()
	if current.programConfigured {
		return errors.New("directive program is already configured")
	}
	current.programConfigured = true
	if executable == nil {
		return nil
	}
	if current.programRuntime == nil {
		return errors.New("program runtime is unavailable")
	}
	run, err := current.programRuntime.StartRun(current.traceID, executable)
	if err != nil {
		return err
	}
	scope, err := run.OpenRequest(module.OpenContext{StartedAt: current.startedAt})
	if err != nil {
		run.Close()
		return err
	}
	current.run = run
	current.requestScope = scope
	if scope == nil {
		return nil
	}
	return scope.RequestStarted(current.ctx, current.requestStarted)
}

func (current *Exchange) PrepareDirective(info DirectiveInfo) error {
	if current == nil || info.Target == nil {
		return ErrDirectiveInvalid
	}
	metadata, err := requestmeta.Normalize(info.Metadata)
	if err != nil {
		return ErrDirectiveInvalid
	}
	value := lifecycle.DirectivePrepared{
		Mode: info.Mode, Backend: info.Backend, Endpoint: info.Endpoint, Resource: info.Resource,
		Duration: info.Duration, PayloadSHA256: info.PayloadSHA256, Target: cloneURL(info.Target), Metadata: metadata,
	}
	current.lifecycleMu.Lock()
	configured := current.programConfigured
	current.lifecycleMu.Unlock()
	if !configured {
		return ErrProgramNotConfigured
	}
	current.stateMu.Lock()
	if current.directivePrepared {
		current.stateMu.Unlock()
		return ErrDirectiveAlreadySet
	}
	if current.completed.Load() || current.phase == PhaseFinished || current.attemptCount > 0 {
		current.stateMu.Unlock()
		return context.Canceled
	}
	current.directive = value
	current.directivePrepared = true
	current.stateMu.Unlock()
	current.lifecycleMu.Lock()
	defer current.lifecycleMu.Unlock()
	if current.requestScope != nil {
		return current.requestScope.DirectivePrepared(current.ctx, value)
	}
	return nil
}

func (current *Exchange) RequestBodyChunk(data []byte) error {
	if current == nil || len(data) == 0 {
		return nil
	}
	current.lifecycleMu.Lock()
	defer current.lifecycleMu.Unlock()
	if current.requestScope != nil {
		return current.requestScope.RequestBodyChunk(current.ctx, lifecycle.BodyChunk{Data: data})
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
	if current.requestScope != nil {
		_ = current.requestScope.RequestBodyEnded(current.ctx, lifecycle.RequestBodyEnded{Total: total, SHA256: digest, Complete: complete})
	}
}

func (attempt *Attempt) Number() int {
	if attempt == nil {
		return 0
	}
	return attempt.number
}

func (attempt *Attempt) OpenScope() error {
	if attempt == nil || attempt.exchange == nil || !attempt.exchange.isCurrent(attempt) {
		return context.Canceled
	}
	if !attempt.scopeOpened.CompareAndSwap(false, true) {
		return ErrAttemptScopeOpened
	}
	current := attempt.exchange
	if current.run == nil {
		return nil
	}
	scope, err := current.run.OpenAttempt(module.OpenContext{Attempt: attempt.number, StartedAt: attempt.startedAt})
	if err != nil {
		return err
	}
	current.lifecycleMu.Lock()
	defer current.lifecycleMu.Unlock()
	if attempt.closed.Load() || !current.isCurrent(attempt) {
		_ = scope.Finish(context.Background(), module.FinishCanceled)
		return context.Canceled
	}
	attempt.scope = scope
	if current.requestScope != nil {
		current.requestScope.SetAttempt(attempt.number)
		if err := current.requestScope.AttemptStarted(current.ctx, attempt.source); err != nil {
			return err
		}
	}
	if scope == nil {
		return nil
	}
	return scope.AttemptStarted(current.ctx, attempt.source)
}

func (attempt *Attempt) MutateOutboundRequest(request *http.Request) error {
	if attempt == nil || attempt.exchange == nil || request == nil || attempt.closed.Load() || !attempt.exchange.isCurrent(attempt) {
		return context.Canceled
	}
	current := attempt.exchange
	current.lifecycleMu.Lock()
	defer current.lifecycleMu.Unlock()
	if current.requestScope != nil {
		if err := current.requestScope.MutateOutboundRequest(request.Context(), request); err != nil {
			return err
		}
	}
	if attempt.scope != nil {
		return attempt.scope.MutateOutboundRequest(request.Context(), request)
	}
	return nil
}

func (attempt *Attempt) MutateOutboundBodyChunk(data []byte) ([]byte, error) {
	if attempt == nil || attempt.exchange == nil || attempt.closed.Load() || !attempt.exchange.isCurrent(attempt) {
		return nil, context.Canceled
	}
	current := attempt.exchange
	draft := lifecycle.BodyDraft{Data: append([]byte(nil), data...)}
	current.lifecycleMu.Lock()
	defer current.lifecycleMu.Unlock()
	if current.requestScope != nil {
		if err := current.requestScope.MutateOutboundBodyChunk(current.ctx, &draft); err != nil {
			return nil, err
		}
	}
	if attempt.scope != nil {
		if err := attempt.scope.MutateOutboundBodyChunk(current.ctx, &draft); err != nil {
			return nil, err
		}
	}
	return draft.Data, nil
}

func (attempt *Attempt) HasOutboundBodyMutators() bool {
	if attempt == nil || attempt.exchange == nil {
		return false
	}
	current := attempt.exchange
	current.lifecycleMu.Lock()
	defer current.lifecycleMu.Unlock()
	return current.requestScope != nil && current.requestScope.HasOutboundBodyMutators() ||
		attempt.scope != nil && attempt.scope.HasOutboundBodyMutators()
}

func (attempt *Attempt) MutateUpstreamResponse(response *http.Response) error {
	if attempt == nil || attempt.exchange == nil || response == nil || attempt.closed.Load() || !attempt.exchange.isCurrent(attempt) {
		return context.Canceled
	}
	current := attempt.exchange
	draft := lifecycle.ResponseDraft{Response: response}
	current.lifecycleMu.Lock()
	defer current.lifecycleMu.Unlock()
	if current.requestScope != nil {
		if err := current.requestScope.MutateUpstreamResponse(current.ctx, &draft); err != nil {
			return err
		}
	}
	if attempt.scope != nil {
		return attempt.scope.MutateUpstreamResponse(current.ctx, &draft)
	}
	return nil
}

func (attempt *Attempt) BeginUpstream(req *http.Request) bool {
	if attempt == nil || attempt.exchange == nil {
		return false
	}
	current := attempt.exchange
	current.stateMu.Lock()
	if current.current != attempt || current.ctx.Err() != nil {
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
	_ = current.dispatchLocked(attempt, func(scope *program.Scope) error {
		return scope.UpstreamStarted(current.ctx, lifecycle.UpstreamStarted{TargetURL: targetURL, Header: headers})
	})
	return true
}

func (attempt *Attempt) FinishRoundTrip(responseStarted bool, attemptErr error) Decision {
	if attempt == nil || attempt.exchange == nil {
		return DecisionReturn
	}
	current := attempt.exchange
	decision := DecisionReturn
	closeLifecycle := true
	current.stateMu.Lock()
	if current.current != attempt {
		current.stateMu.Unlock()
		return DecisionReturn
	}
	if current.phase == PhaseRetryRequested && current.ctx.Err() == nil {
		decision = DecisionRetry
		current.current = nil
	} else if responseStarted && attemptErr == nil {
		current.phase = PhaseStreamingResponse
		closeLifecycle = false
	} else {
		current.current = nil
		current.phase = PhaseFinished
	}
	attempt.cancel = nil
	current.stateMu.Unlock()
	if !closeLifecycle {
		return decision
	}
	outcome := lifecycle.OutcomeEndedWithoutResponse
	cause := module.FinishFailed
	if decision == DecisionRetry {
		outcome = lifecycle.OutcomeCanceledForRetry
		cause = module.FinishReplaced
	} else if attemptErr != nil {
		outcome = lifecycle.OutcomeTransportError
		if errorsIsCancellation(attemptErr) {
			cause = module.FinishCanceled
		}
	}
	attempt.finishLifecycle(outcome, cause, nil, false)
	return decision
}

func (attempt *Attempt) RecoveryStarted(value lifecycle.RecoveryStarted) {
	if attempt == nil || attempt.exchange == nil {
		return
	}
	current := attempt.exchange
	current.lifecycleMu.Lock()
	defer current.lifecycleMu.Unlock()
	_ = current.dispatchLocked(attempt, func(scope *program.Scope) error {
		return scope.RecoveryStarted(current.ctx, value)
	})
}

func (attempt *Attempt) RecoveryDecided(value lifecycle.RecoveryDecided) {
	if attempt == nil || attempt.exchange == nil {
		return
	}
	current := attempt.exchange
	current.lifecycleMu.Lock()
	defer current.lifecycleMu.Unlock()
	_ = current.dispatchLocked(attempt, func(scope *program.Scope) error {
		return scope.RecoveryDecided(current.ctx, value)
	})
}

func (attempt *Attempt) RecoveryFinished(value lifecycle.RecoveryFinished) {
	if attempt == nil || attempt.exchange == nil {
		return
	}
	current := attempt.exchange
	current.lifecycleMu.Lock()
	defer current.lifecycleMu.Unlock()
	_ = current.dispatchLocked(attempt, func(scope *program.Scope) error {
		return scope.RecoveryFinished(current.ctx, value)
	})
}

func (attempt *Attempt) finishLifecycle(outcome lifecycle.Outcome, cause module.FinishCause, bodyCause error, emitBodyEnd bool) {
	if attempt == nil || attempt.exchange == nil || !attempt.closed.CompareAndSwap(false, true) {
		return
	}
	current := attempt.exchange
	current.lifecycleMu.Lock()
	defer current.lifecycleMu.Unlock()
	if attempt.projection != nil {
		_ = attempt.projection.Finish(current.ctx, time.Now().UTC())
		attempt.projection = nil
	}
	if emitBodyEnd {
		_ = current.dispatchLocked(attempt, func(scope *program.Scope) error {
			return scope.UpstreamBodyEnded(current.ctx, lifecycle.BodyEnded{Cause: bodyCause})
		})
	}
	if current.requestScope != nil {
		current.requestScope.SetAttempt(attempt.number)
		_ = current.requestScope.AttemptFinished(current.ctx, lifecycle.AttemptFinished{Outcome: outcome})
	}
	if attempt.scope != nil {
		_ = attempt.scope.AttemptFinished(current.ctx, lifecycle.AttemptFinished{Outcome: outcome})
		_ = attempt.scope.Finish(context.WithoutCancel(current.ctx), cause)
		attempt.scope = nil
	}
}

func (current *Exchange) dispatchLocked(attempt *Attempt, run func(*program.Scope) error) error {
	if run == nil {
		return nil
	}
	if current.requestScope != nil {
		if attempt != nil {
			current.requestScope.SetAttempt(attempt.number)
		}
		if err := run(current.requestScope); err != nil {
			return err
		}
	}
	if attempt != nil && attempt.scope != nil {
		return run(attempt.scope)
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
