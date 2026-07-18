package module

import (
	"net/http"

	"github.com/lwmacct/260628-directive-proxy/internal/core/lifecycle"
)

type Handler[T any] func(Context, T) error
type Mutator[T any] func(Context, *T) error

type Registrar interface {
	OnRequestStarted(Policy, Handler[lifecycle.RequestStarted])
	OnRequestBodyChunk(Policy, Handler[lifecycle.BodyChunk])
	OnRequestBodyEnded(Policy, Handler[lifecycle.RequestBodyEnded])
	OnRoundTripStarted(Policy, Handler[lifecycle.RoundTripStarted])
	OnDirectivePrepared(Policy, Handler[lifecycle.DirectivePrepared])
	OnUpstreamStarted(Policy, Handler[lifecycle.UpstreamStarted])
	OnUpstreamResponseStarted(Policy, Handler[lifecycle.ResponseStarted])
	OnUpstreamJSONChunk(Policy, Handler[lifecycle.BodyChunk])
	OnUpstreamBodyChunk(Policy, Handler[lifecycle.BodyChunk])
	OnUpstreamSSEData(Policy, Handler[lifecycle.SSEData])
	OnUpstreamBodyEnded(Policy, Handler[lifecycle.BodyEnded])
	OnRoundTripFinished(Policy, Handler[lifecycle.RoundTripFinished])
	OnRecoveryStarted(Policy, Handler[lifecycle.RecoveryStarted])
	OnRecoveryDecided(Policy, Handler[lifecycle.RecoveryDecided])
	OnRecoveryFinished(Policy, Handler[lifecycle.RecoveryFinished])
	OnDownstreamResponseStarted(Policy, Handler[lifecycle.ResponseStarted])
	OnDownstreamBodyChunk(Policy, Handler[lifecycle.BodyChunk])
	OnDownstreamSSEData(Policy, Handler[lifecycle.SSEData])
	OnDownstreamSSEComment(Policy, Handler[lifecycle.SSEComment])
	OnDownstreamBodyEnded(Policy, Handler[lifecycle.BodyEnded])
	OnRequestFinished(Policy, Handler[lifecycle.RequestFinished])
	MutateOutboundRequest(Policy, Mutator[http.Request])
	MutateOutboundBodyChunk(Policy, Mutator[lifecycle.BodyDraft])
	MutateUpstreamResponse(Policy, Mutator[lifecycle.ResponseDraft])
	MutateUpstreamBodyChunk(Policy, Mutator[lifecycle.BodyDraft])
}
