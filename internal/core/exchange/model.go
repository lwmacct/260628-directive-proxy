package exchange

import (
	"errors"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/requestmeta"
)

var (
	ErrMaxAttempts            = errors.New("exchange maximum attempts reached")
	ErrRecoveryBudgetExceeded = errors.New("exchange recovery time budget is exhausted")
	ErrIdempotencyKeyRequired = errors.New("exchange idempotency key is required for retry")
	ErrAttemptActive          = errors.New("exchange already has an active attempt")
	ErrAttemptConfigured      = errors.New("exchange attempt module program is already configured")
)

type Phase string

const (
	PhaseStartingBody      Phase = "starting_body_stream"
	PhaseStreamingRequest  Phase = "streaming_request"
	PhaseResolving         Phase = "resolving_directive"
	PhaseAwaitingResponse  Phase = "awaiting_response"
	PhaseRecovering        Phase = "recovering"
	PhaseRetryRequested    Phase = "retry_requested"
	PhaseStreamingResponse Phase = "streaming_response"
	PhaseFinished          Phase = "finished"
)

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
	MaxAttempts int
}

type RecoveryContext struct {
	TraceID      string
	Attempt      int
	MaxAttempts  int
	StartedAt    time.Time
	Elapsed      time.Duration
	Remaining    time.Duration
	NextAttempt  int
	RetryAllowed bool
	Metadata     requestmeta.Metadata
}
