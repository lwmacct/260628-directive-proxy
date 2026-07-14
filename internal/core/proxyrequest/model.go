package proxyrequest

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/requestmeta"
)

var (
	ErrNotFound        = errors.New("proxy request not found")
	ErrAttemptChanged  = errors.New("proxy request attempt changed")
	ErrRetryNotReady   = errors.New("proxy request retry is not ready")
	ErrRetryInProgress = errors.New("proxy request retry is already in progress")
	ErrMaxAttempts     = errors.New("proxy request maximum attempts reached")
	ErrAmbiguous       = errors.New("proxy request metadata is ambiguous")
	ErrInvalidMetadata = errors.New("proxy request metadata is invalid")
)

type State string

const (
	StateResolvingDirective State = "resolving_directive"
	StateBufferingBody      State = "buffering_body"
	StateAwaitingResponse   State = "awaiting_response"
	StateRetryRequested     State = "retry_requested"
)

type ActiveRequest struct {
	TraceID           string
	Metadata          requestmeta.Metadata
	State             State
	Method            string
	URL               string
	TargetURL         string
	StartedAt         time.Time
	Attempt           int
	AttemptStartedAt  time.Time
	UpstreamStartedAt time.Time
	RetryableAt       time.Time
	MaxAttempts       int
}

type RetryTrigger string

const (
	RetryTriggerRequesterAPI RetryTrigger = "requester_api"
	RetryTriggerControlAPI   RetryTrigger = "control_api"
)

type RetryResult struct {
	Request     ActiveRequest
	NextAttempt int
}

type AttemptAction uint8

const (
	AttemptReturn AttemptAction = iota
	AttemptRetry
)

type Tracker interface {
	Start(*http.Request) Session
	ListActive() []ActiveRequest
	GetActive(string) (ActiveRequest, bool)
	RetryByTraceID(string, int, RetryTrigger) (RetryResult, error)
	RetryByMetadata(requestmeta.Selector, int, RetryTrigger) (RetryResult, error)
}

type Session interface {
	TraceID() string
	WrapResponseWriter(http.ResponseWriter) http.ResponseWriter
	RequestBodyChunk([]byte, int64)
	RequestBodyEnd(int64, string, bool)
	BeginAttempt(func(), string, string, string, string) int
	BindMetadata(int, requestmeta.Metadata) bool
	BeginBodyBuffering(int)
	DirectiveResolved(int, *url.URL, time.Duration, string, bool, bool)
	DirectiveFailed(int, time.Duration, string)
	ConfigureAttempt(int, map[string][]byte) error
	BeginUpstream(int, *http.Request) bool
	FinishAttempt(int, bool, error) AttemptAction
	ObserveUpstreamResponse(int, *http.Response)
	Complete()
}
