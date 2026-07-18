package program

import (
	"net/http"

	"github.com/lwmacct/260628-directive-proxy/internal/core/lifecycle"
	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
)

type subscription[T any] struct {
	policy module.Policy
	handle module.Handler[T]
}

type mutation[T any] struct {
	policy module.Policy
	handle module.Mutator[T]
}

type binder struct {
	requestStarted       []subscription[lifecycle.RequestStarted]
	requestBodyChunk     []subscription[lifecycle.BodyChunk]
	requestBodyEnded     []subscription[lifecycle.RequestBodyEnded]
	attemptStarted       []subscription[lifecycle.AttemptStarted]
	directivePrepared    []subscription[lifecycle.DirectivePrepared]
	upstreamStarted      []subscription[lifecycle.UpstreamStarted]
	upstreamResponse     []subscription[lifecycle.ResponseStarted]
	upstreamBodyChunk    []subscription[lifecycle.BodyChunk]
	upstreamJSONChunk    []subscription[lifecycle.BodyChunk]
	upstreamSSEData      []subscription[lifecycle.SSEData]
	upstreamBodyEnded    []subscription[lifecycle.BodyEnded]
	attemptFinished      []subscription[lifecycle.AttemptFinished]
	recoveryStarted      []subscription[lifecycle.RecoveryStarted]
	recoveryDecided      []subscription[lifecycle.RecoveryDecided]
	recoveryFinished     []subscription[lifecycle.RecoveryFinished]
	downstreamResponse   []subscription[lifecycle.ResponseStarted]
	downstreamBodyChunk  []subscription[lifecycle.BodyChunk]
	downstreamSSEData    []subscription[lifecycle.SSEData]
	downstreamSSEComment []subscription[lifecycle.SSEComment]
	downstreamBodyEnded  []subscription[lifecycle.BodyEnded]
	requestFinished      []subscription[lifecycle.RequestFinished]
	outboundRequest      []mutation[http.Request]
	outboundBodyChunk    []mutation[lifecycle.BodyDraft]
	upstreamDraft        []mutation[lifecycle.ResponseDraft]
	upstreamBodyDraft    []mutation[lifecycle.BodyDraft]
}

func appendSubscription[T any](target *[]subscription[T], policy module.Policy, handle module.Handler[T]) {
	if handle != nil {
		*target = append(*target, subscription[T]{policy: normalizePolicy(policy), handle: handle})
	}
}

func appendMutation[T any](target *[]mutation[T], policy module.Policy, handle module.Mutator[T]) {
	if handle != nil {
		policy = normalizePolicy(policy)
		policy.Barrier = module.BarrierBeforeCommit
		*target = append(*target, mutation[T]{policy: policy, handle: handle})
	}
}

func (b *binder) OnRequestStarted(policy module.Policy, handle module.Handler[lifecycle.RequestStarted]) {
	appendSubscription(&b.requestStarted, policy, handle)
}
func (b *binder) OnRequestBodyChunk(policy module.Policy, handle module.Handler[lifecycle.BodyChunk]) {
	appendSubscription(&b.requestBodyChunk, policy, handle)
}
func (b *binder) OnRequestBodyEnded(policy module.Policy, handle module.Handler[lifecycle.RequestBodyEnded]) {
	appendSubscription(&b.requestBodyEnded, policy, handle)
}
func (b *binder) OnAttemptStarted(policy module.Policy, handle module.Handler[lifecycle.AttemptStarted]) {
	appendSubscription(&b.attemptStarted, policy, handle)
}
func (b *binder) OnDirectivePrepared(policy module.Policy, handle module.Handler[lifecycle.DirectivePrepared]) {
	appendSubscription(&b.directivePrepared, policy, handle)
}
func (b *binder) OnUpstreamStarted(policy module.Policy, handle module.Handler[lifecycle.UpstreamStarted]) {
	appendSubscription(&b.upstreamStarted, policy, handle)
}
func (b *binder) OnUpstreamResponseStarted(policy module.Policy, handle module.Handler[lifecycle.ResponseStarted]) {
	appendSubscription(&b.upstreamResponse, policy, handle)
}
func (b *binder) OnUpstreamJSONChunk(policy module.Policy, handle module.Handler[lifecycle.BodyChunk]) {
	appendSubscription(&b.upstreamJSONChunk, policy, handle)
}
func (b *binder) OnUpstreamBodyChunk(policy module.Policy, handle module.Handler[lifecycle.BodyChunk]) {
	appendSubscription(&b.upstreamBodyChunk, policy, handle)
}
func (b *binder) OnUpstreamSSEData(policy module.Policy, handle module.Handler[lifecycle.SSEData]) {
	appendSubscription(&b.upstreamSSEData, policy, handle)
}
func (b *binder) OnUpstreamBodyEnded(policy module.Policy, handle module.Handler[lifecycle.BodyEnded]) {
	appendSubscription(&b.upstreamBodyEnded, policy, handle)
}
func (b *binder) OnAttemptFinished(policy module.Policy, handle module.Handler[lifecycle.AttemptFinished]) {
	appendSubscription(&b.attemptFinished, policy, handle)
}
func (b *binder) OnRecoveryStarted(policy module.Policy, handle module.Handler[lifecycle.RecoveryStarted]) {
	appendSubscription(&b.recoveryStarted, policy, handle)
}
func (b *binder) OnRecoveryDecided(policy module.Policy, handle module.Handler[lifecycle.RecoveryDecided]) {
	appendSubscription(&b.recoveryDecided, policy, handle)
}
func (b *binder) OnRecoveryFinished(policy module.Policy, handle module.Handler[lifecycle.RecoveryFinished]) {
	appendSubscription(&b.recoveryFinished, policy, handle)
}
func (b *binder) OnDownstreamResponseStarted(policy module.Policy, handle module.Handler[lifecycle.ResponseStarted]) {
	appendSubscription(&b.downstreamResponse, policy, handle)
}
func (b *binder) OnDownstreamBodyChunk(policy module.Policy, handle module.Handler[lifecycle.BodyChunk]) {
	appendSubscription(&b.downstreamBodyChunk, policy, handle)
}
func (b *binder) OnDownstreamSSEData(policy module.Policy, handle module.Handler[lifecycle.SSEData]) {
	appendSubscription(&b.downstreamSSEData, policy, handle)
}
func (b *binder) OnDownstreamSSEComment(policy module.Policy, handle module.Handler[lifecycle.SSEComment]) {
	appendSubscription(&b.downstreamSSEComment, policy, handle)
}
func (b *binder) OnDownstreamBodyEnded(policy module.Policy, handle module.Handler[lifecycle.BodyEnded]) {
	appendSubscription(&b.downstreamBodyEnded, policy, handle)
}
func (b *binder) OnRequestFinished(policy module.Policy, handle module.Handler[lifecycle.RequestFinished]) {
	appendSubscription(&b.requestFinished, policy, handle)
}
func (b *binder) MutateOutboundRequest(policy module.Policy, handle module.Mutator[http.Request]) {
	appendMutation(&b.outboundRequest, policy, handle)
}
func (b *binder) MutateOutboundBodyChunk(policy module.Policy, handle module.Mutator[lifecycle.BodyDraft]) {
	appendMutation(&b.outboundBodyChunk, policy, handle)
}
func (b *binder) MutateUpstreamResponse(policy module.Policy, handle module.Mutator[lifecycle.ResponseDraft]) {
	appendMutation(&b.upstreamDraft, policy, handle)
}
func (b *binder) MutateUpstreamBodyChunk(policy module.Policy, handle module.Mutator[lifecycle.BodyDraft]) {
	appendMutation(&b.upstreamBodyDraft, policy, handle)
}

func normalizePolicy(policy module.Policy) module.Policy {
	if policy.Executor == "" {
		policy.Executor = module.ExecutorCaller
	}
	if policy.Barrier == "" {
		if policy.Executor == module.ExecutorCaller {
			policy.Barrier = module.BarrierBeforeCommit
		} else {
			policy.Barrier = module.BarrierScopeEnd
		}
	}
	if policy.Overflow == "" {
		policy.Overflow = module.OverflowBlock
	}
	if policy.Capacity <= 0 {
		policy.Capacity = 128
	}
	return policy
}
