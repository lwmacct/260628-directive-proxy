package module

import (
	"context"
	"encoding/json"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/event"
	"github.com/lwmacct/260628-directive-proxy/internal/core/metadata"
)

type Lifetime string

const (
	LifetimeExchange  Lifetime = "exchange"
	LifetimeRoundTrip Lifetime = "round_trip"
)

type Definition interface {
	Name() string
	Lifetime() Lifetime
	Compile(json.RawMessage) (Binding, error)
}

// Binding is immutable and safe for concurrent Open calls after Compile returns.
type Binding interface {
	Open(OpenContext) (Instance, error)
}

type Instance interface {
	Bind(Registrar)
	Finish(FinishContext) error
}

type NopInstance struct{}

func (NopInstance) Bind(Registrar)             {}
func (NopInstance) Finish(FinishContext) error { return nil }

type OpenContext struct {
	TraceID   string
	Metadata  metadata.Set
	Lifetime  Lifetime
	RoundTrip int
	StartedAt time.Time
}

type FinishCause string

const (
	FinishCompleted FinishCause = "completed"
	FinishFailed    FinishCause = "failed"
	FinishCanceled  FinishCause = "canceled"
	FinishReplaced  FinishCause = "replaced"
)

type Context struct {
	Context    context.Context
	TraceID    string
	Metadata   metadata.Set
	RoundTrip  int
	EventID    string
	Sequence   uint64
	ObservedAt time.Time
	Emitter    event.Emitter
}

type FinishContext struct {
	Context
	Cause FinishCause
}

type Executor string

const (
	ExecutorCaller      Executor = "caller"
	ExecutorOrderedLane Executor = "ordered_lane"
)

type Barrier string

const (
	BarrierBeforeCommit Barrier = "before_commit"
	BarrierScopeEnd     Barrier = "scope_end"
	BarrierNone         Barrier = "none"
)

type Overflow string

const (
	OverflowBlock       Overflow = "block"
	OverflowDrop        Overflow = "drop"
	OverflowFailRequest Overflow = "fail_request"
)

type Policy struct {
	Executor Executor
	Barrier  Barrier
	Overflow Overflow
	Capacity int
}

func SyncPolicy() Policy {
	return Policy{Executor: ExecutorCaller, Barrier: BarrierBeforeCommit, Overflow: OverflowFailRequest}
}

func AsyncPolicy(overflow Overflow) Policy {
	return Policy{Executor: ExecutorOrderedLane, Barrier: BarrierScopeEnd, Overflow: overflow, Capacity: 128}
}

func AsyncBarrierPolicy(overflow Overflow) Policy {
	return Policy{Executor: ExecutorOrderedLane, Barrier: BarrierBeforeCommit, Overflow: overflow, Capacity: 128}
}
