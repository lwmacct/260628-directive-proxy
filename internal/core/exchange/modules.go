package exchange

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
	"github.com/lwmacct/260628-directive-proxy/internal/core/requestmeta"
)

func (current *Exchange) ConfigureRequest(specs []module.Spec) error {
	if current == nil {
		return nil
	}
	current.lifecycleMu.Lock()
	defer current.lifecycleMu.Unlock()
	if current.requestConfigured {
		return errors.New("request module program is already configured")
	}
	current.requestConfigured = true
	if current.moduleRuntime == nil || current.run == nil {
		return nil
	}
	compiled, err := current.moduleRuntime.Compile(module.LifetimeRequest, specs)
	if err != nil {
		return err
	}
	scope, err := current.run.OpenScope(module.OpenContext{StartedAt: current.startedAt}, compiled)
	if err != nil {
		return err
	}
	current.requestScope = scope
	return scope.RequestStarted(current.ctx, current.requestStarted)
}

func (current *Exchange) RequestBodyChunk(data []byte) error {
	if current == nil || len(data) == 0 {
		return nil
	}
	current.lifecycleMu.Lock()
	defer current.lifecycleMu.Unlock()
	if current.requestScope != nil {
		return current.requestScope.RequestBodyChunk(current.ctx, module.BodyChunk{Data: data})
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
		_ = current.requestScope.RequestBodyEnded(current.ctx, module.RequestBodyEnded{Total: total, SHA256: digest, Complete: complete})
	}
}

func (attempt *Attempt) Number() int {
	if attempt == nil {
		return 0
	}
	return attempt.number
}

func (attempt *Attempt) ConfigureModules(specs []module.Spec) error {
	if attempt == nil || attempt.exchange == nil || !attempt.exchange.isCurrent(attempt) {
		return context.Canceled
	}
	if !attempt.configured.CompareAndSwap(false, true) {
		return ErrAttemptConfigured
	}
	current := attempt.exchange
	if current.moduleRuntime == nil || current.run == nil {
		return nil
	}
	compiled, err := current.moduleRuntime.Compile(module.LifetimeAttempt, specs)
	if err != nil {
		return err
	}
	scope, err := current.run.OpenScope(module.OpenContext{Attempt: attempt.number, StartedAt: attempt.startedAt}, compiled)
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
	return scope.AttemptStarted(current.ctx, attempt.source)
}

func (attempt *Attempt) BindMetadata(observed requestmeta.Metadata) bool {
	if attempt == nil || attempt.exchange == nil {
		return false
	}
	normalized, err := requestmeta.Normalize(observed)
	if err != nil {
		return false
	}
	current := attempt.exchange
	current.stateMu.Lock()
	if current.current != attempt {
		current.stateMu.Unlock()
		return false
	}
	attempt.metadata = requestmeta.Clone(normalized)
	first := false
	if !current.metadataBound {
		current.metadata = requestmeta.Clone(normalized)
		current.metadataBound = true
		first = true
	}
	bound := requestmeta.Clone(current.metadata)
	changed := !first && !requestmeta.Equal(bound, normalized)
	current.stateMu.Unlock()
	current.lifecycleMu.Lock()
	defer current.lifecycleMu.Unlock()
	if first {
		if len(bound) > 0 {
			_ = current.dispatchLocked(attempt, func(scope *module.Scope) error {
				return scope.MetadataBound(current.ctx, module.MetadataBound{Metadata: bound})
			})
		}
		return false
	}
	if changed {
		_ = current.dispatchLocked(attempt, func(scope *module.Scope) error {
			return scope.MetadataChanged(current.ctx, module.MetadataChanged{Bound: bound, Observed: normalized})
		})
	}
	return changed
}

func (attempt *Attempt) DirectiveResolved(target *url.URL, duration time.Duration, payloadSHA256 string, targetChanged, planChanged bool) {
	if attempt == nil || attempt.exchange == nil || target == nil {
		return
	}
	current := attempt.exchange
	current.stateMu.Lock()
	if current.current == attempt {
		current.targetURL = redactURL(target.String())
	}
	metadata := requestmeta.Clone(attempt.metadata)
	current.stateMu.Unlock()
	current.lifecycleMu.Lock()
	defer current.lifecycleMu.Unlock()
	_ = current.dispatchLocked(attempt, func(scope *module.Scope) error {
		return scope.DirectiveResolved(current.ctx, module.DirectiveResolved{
			Duration: duration, PayloadSHA256: payloadSHA256, Target: cloneURL(target), TargetChanged: targetChanged,
			PlanChanged: planChanged, Metadata: metadata,
		})
	})
}

func (attempt *Attempt) DirectiveFailed(duration time.Duration, code string) {
	if attempt == nil || attempt.exchange == nil {
		return
	}
	current := attempt.exchange
	current.lifecycleMu.Lock()
	defer current.lifecycleMu.Unlock()
	_ = current.dispatchLocked(attempt, func(scope *module.Scope) error {
		return scope.DirectiveFailed(current.ctx, module.DirectiveFailed{Duration: duration, Code: code})
	})
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
	draft := module.BodyDraft{Data: append([]byte(nil), data...)}
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
	draft := module.ResponseDraft{Response: response}
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
	targetURL := current.targetURL
	current.stateMu.Unlock()
	var headers http.Header
	if req != nil {
		headers = req.Header.Clone()
	}
	current.lifecycleMu.Lock()
	defer current.lifecycleMu.Unlock()
	_ = current.dispatchLocked(attempt, func(scope *module.Scope) error {
		return scope.UpstreamStarted(current.ctx, module.UpstreamStarted{TargetURL: targetURL, Header: headers})
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
	outcome := "ended_without_response"
	cause := module.FinishFailed
	if decision == DecisionRetry {
		outcome = "canceled_for_retry"
		cause = module.FinishReplaced
	} else if attemptErr != nil {
		outcome = "transport_error"
		if errorsIsCancellation(attemptErr) {
			cause = module.FinishCanceled
		}
	}
	attempt.finishLifecycle(outcome, cause, nil, false)
	return decision
}

func (attempt *Attempt) RecoveryStarted(value module.RecoveryStarted) {
	if attempt == nil || attempt.exchange == nil {
		return
	}
	current := attempt.exchange
	current.lifecycleMu.Lock()
	defer current.lifecycleMu.Unlock()
	attempt.recoveryEventID = value.EventID
	_ = current.dispatchLocked(attempt, func(scope *module.Scope) error {
		return scope.RecoveryStarted(current.ctx, value)
	})
}

func (attempt *Attempt) RecoveryDecided(value module.RecoveryDecided) {
	if attempt == nil || attempt.exchange == nil {
		return
	}
	current := attempt.exchange
	current.lifecycleMu.Lock()
	defer current.lifecycleMu.Unlock()
	attempt.recoveryAfterMS = value.AfterMS
	_ = current.dispatchLocked(attempt, func(scope *module.Scope) error {
		return scope.RecoveryDecided(current.ctx, value)
	})
}

func (attempt *Attempt) RecoveryFinished(value module.RecoveryFinished) {
	if attempt == nil || attempt.exchange == nil {
		return
	}
	current := attempt.exchange
	current.lifecycleMu.Lock()
	defer current.lifecycleMu.Unlock()
	if value.EventID == "" {
		value.EventID = attempt.recoveryEventID
	}
	_ = current.dispatchLocked(attempt, func(scope *module.Scope) error {
		return scope.RecoveryFinished(current.ctx, value)
	})
}

func (attempt *Attempt) finishLifecycle(outcome string, cause module.FinishCause, bodyCause error, emitBodyEnd bool) {
	if attempt == nil || attempt.exchange == nil || !attempt.closed.CompareAndSwap(false, true) {
		return
	}
	current := attempt.exchange
	current.lifecycleMu.Lock()
	defer current.lifecycleMu.Unlock()
	if attempt.projection != nil {
		_ = attempt.projection.Close(current.ctx, time.Now().UTC())
		attempt.projection = nil
	}
	if emitBodyEnd {
		_ = current.dispatchLocked(attempt, func(scope *module.Scope) error {
			return scope.UpstreamBodyEnded(current.ctx, module.BodyEnded{Cause: bodyCause})
		})
	}
	if current.requestScope != nil {
		current.requestScope.SetAttempt(attempt.number)
		_ = current.requestScope.AttemptFinished(current.ctx, module.AttemptFinished{Outcome: outcome})
	}
	if attempt.scope != nil {
		_ = attempt.scope.AttemptFinished(current.ctx, module.AttemptFinished{Outcome: outcome})
		_ = attempt.scope.Finish(context.WithoutCancel(current.ctx), cause)
		attempt.scope = nil
	}
}

func (current *Exchange) dispatchLocked(attempt *Attempt, run func(*module.Scope) error) error {
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

func finishCauseForBody(cause error) (string, module.FinishCause) {
	if cause == nil || errors.Is(cause, io.EOF) {
		return "completed", module.FinishCompleted
	}
	if errorsIsCancellation(cause) {
		return "interrupted", module.FinishCanceled
	}
	return "interrupted", module.FinishFailed
}
