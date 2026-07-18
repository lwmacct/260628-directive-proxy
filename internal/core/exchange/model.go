package exchange

import (
	"errors"
	"net/url"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/requestmeta"
)

var (
	ErrMaxAttempts            = errors.New("exchange maximum attempts reached")
	ErrRecoveryBudgetExceeded = errors.New("exchange recovery time budget is exhausted")
	ErrIdempotencyKeyRequired = errors.New("exchange idempotency key is required for retry")
	ErrRecoveryFailed         = errors.New("exchange recovery failed")
	ErrAttemptActive          = errors.New("exchange already has an active attempt")
	ErrAttemptScopeOpened     = errors.New("exchange attempt scope is already open")
	ErrProgramNotConfigured   = errors.New("exchange program is not configured")
	ErrDirectiveNotPrepared   = errors.New("exchange directive is not prepared")
	ErrDirectiveAlreadySet    = errors.New("exchange directive is already prepared")
	ErrDirectiveInvalid       = errors.New("exchange directive is invalid")
)

type Phase string

const (
	PhaseStartingBody      Phase = "starting_body_stream"
	PhaseStreamingRequest  Phase = "streaming_request"
	PhasePreparingAttempt  Phase = "preparing_attempt"
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

type DirectiveInfo struct {
	Mode          string
	Backend       string
	Endpoint      string
	Resource      string
	PayloadSHA256 string
	Duration      time.Duration
	Target        *url.URL
	Metadata      requestmeta.Metadata
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
