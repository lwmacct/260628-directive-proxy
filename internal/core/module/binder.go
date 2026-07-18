package module

import "net/http"

type Handler[T any] func(EventContext, T) error
type Mutator[T any] func(EventContext, *T) error

type subscription[T any] struct {
	policy Policy
	handle Handler[T]
}

type mutation[T any] struct {
	policy Policy
	handle Mutator[T]
}

type Binder struct {
	requestStarted       []subscription[RequestStarted]
	requestBodyChunk     []subscription[BodyChunk]
	requestBodyEnded     []subscription[RequestBodyEnded]
	attemptStarted       []subscription[AttemptStarted]
	directiveResolved    []subscription[DirectiveResolved]
	directiveFailed      []subscription[DirectiveFailed]
	metadataBound        []subscription[MetadataBound]
	metadataChanged      []subscription[MetadataChanged]
	upstreamStarted      []subscription[UpstreamStarted]
	upstreamResponse     []subscription[ResponseStarted]
	upstreamBodyChunk    []subscription[BodyChunk]
	upstreamJSONChunk    []subscription[BodyChunk]
	upstreamSSEData      []subscription[SSEData]
	upstreamBodyEnded    []subscription[BodyEnded]
	attemptFinished      []subscription[AttemptFinished]
	recoveryStarted      []subscription[RecoveryStarted]
	recoveryDecided      []subscription[RecoveryDecided]
	recoveryFinished     []subscription[RecoveryFinished]
	downstreamResponse   []subscription[ResponseStarted]
	downstreamBodyChunk  []subscription[BodyChunk]
	downstreamSSEData    []subscription[SSEData]
	downstreamSSEComment []subscription[SSEComment]
	downstreamBodyEnded  []subscription[BodyEnded]
	requestFinished      []subscription[RequestFinished]
	outboundRequest      []mutation[http.Request]
	outboundBodyChunk    []mutation[BodyDraft]
	upstreamDraft        []mutation[ResponseDraft]
	upstreamBodyDraft    []mutation[BodyDraft]
}

func appendSubscription[T any](target *[]subscription[T], policy Policy, handle Handler[T]) {
	if handle != nil {
		*target = append(*target, subscription[T]{policy: normalizePolicy(policy), handle: handle})
	}
}

func appendMutation[T any](target *[]mutation[T], policy Policy, handle Mutator[T]) {
	if handle != nil {
		policy = normalizePolicy(policy)
		policy.Barrier = BarrierBeforeCommit
		*target = append(*target, mutation[T]{policy: policy, handle: handle})
	}
}

func (b *Binder) OnRequestStarted(policy Policy, handle Handler[RequestStarted]) {
	appendSubscription(&b.requestStarted, policy, handle)
}
func (b *Binder) OnRequestBodyChunk(policy Policy, handle Handler[BodyChunk]) {
	appendSubscription(&b.requestBodyChunk, policy, handle)
}
func (b *Binder) OnRequestBodyEnded(policy Policy, handle Handler[RequestBodyEnded]) {
	appendSubscription(&b.requestBodyEnded, policy, handle)
}
func (b *Binder) OnAttemptStarted(policy Policy, handle Handler[AttemptStarted]) {
	appendSubscription(&b.attemptStarted, policy, handle)
}
func (b *Binder) OnDirectiveResolved(policy Policy, handle Handler[DirectiveResolved]) {
	appendSubscription(&b.directiveResolved, policy, handle)
}
func (b *Binder) OnDirectiveFailed(policy Policy, handle Handler[DirectiveFailed]) {
	appendSubscription(&b.directiveFailed, policy, handle)
}
func (b *Binder) OnMetadataBound(policy Policy, handle Handler[MetadataBound]) {
	appendSubscription(&b.metadataBound, policy, handle)
}
func (b *Binder) OnMetadataChanged(policy Policy, handle Handler[MetadataChanged]) {
	appendSubscription(&b.metadataChanged, policy, handle)
}
func (b *Binder) OnUpstreamStarted(policy Policy, handle Handler[UpstreamStarted]) {
	appendSubscription(&b.upstreamStarted, policy, handle)
}
func (b *Binder) OnUpstreamResponseStarted(policy Policy, handle Handler[ResponseStarted]) {
	appendSubscription(&b.upstreamResponse, policy, handle)
}
func (b *Binder) OnUpstreamJSONChunk(policy Policy, handle Handler[BodyChunk]) {
	appendSubscription(&b.upstreamJSONChunk, policy, handle)
}
func (b *Binder) OnUpstreamBodyChunk(policy Policy, handle Handler[BodyChunk]) {
	appendSubscription(&b.upstreamBodyChunk, policy, handle)
}
func (b *Binder) OnUpstreamSSEData(policy Policy, handle Handler[SSEData]) {
	appendSubscription(&b.upstreamSSEData, policy, handle)
}
func (b *Binder) OnUpstreamBodyEnded(policy Policy, handle Handler[BodyEnded]) {
	appendSubscription(&b.upstreamBodyEnded, policy, handle)
}
func (b *Binder) OnAttemptFinished(policy Policy, handle Handler[AttemptFinished]) {
	appendSubscription(&b.attemptFinished, policy, handle)
}
func (b *Binder) OnRecoveryStarted(policy Policy, handle Handler[RecoveryStarted]) {
	appendSubscription(&b.recoveryStarted, policy, handle)
}
func (b *Binder) OnRecoveryDecided(policy Policy, handle Handler[RecoveryDecided]) {
	appendSubscription(&b.recoveryDecided, policy, handle)
}
func (b *Binder) OnRecoveryFinished(policy Policy, handle Handler[RecoveryFinished]) {
	appendSubscription(&b.recoveryFinished, policy, handle)
}
func (b *Binder) OnDownstreamResponseStarted(policy Policy, handle Handler[ResponseStarted]) {
	appendSubscription(&b.downstreamResponse, policy, handle)
}
func (b *Binder) OnDownstreamBodyChunk(policy Policy, handle Handler[BodyChunk]) {
	appendSubscription(&b.downstreamBodyChunk, policy, handle)
}
func (b *Binder) OnDownstreamSSEData(policy Policy, handle Handler[SSEData]) {
	appendSubscription(&b.downstreamSSEData, policy, handle)
}
func (b *Binder) OnDownstreamSSEComment(policy Policy, handle Handler[SSEComment]) {
	appendSubscription(&b.downstreamSSEComment, policy, handle)
}
func (b *Binder) OnDownstreamBodyEnded(policy Policy, handle Handler[BodyEnded]) {
	appendSubscription(&b.downstreamBodyEnded, policy, handle)
}
func (b *Binder) OnRequestFinished(policy Policy, handle Handler[RequestFinished]) {
	appendSubscription(&b.requestFinished, policy, handle)
}
func (b *Binder) MutateOutboundRequest(policy Policy, handle Mutator[http.Request]) {
	appendMutation(&b.outboundRequest, policy, handle)
}
func (b *Binder) MutateOutboundBodyChunk(policy Policy, handle Mutator[BodyDraft]) {
	appendMutation(&b.outboundBodyChunk, policy, handle)
}
func (b *Binder) MutateUpstreamResponse(policy Policy, handle Mutator[ResponseDraft]) {
	appendMutation(&b.upstreamDraft, policy, handle)
}
func (b *Binder) MutateUpstreamBodyChunk(policy Policy, handle Mutator[BodyDraft]) {
	appendMutation(&b.upstreamBodyDraft, policy, handle)
}

func normalizePolicy(policy Policy) Policy {
	if policy.Executor == "" {
		policy.Executor = ExecutorCaller
	}
	if policy.Barrier == "" {
		if policy.Executor == ExecutorCaller {
			policy.Barrier = BarrierBeforeCommit
		} else {
			policy.Barrier = BarrierScopeEnd
		}
	}
	if policy.Overflow == "" {
		policy.Overflow = OverflowBlock
	}
	if policy.Capacity <= 0 {
		policy.Capacity = 128
	}
	return policy
}
