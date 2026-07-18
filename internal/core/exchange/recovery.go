package exchange

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/lifecycle"
	"github.com/lwmacct/260628-directive-proxy/internal/core/metadata"
	"github.com/lwmacct/260628-directive-proxy/internal/core/recovery"
)

var ErrRecoveryNotStarted = errors.New("exchange recovery cycle was not started")

type RecoveryInput struct {
	Trigger   recovery.Trigger
	Directive recovery.DirectiveInfo
	Metadata  metadata.Set
	Response  *recovery.Response
}

type RecoveryResult struct {
	Decision recovery.Decision
	Outcome  lifecycle.RecoveryOutcome
	Retry    bool
}

type RecoveryCycle struct {
	roundTrip  *RoundTrip
	policy     *recovery.Policy
	controller recovery.ControllerBinding
	event      recovery.Event
	eventID    string
	decision   recovery.Decision
	decided    bool
	deciding   bool
	finished   bool
	mu         sync.Mutex
}

func NewRecoveryCycle(roundTrip *RoundTrip, policy *recovery.Policy, input RecoveryInput) (*RecoveryCycle, error) {
	if roundTrip == nil || policy == nil || policy.Controller.Binding == nil {
		return nil, ErrRecoveryFailed
	}
	if !roundTrip.BeginRecovery() {
		return nil, ErrRecoveryNotStarted
	}
	info := roundTrip.RecoveryContext()
	eventID := fmt.Sprintf("%s:%d:%s", info.TraceID, info.RoundTrip, input.Trigger.Type)
	event := recovery.Event{
		Protocol:   recovery.Protocol,
		EventID:    eventID,
		TraceID:    info.TraceID,
		ObservedAt: time.Now().UTC(),
		RoundTrip: recovery.RoundTripInfo{
			Number: info.RoundTrip, MaxRoundTrips: info.MaxRoundTrips, ElapsedMS: info.Elapsed.Milliseconds(),
			RemainingMS: info.Remaining.Milliseconds(), NextRoundTrip: info.NextRoundTrip, RetryAllowed: info.RetryAllowed,
		},
		Trigger:   input.Trigger,
		Directive: input.Directive,
		Metadata:  input.Metadata.Map(),
		Response:  cloneRecoveryResponse(input.Response),
	}
	cycle := &RecoveryCycle{
		roundTrip: roundTrip, policy: policy, controller: policy.Controller.Binding,
		event: event, eventID: eventID,
	}
	roundTrip.RecoveryStarted(moduleRecoveryStarted(event, policy))
	return cycle, nil
}

func (cycle *RecoveryCycle) Decide(ctx context.Context) (recovery.Decision, error) {
	if cycle == nil || cycle.roundTrip == nil || cycle.controller == nil || cycle.policy == nil {
		return recovery.Decision{}, ErrRecoveryFailed
	}
	cycle.mu.Lock()
	if cycle.decided || cycle.deciding || cycle.finished {
		cycle.mu.Unlock()
		return cycle.decision, errors.New("recovery cycle decision is already finalized")
	}
	cycle.deciding = true
	cycle.mu.Unlock()
	defer func() {
		cycle.mu.Lock()
		cycle.deciding = false
		cycle.mu.Unlock()
	}()
	if ctx == nil {
		ctx = context.Background()
	}
	decision, err := cycle.controller.Decide(ctx, cycle.event)
	if err != nil {
		cycle.finish(lifecycle.RecoveryFinished{
			Outcome:   lifecycle.RecoveryOutcomeControllerError,
			ErrorCode: lifecycle.RecoveryErrorCodeController, Error: err.Error(),
		})
		return recovery.Decision{}, err
	}
	if !validRecoveryDecision(decision.Action, cycle.event.Trigger, cycle.event.Response) || decision.AfterMS < 0 {
		err := fmt.Errorf("recovery callback returned an invalid decision: action=%q trigger=%q after_ms=%d", decision.Action, cycle.event.Trigger.Type, decision.AfterMS)
		cycle.finish(lifecycle.RecoveryFinished{
			Outcome: lifecycle.RecoveryOutcomeInvalidDecision,
			Action:  lifecycle.RecoveryAction(decision.Action), ErrorCode: lifecycle.RecoveryErrorCodeInvalidDecision, Error: err.Error(),
		})
		return recovery.Decision{}, err
	}
	cycle.mu.Lock()
	cycle.decision = decision
	cycle.decided = true
	cycle.mu.Unlock()
	cycle.roundTrip.RecoveryDecided(lifecycle.RecoveryDecided{
		EventID: cycle.eventID, Action: lifecycle.RecoveryAction(decision.Action), AfterMS: decision.AfterMS,
	})
	info := cycle.roundTrip.RecoveryContext()
	if decision.Action == recovery.ActionRetry && !info.RetryAllowed {
		cycle.finish(lifecycle.RecoveryFinished{
			Outcome: lifecycle.RecoveryOutcomeBudgetRejected,
			Action:  lifecycle.RecoveryAction(decision.Action), AfterMS: decision.AfterMS,
			NextRoundTrip: info.NextRoundTrip, ErrorCode: lifecycle.RecoveryErrorCodeRetryNotAllowed,
		})
		return recovery.Decision{}, ErrMaxRoundTrips
	}
	if decision.AfterMS > 0 {
		delay := time.Duration(decision.AfterMS) * time.Millisecond
		if info.Remaining > 0 && delay >= info.Remaining {
			cycle.finish(lifecycle.RecoveryFinished{
				Outcome: lifecycle.RecoveryOutcomeBudgetRejected,
				Action:  lifecycle.RecoveryAction(decision.Action), AfterMS: decision.AfterMS,
				NextRoundTrip: info.NextRoundTrip, ErrorCode: lifecycle.RecoveryErrorCodeBudgetExceeded,
			})
			return recovery.Decision{}, ErrRecoveryBudgetExceeded
		}
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			cycle.finish(lifecycle.RecoveryFinished{
				Outcome: lifecycle.RecoveryOutcomeCanceled,
				Action:  lifecycle.RecoveryAction(decision.Action), AfterMS: decision.AfterMS,
				NextRoundTrip: info.NextRoundTrip, ErrorCode: lifecycle.RecoveryErrorCodeContextCanceled, Error: ctx.Err().Error(),
			})
			return recovery.Decision{}, ctx.Err()
		}
	}
	return decision, nil
}

func (cycle *RecoveryCycle) Apply() (RecoveryResult, error) {
	if cycle == nil || cycle.roundTrip == nil {
		return RecoveryResult{}, ErrRecoveryFailed
	}
	cycle.mu.Lock()
	if !cycle.decided || cycle.finished {
		cycle.mu.Unlock()
		return RecoveryResult{}, ErrRecoveryFailed
	}
	decision := cycle.decision
	cycle.mu.Unlock()
	result := RecoveryResult{Decision: decision}
	switch decision.Action {
	case recovery.ActionRetry:
		retryErr := cycle.roundTrip.RequestRecoveryRetry()
		if retryErr != nil {
			result.Outcome = recoveryOutcomeForError(retryErr)
			cycle.finish(lifecycle.RecoveryFinished{
				Outcome: result.Outcome, Action: lifecycle.RecoveryActionRetry,
				AfterMS: decision.AfterMS, ErrorCode: recoveryErrorCode(retryErr), Error: retryErr.Error(),
			})
			return result, nil
		}
		result.Outcome = lifecycle.RecoveryOutcomeRetryRequested
		result.Retry = true
		info := cycle.roundTrip.RecoveryContext()
		cycle.finish(lifecycle.RecoveryFinished{
			Outcome: result.Outcome, Action: lifecycle.RecoveryActionRetry,
			AfterMS: decision.AfterMS, NextRoundTrip: info.NextRoundTrip,
		})
		return result, nil
	case recovery.ActionForward:
		result.Outcome = lifecycle.RecoveryOutcomeForwarded
		cycle.finish(lifecycle.RecoveryFinished{
			Outcome: result.Outcome, Action: lifecycle.RecoveryActionForward, AfterMS: decision.AfterMS,
		})
		return result, nil
	case recovery.ActionFail:
		result.Outcome = lifecycle.RecoveryOutcomeFailed
		cycle.finish(lifecycle.RecoveryFinished{
			Outcome: result.Outcome, Action: lifecycle.RecoveryActionFail,
			AfterMS: decision.AfterMS, ErrorCode: lifecycle.RecoveryErrorCodeControllerFail,
		})
		return result, ErrRecoveryFailed
	default:
		return RecoveryResult{}, ErrRecoveryFailed
	}
}

func (cycle *RecoveryCycle) finish(value lifecycle.RecoveryFinished) {
	if cycle == nil {
		return
	}
	cycle.mu.Lock()
	if cycle.finished {
		cycle.mu.Unlock()
		return
	}
	cycle.finished = true
	cycle.mu.Unlock()
	value.EventID = cycle.eventID
	cycle.roundTrip.RecoveryFinished(value)
}

func moduleRecoveryStarted(event recovery.Event, policy *recovery.Policy) lifecycle.RecoveryStarted {
	var observation recovery.ControllerObservation
	if observable, ok := policy.Controller.Binding.(recovery.ObservableControllerBinding); ok {
		observation = observable.Observation()
	}
	return lifecycle.RecoveryStarted{
		EventID: event.EventID, Trigger: string(event.Trigger.Type), TriggerCode: event.Trigger.Code,
		TriggerTimeoutMS: event.Trigger.TimeoutMS,
		RoundTrip: lifecycle.RecoveryRoundTrip{
			Number: event.RoundTrip.Number, MaxRoundTrips: event.RoundTrip.MaxRoundTrips,
			ElapsedMS: event.RoundTrip.ElapsedMS, RemainingMS: event.RoundTrip.RemainingMS,
			NextRoundTrip: event.RoundTrip.NextRoundTrip, RetryAllowed: event.RoundTrip.RetryAllowed,
		},
		Directive: lifecycle.RecoveryDirective{
			Mode: event.Directive.Mode, Backend: event.Directive.Backend,
			Endpoint: event.Directive.Endpoint, Resource: event.Directive.Resource,
			PayloadSHA256: event.Directive.PayloadSHA256,
		},
		Response:            moduleRecoveryResponse(event.Response),
		ControllerModule:    policy.Controller.Spec.Module,
		ControllerEndpoint:  observation.Endpoint,
		ControllerTimeoutMS: observation.Timeout.Milliseconds(),
		ControllerHeaders:   observation.Headers.Clone(),
	}
}

func validRecoveryDecision(action recovery.Action, trigger recovery.Trigger, response *recovery.Response) bool {
	switch action {
	case recovery.ActionRetry, recovery.ActionFail:
		return true
	case recovery.ActionForward:
		return trigger.Type == recovery.TriggerUnexpectedStatus && response != nil
	default:
		return false
	}
}

func moduleRecoveryResponse(response *recovery.Response) *lifecycle.RecoveryResponse {
	if response == nil {
		return nil
	}
	result := &lifecycle.RecoveryResponse{StatusCode: response.StatusCode, Header: response.Headers.Clone()}
	result.Body = &lifecycle.RecoveryBody{
		Encoding: response.Body.Encoding, Data: response.Body.Data,
		Size: response.Body.Size, Truncated: response.Body.Truncated,
	}
	return result
}

func cloneRecoveryResponse(response *recovery.Response) *recovery.Response {
	if response == nil {
		return nil
	}
	cloned := *response
	cloned.Headers = response.Headers.Clone()
	return &cloned
}

func recoveryOutcomeForError(err error) lifecycle.RecoveryOutcome {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return lifecycle.RecoveryOutcomeCanceled
	case errors.Is(err, ErrMaxRoundTrips), errors.Is(err, ErrRecoveryBudgetExceeded), errors.Is(err, ErrIdempotencyKeyRequired):
		return lifecycle.RecoveryOutcomeBudgetRejected
	default:
		return lifecycle.RecoveryOutcomeFailed
	}
}

func recoveryErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrMaxRoundTrips):
		return lifecycle.RecoveryErrorCodeMaxRoundTrips
	case errors.Is(err, ErrRecoveryBudgetExceeded):
		return lifecycle.RecoveryErrorCodeBudgetExceeded
	case errors.Is(err, ErrIdempotencyKeyRequired):
		return lifecycle.RecoveryErrorCodeIdempotencyRequired
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return lifecycle.RecoveryErrorCodeContextCanceled
	default:
		return lifecycle.RecoveryErrorCodeRecoveryFailed
	}
}
