package exchange

import (
	"errors"
	"net/url"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/metadata"
	"github.com/lwmacct/260628-directive-proxy/internal/core/program"
)

var (
	ErrMaxAttempts               = errors.New("exchange maximum attempts reached")
	ErrRecoveryBudgetExceeded    = errors.New("exchange recovery time budget is exhausted")
	ErrIdempotencyKeyRequired    = errors.New("exchange idempotency key is required for retry")
	ErrRecoveryFailed            = errors.New("exchange recovery failed")
	ErrAttemptActive             = errors.New("exchange already has an active attempt")
	ErrAttemptScopeOpened        = errors.New("exchange attempt scope is already open")
	ErrExchangeNotConfigured     = errors.New("exchange is not configured")
	ErrExchangeConfigured        = errors.New("exchange is already configured")
	ErrProgramRuntimeUnavailable = errors.New("exchange program runtime is unavailable")
	ErrDirectiveInvalid          = errors.New("exchange directive is invalid")
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
}

type Configuration struct {
	Directive DirectiveInfo
	Metadata  metadata.Set
	Program   *program.Executable
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
	Metadata     metadata.Set
}
