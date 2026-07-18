package lifecycle

import (
	"net/http"
	"net/url"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/requestmeta"
)

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
	Mode          string
	Backend       string
	Endpoint      string
	Resource      string
	PayloadSHA256 string
	Target        *url.URL
	Metadata      requestmeta.Metadata
}

type DirectivePrepared struct {
	Mode          string
	Backend       string
	Endpoint      string
	Resource      string
	Duration      time.Duration
	PayloadSHA256 string
	Target        *url.URL
	Metadata      requestmeta.Metadata
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

type Outcome string

const (
	OutcomeCompleted            Outcome = "completed"
	OutcomeInterrupted          Outcome = "interrupted"
	OutcomeClientCanceled       Outcome = "client_canceled"
	OutcomeEndedWithoutResponse Outcome = "ended_without_response"
	OutcomeCanceledForRetry     Outcome = "canceled_for_retry"
	OutcomeTransportError       Outcome = "transport_error"
)

type AttemptFinished struct{ Outcome Outcome }

type RecoveryAction string

const (
	RecoveryActionRetry   RecoveryAction = "retry"
	RecoveryActionForward RecoveryAction = "forward"
	RecoveryActionFail    RecoveryAction = "fail"
)

type RecoveryOutcome string

const (
	RecoveryOutcomeRetryRequested  RecoveryOutcome = "retry_requested"
	RecoveryOutcomeForwarded       RecoveryOutcome = "forwarded"
	RecoveryOutcomeFailed          RecoveryOutcome = "failed"
	RecoveryOutcomeControllerError RecoveryOutcome = "controller_error"
	RecoveryOutcomeInvalidDecision RecoveryOutcome = "invalid_decision"
	RecoveryOutcomeBudgetRejected  RecoveryOutcome = "budget_rejected"
	RecoveryOutcomeCanceled        RecoveryOutcome = "canceled"
)

const (
	RecoveryErrorCodeController          = "controller_error"
	RecoveryErrorCodeInvalidDecision     = "invalid_decision"
	RecoveryErrorCodeRetryNotAllowed     = "retry_not_allowed"
	RecoveryErrorCodeBudgetExceeded      = "recovery_budget_exceeded"
	RecoveryErrorCodeContextCanceled     = "context_canceled"
	RecoveryErrorCodeMaxAttempts         = "max_attempts"
	RecoveryErrorCodeIdempotencyRequired = "idempotency_key_required"
	RecoveryErrorCodeRecoveryFailed      = "recovery_failed"
	RecoveryErrorCodeControllerFail      = "controller_fail"
)

type RecoveryAttempt struct {
	Number       int
	MaxAttempts  int
	ElapsedMS    int64
	RemainingMS  int64
	NextAttempt  int
	RetryAllowed bool
}

type RecoveryDirective struct {
	Mode          string
	Backend       string
	Endpoint      string
	Resource      string
	PayloadSHA256 string
}

type RecoveryBody struct {
	Encoding  string
	Data      string
	Size      int64
	Truncated bool
}

type RecoveryResponse struct {
	StatusCode int
	Header     http.Header
	Body       *RecoveryBody
}

type RecoveryStarted struct {
	EventID             string
	Trigger             string
	TriggerCode         string
	TriggerTimeoutMS    int64
	Attempt             RecoveryAttempt
	Directive           RecoveryDirective
	Metadata            requestmeta.Metadata
	Response            *RecoveryResponse
	ControllerURL       string
	ControllerTimeoutMS int64
	ControllerHeaders   http.Header
}

type RecoveryDecided struct {
	EventID string
	Action  RecoveryAction
	AfterMS int64
}

type RecoveryFinished struct {
	EventID     string
	Outcome     RecoveryOutcome
	Action      RecoveryAction
	AfterMS     int64
	NextAttempt int
	ErrorCode   string
	Error       string
}

type RequestFinished struct {
	Outcome    Outcome
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
