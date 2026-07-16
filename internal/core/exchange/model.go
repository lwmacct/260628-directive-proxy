package exchange

import (
	"errors"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/requestmeta"
)

var (
	ErrNotFound               = errors.New("exchange not found")
	ErrAttemptChanged         = errors.New("exchange attempt changed")
	ErrRetryNotReady          = errors.New("exchange retry is not ready")
	ErrMaxAttempts            = errors.New("exchange maximum attempts reached")
	ErrIdempotencyKeyRequired = errors.New("exchange idempotency key is required for retry")
	ErrAttemptActive          = errors.New("exchange already has an active attempt")
	ErrAttemptConfigured      = errors.New("exchange attempt module program is already configured")
)

type Phase string

const (
	PhaseWaitingBody       Phase = "waiting_body_memory"
	PhaseReadingBody       Phase = "reading_body"
	PhaseResolving         Phase = "resolving_directive"
	PhaseAwaitingResponse  Phase = "awaiting_response"
	PhaseRetryRequested    Phase = "retry_requested"
	PhaseStreamingResponse Phase = "streaming_response"
	PhaseFinished          Phase = "finished"
)

type Snapshot struct {
	TraceID           string
	HasRetryID        bool
	Metadata          requestmeta.Metadata
	Phase             Phase
	Method            string
	URL               string
	TargetURL         string
	StartedAt         time.Time
	Attempt           int
	AttemptStartedAt  time.Time
	UpstreamStartedAt time.Time
	MaxAttempts       int
}

type Trigger string

const (
	TriggerRequesterAPI Trigger = "requester_api"
	TriggerAdminAPI     Trigger = "admin_api"
)

type RetryResult struct {
	Exchange    Snapshot
	NextAttempt int
}

type Decision uint8

const (
	DecisionReturn Decision = iota
	DecisionRetry
)

type AttemptSource struct {
	Mode     string
	Backend  string
	Endpoint string
	Key      string
}

type ManagerOptions struct {
	MaxAttempts      int
	CommandRetention time.Duration
}
