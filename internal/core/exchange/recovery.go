package exchange

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
	"github.com/lwmacct/260628-directive-proxy/internal/core/recovery"
)

var ErrRecoveryNotStarted = errors.New("exchange recovery cycle was not started")

type RecoveryInput struct {
	Trigger   recovery.Trigger
	Directive recovery.DirectiveInfo
	Metadata  map[string][]string
	Response  *recovery.Response
}

type RecoveryResult struct {
	Decision recovery.Decision
	Outcome  module.RecoveryOutcome
	Retry    bool
}

type RecoveryCycle struct {
	attempt    *Attempt
	policy     *recovery.Policy
	controller recovery.Controller
	event      recovery.Event
	eventID    string
	decision   recovery.Decision
	decided    bool
	deciding   bool
	finished   bool
	mu         sync.Mutex
}

func NewRecoveryCycle(attempt *Attempt, policy *recovery.Policy, controller recovery.Controller, input RecoveryInput) (*RecoveryCycle, error) {
	if attempt == nil || policy == nil || controller == nil {
		return nil, ErrRecoveryFailed
	}
	if !attempt.BeginRecovery() {
		return nil, ErrRecoveryNotStarted
	}
	info := attempt.RecoveryContext()
	eventID := fmt.Sprintf("%s:%d:%s", info.TraceID, info.Attempt, input.Trigger.Type)
	event := recovery.Event{
		Protocol:   recovery.Protocol,
		EventID:    eventID,
		TraceID:    info.TraceID,
		ObservedAt: time.Now().UTC(),
		Attempt: recovery.AttemptInfo{
			Number: info.Attempt, MaxAttempts: info.MaxAttempts, ElapsedMS: info.Elapsed.Milliseconds(),
			RemainingMS: info.Remaining.Milliseconds(), NextAttempt: info.NextAttempt, RetryAllowed: info.RetryAllowed,
		},
		Trigger:   input.Trigger,
		Directive: input.Directive,
		Metadata:  cloneMetadata(input.Metadata),
		Response:  cloneRecoveryResponse(input.Response),
	}
	cycle := &RecoveryCycle{
		attempt: attempt, policy: policy, controller: controller,
		event: event, eventID: eventID,
	}
	attempt.RecoveryStarted(moduleRecoveryStarted(event, policy))
	return cycle, nil
}

func (cycle *RecoveryCycle) Decide(ctx context.Context) (recovery.Decision, error) {
	if cycle == nil || cycle.attempt == nil || cycle.controller == nil || cycle.policy == nil {
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
	decision, err := cycle.controller.Decide(ctx, cycle.policy.Controller, cycle.event)
	if err != nil {
		cycle.finish(module.RecoveryFinished{
			Outcome:   module.RecoveryOutcomeControllerError,
			ErrorCode: module.RecoveryErrorCodeController, Error: err.Error(),
		})
		return recovery.Decision{}, err
	}
	if !validRecoveryDecision(decision.Action, cycle.event.Trigger, cycle.event.Response) || decision.AfterMS < 0 {
		err := fmt.Errorf("recovery callback returned an invalid decision: action=%q trigger=%q after_ms=%d", decision.Action, cycle.event.Trigger.Type, decision.AfterMS)
		cycle.finish(module.RecoveryFinished{
			Outcome: module.RecoveryOutcomeInvalidDecision,
			Action:  module.RecoveryAction(decision.Action), ErrorCode: module.RecoveryErrorCodeInvalidDecision, Error: err.Error(),
		})
		return recovery.Decision{}, err
	}
	cycle.mu.Lock()
	cycle.decision = decision
	cycle.decided = true
	cycle.mu.Unlock()
	cycle.attempt.RecoveryDecided(module.RecoveryDecided{
		EventID: cycle.eventID, Action: module.RecoveryAction(decision.Action), AfterMS: decision.AfterMS,
	})
	info := cycle.attempt.RecoveryContext()
	if decision.Action == recovery.ActionRetry && !info.RetryAllowed {
		cycle.finish(module.RecoveryFinished{
			Outcome: module.RecoveryOutcomeBudgetRejected,
			Action:  module.RecoveryAction(decision.Action), AfterMS: decision.AfterMS,
			NextAttempt: info.NextAttempt, ErrorCode: module.RecoveryErrorCodeRetryNotAllowed,
		})
		return recovery.Decision{}, ErrMaxAttempts
	}
	if decision.AfterMS > 0 {
		delay := time.Duration(decision.AfterMS) * time.Millisecond
		if info.Remaining > 0 && delay >= info.Remaining {
			cycle.finish(module.RecoveryFinished{
				Outcome: module.RecoveryOutcomeBudgetRejected,
				Action:  module.RecoveryAction(decision.Action), AfterMS: decision.AfterMS,
				NextAttempt: info.NextAttempt, ErrorCode: module.RecoveryErrorCodeBudgetExceeded,
			})
			return recovery.Decision{}, ErrRecoveryBudgetExceeded
		}
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			cycle.finish(module.RecoveryFinished{
				Outcome: module.RecoveryOutcomeCanceled,
				Action:  module.RecoveryAction(decision.Action), AfterMS: decision.AfterMS,
				NextAttempt: info.NextAttempt, ErrorCode: module.RecoveryErrorCodeContextCanceled, Error: ctx.Err().Error(),
			})
			return recovery.Decision{}, ctx.Err()
		}
	}
	return decision, nil
}

func (cycle *RecoveryCycle) Apply() (RecoveryResult, error) {
	if cycle == nil || cycle.attempt == nil {
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
		retryErr := cycle.attempt.RequestRecoveryRetry()
		if retryErr != nil {
			result.Outcome = recoveryOutcomeForError(retryErr)
			cycle.finish(module.RecoveryFinished{
				Outcome: result.Outcome, Action: module.RecoveryActionRetry,
				AfterMS: decision.AfterMS, ErrorCode: recoveryErrorCode(retryErr), Error: retryErr.Error(),
			})
			return result, nil
		}
		result.Outcome = module.RecoveryOutcomeRetryRequested
		result.Retry = true
		info := cycle.attempt.RecoveryContext()
		cycle.finish(module.RecoveryFinished{
			Outcome: result.Outcome, Action: module.RecoveryActionRetry,
			AfterMS: decision.AfterMS, NextAttempt: info.NextAttempt,
		})
		return result, nil
	case recovery.ActionForward:
		result.Outcome = module.RecoveryOutcomeForwarded
		cycle.finish(module.RecoveryFinished{
			Outcome: result.Outcome, Action: module.RecoveryActionForward, AfterMS: decision.AfterMS,
		})
		return result, nil
	case recovery.ActionFail:
		result.Outcome = module.RecoveryOutcomeFailed
		cycle.finish(module.RecoveryFinished{
			Outcome: result.Outcome, Action: module.RecoveryActionFail,
			AfterMS: decision.AfterMS, ErrorCode: module.RecoveryErrorCodeControllerFail,
		})
		return result, ErrRecoveryFailed
	default:
		return RecoveryResult{}, ErrRecoveryFailed
	}
}

func (cycle *RecoveryCycle) finish(value module.RecoveryFinished) {
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
	cycle.attempt.RecoveryFinished(value)
}

func moduleRecoveryStarted(event recovery.Event, policy *recovery.Policy) module.RecoveryStarted {
	controllerURL := ""
	if policy.Controller.URL != nil {
		controllerURL = policy.Controller.URL.String()
	}
	return module.RecoveryStarted{
		EventID: event.EventID, Trigger: string(event.Trigger.Type), TriggerCode: event.Trigger.Code,
		TriggerTimeoutMS: event.Trigger.TimeoutMS,
		Attempt: module.RecoveryAttempt{
			Number: event.Attempt.Number, MaxAttempts: event.Attempt.MaxAttempts,
			ElapsedMS: event.Attempt.ElapsedMS, RemainingMS: event.Attempt.RemainingMS,
			NextAttempt: event.Attempt.NextAttempt, RetryAllowed: event.Attempt.RetryAllowed,
		},
		Directive: module.RecoveryDirective{
			Mode: event.Directive.Mode, Backend: event.Directive.Backend,
			Endpoint: event.Directive.Endpoint, Resource: event.Directive.Resource,
			PayloadSHA256: event.Directive.PayloadSHA256,
		},
		Metadata: event.Metadata, Response: moduleRecoveryResponse(event.Response),
		ControllerURL: controllerURL, ControllerTimeoutMS: policy.Controller.Timeout.Milliseconds(),
		ControllerHeaders: policy.Controller.Headers.Clone(),
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

func moduleRecoveryResponse(response *recovery.Response) *module.RecoveryResponse {
	if response == nil {
		return nil
	}
	result := &module.RecoveryResponse{StatusCode: response.StatusCode, Header: response.Headers.Clone()}
	result.Body = &module.RecoveryBody{
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

func recoveryOutcomeForError(err error) module.RecoveryOutcome {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return module.RecoveryOutcomeCanceled
	case errors.Is(err, ErrMaxAttempts), errors.Is(err, ErrRecoveryBudgetExceeded), errors.Is(err, ErrIdempotencyKeyRequired):
		return module.RecoveryOutcomeBudgetRejected
	default:
		return module.RecoveryOutcomeFailed
	}
}

func recoveryErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrMaxAttempts):
		return module.RecoveryErrorCodeMaxAttempts
	case errors.Is(err, ErrRecoveryBudgetExceeded):
		return module.RecoveryErrorCodeBudgetExceeded
	case errors.Is(err, ErrIdempotencyKeyRequired):
		return module.RecoveryErrorCodeIdempotencyRequired
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return module.RecoveryErrorCodeContextCanceled
	default:
		return module.RecoveryErrorCodeRecoveryFailed
	}
}

func cloneMetadata(in map[string][]string) map[string][]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string][]string, len(in))
	for name, values := range in {
		out[name] = append([]string(nil), values...)
	}
	return out
}
