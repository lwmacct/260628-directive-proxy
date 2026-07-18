package exchange

import (
	"errors"
	"net/url"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/metadata"
	"github.com/lwmacct/260628-directive-proxy/internal/core/program"
)

var (
	ErrMaxRoundTrips             = errors.New("exchange maximum round trips reached")
	ErrRecoveryBudgetExceeded    = errors.New("exchange recovery time budget is exhausted")
	ErrIdempotencyKeyRequired    = errors.New("exchange idempotency key is required for retry")
	ErrRecoveryFailed            = errors.New("exchange recovery failed")
	ErrRoundTripActive           = errors.New("exchange already has an active round trip")
	ErrRoundTripScopeOpened      = errors.New("exchange round-trip scope is already open")
	ErrExchangeNotConfigured     = errors.New("exchange is not configured")
	ErrExchangeConfigured        = errors.New("exchange is already configured")
	ErrProgramRuntimeUnavailable = errors.New("exchange program runtime is unavailable")
	ErrDirectiveInvalid          = errors.New("exchange directive is invalid")
)

type Phase string

const (
	PhaseStartingBody       Phase = "starting_body_stream"
	PhaseStreamingRequest   Phase = "streaming_request"
	PhasePreparingRoundTrip Phase = "preparing_round_trip"
	PhaseAwaitingResponse   Phase = "awaiting_response"
	PhaseRecovering         Phase = "recovering"
	PhaseRetryRequested     Phase = "retry_requested"
	PhaseStreamingResponse  Phase = "streaming_response"
	PhaseFinished           Phase = "finished"
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
	MaxRoundTrips int
}

type RecoveryContext struct {
	TraceID       string
	RoundTrip     int
	MaxRoundTrips int
	StartedAt     time.Time
	Elapsed       time.Duration
	Remaining     time.Duration
	NextRoundTrip int
	RetryAllowed  bool
	Metadata      metadata.Set
}
