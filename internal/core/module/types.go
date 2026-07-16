package module

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/requestmeta"
)

type Lifetime string

const (
	LifetimeRequest Lifetime = "request"
	LifetimeAttempt Lifetime = "attempt"
)

type Spec struct {
	ID     string          `json:"id"`
	Module string          `json:"module"`
	Config json.RawMessage `json:"config,omitempty"`
}

type Program struct {
	Request []Spec `json:"request,omitempty"`
	Attempt []Spec `json:"attempt,omitempty"`
}

type Definition interface {
	Name() string
	Compile(json.RawMessage) (Binding, error)
}

type Binding interface {
	Lifetime() Lifetime
	Open(OpenContext) (Instance, error)
}

type Instance interface {
	Mount(*Binder)
	Finish(FinishContext) error
}

type NopInstance struct{}

func (NopInstance) Mount(*Binder)              {}
func (NopInstance) Finish(FinishContext) error { return nil }

type OpenContext struct {
	TraceID   string
	Attempt   int
	StartedAt time.Time
}

type FinishCause string

const (
	FinishCompleted FinishCause = "completed"
	FinishFailed    FinishCause = "failed"
	FinishCanceled  FinishCause = "canceled"
	FinishReplaced  FinishCause = "replaced"
)

type FinishContext struct {
	Context context.Context
	EventContext
	Cause FinishCause
}

type Emitter interface {
	Emit(topic string, data map[string]any) bool
	EmitOwned(topic string, data map[string]any, release func()) bool
	EmitBorrowed(topic string, data map[string]any) bool
}

type EmissionSession interface {
	Emitter(producer string, attempt int) Emitter
	Close()
}

type EmissionProvider interface {
	Open(traceID string) EmissionSession
}

type EventContext struct {
	Context    context.Context
	TraceID    string
	Attempt    int
	ObservedAt time.Time
	Emitter    Emitter
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

type RequestStarted struct {
	Method string
	URL    string
	Host   string
	Header http.Header
}

type RequestBodyEnded struct {
	Total    int64
	SHA256   string
	Complete bool
}

type AttemptStarted struct {
	Mode     string
	Backend  string
	Endpoint string
	Key      string
}

type DirectiveResolved struct {
	Duration      time.Duration
	PayloadSHA256 string
	Target        *url.URL
	TargetChanged bool
	PlanChanged   bool
	Metadata      requestmeta.Metadata
}

type DirectiveFailed struct {
	Duration time.Duration
	Code     string
}

type MetadataBound struct{ Metadata requestmeta.Metadata }

type MetadataChanged struct {
	Bound    requestmeta.Metadata
	Observed requestmeta.Metadata
}

type UpstreamStarted struct {
	TargetURL string
	Header    http.Header
}

type ResponseStarted struct {
	StatusCode int
	Header     http.Header
	Metadata   requestmeta.Metadata
}

type BodyChunk struct{ Data []byte }

type BodyEnded struct{ Cause error }

type AttemptFinished struct{ Outcome string }

type RetryRequested struct {
	Trigger          string
	NextAttempt      int
	SelectorMetadata requestmeta.Metadata
}

type RequestFinished struct {
	Outcome    string
	StatusCode int
	Duration   time.Duration
}

type SSEData struct {
	Sequence    uint64
	Event       string
	ID          string
	Data        []byte
	RetryMillis *int64
	Truncated   bool
}

type SSEComment struct {
	Sequence uint64
	Comment  string
}

type BodyDraft struct{ Data []byte }

type ResponseDraft struct{ Response *http.Response }
