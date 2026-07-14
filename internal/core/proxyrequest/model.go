package proxyrequest

import (
	"errors"
	"net/http"
	"net/url"
	"time"
)

var (
	ErrNotFound        = errors.New("proxy request not found")
	ErrAttemptChanged  = errors.New("proxy request attempt changed")
	ErrRetryNotReady   = errors.New("proxy request retry is not ready")
	ErrRetryInProgress = errors.New("proxy request retry is already in progress")
	ErrMaxAttempts     = errors.New("proxy request maximum attempts reached")
)

type State string

const (
	StateAwaitingResponse State = "awaiting_response"
	StateRetryRequested   State = "retry_requested"
)

type ActiveRequest struct {
	TraceID          string
	State            State
	Method           string
	URL              string
	TargetURL        string
	StartedAt        time.Time
	Attempt          int
	AttemptStartedAt time.Time
	RetryableAt      time.Time
	MaxAttempts      int
}

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
	Retry(string, int) (RetryResult, error)
}

type Session interface {
	TraceID() string
	SetTargetURL(*url.URL)
	SetDirective(string, string, string, string, time.Duration)
	WrapResponseWriter(http.ResponseWriter) http.ResponseWriter
	RequestBodyChunk([]byte, int64)
	RequestBodyEnd(int64, string, bool)
	BeginAttempt(*http.Request, func()) int
	FinishAttempt(int, bool, error) AttemptAction
	Complete()
}
