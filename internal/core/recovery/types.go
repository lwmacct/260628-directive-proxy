package recovery

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/url"
	"time"
)

const Protocol = "dproxy.recovery.v1"

type TriggerType string

const (
	TriggerUnexpectedStatus      TriggerType = "unexpected_status"
	TriggerResponseHeaderTimeout TriggerType = "response_header_timeout"
	TriggerTransportError        TriggerType = "transport_error"
	TriggerDirectiveError        TriggerType = "directive_error"
)

type Action string

const (
	ActionRetry   Action = "retry"
	ActionForward Action = "forward"
	ActionFail    Action = "fail"
)

type StatusRange struct {
	From int
	To   int
}

type ControllerSpec struct {
	URL     *url.URL
	Headers http.Header
	Timeout time.Duration
}

type UnexpectedStatusPolicy struct {
	Expected         []StatusRange
	CaptureBodyBytes int64
}

type TriggerPolicy struct {
	ResponseHeaderTimeout time.Duration
	UnexpectedStatus      *UnexpectedStatusPolicy
	TransportError        bool
	DirectiveError        bool
}

type Budget struct {
	MaxAttempts int
	MaxElapsed  time.Duration
}

type Policy struct {
	Controller ControllerSpec
	Triggers   TriggerPolicy
	Budget     Budget
}

func ClonePolicy(in *Policy) *Policy {
	if in == nil {
		return nil
	}
	out := *in
	if in.Controller.URL != nil {
		value := *in.Controller.URL
		out.Controller.URL = &value
	}
	out.Controller.Headers = in.Controller.Headers.Clone()
	if in.Triggers.UnexpectedStatus != nil {
		status := *in.Triggers.UnexpectedStatus
		status.Expected = append([]StatusRange(nil), status.Expected...)
		out.Triggers.UnexpectedStatus = &status
	}
	return &out
}

func (policy *UnexpectedStatusPolicy) Matches(status int) bool {
	if policy == nil {
		return false
	}
	for _, expected := range policy.Expected {
		if status >= expected.From && status <= expected.To {
			return false
		}
	}
	return true
}

type AttemptInfo struct {
	Number       int   `json:"number"`
	MaxAttempts  int   `json:"max_attempts"`
	ElapsedMS    int64 `json:"elapsed_ms"`
	RemainingMS  int64 `json:"remaining_ms,omitempty"`
	NextAttempt  int   `json:"next_attempt,omitempty"`
	RetryAllowed bool  `json:"retry_allowed"`
}

type Trigger struct {
	Type      TriggerType `json:"type"`
	TimeoutMS int64       `json:"timeout_ms,omitempty"`
	Code      string      `json:"code,omitempty"`
}

type DirectiveInfo struct {
	Mode          string `json:"mode"`
	Backend       string `json:"backend,omitempty"`
	Endpoint      string `json:"endpoint,omitempty"`
	Key           string `json:"key,omitempty"`
	PayloadSHA256 string `json:"payload_sha256,omitempty"`
}

type CapturedBody struct {
	Encoding  string `json:"encoding"`
	Data      string `json:"data"`
	Size      int64  `json:"size"`
	Truncated bool   `json:"truncated"`
}

func NewCapturedBody(data []byte, size int64, truncated bool) CapturedBody {
	return CapturedBody{
		Encoding:  "base64",
		Data:      base64.StdEncoding.EncodeToString(data),
		Size:      size,
		Truncated: truncated,
	}
}

type Response struct {
	StatusCode int          `json:"status_code"`
	Headers    http.Header  `json:"headers"`
	Body       CapturedBody `json:"body"`
}

type Event struct {
	Protocol   string              `json:"protocol"`
	EventID    string              `json:"event_id"`
	TraceID    string              `json:"trace_id"`
	ObservedAt time.Time           `json:"observed_at"`
	Attempt    AttemptInfo         `json:"attempt"`
	Trigger    Trigger             `json:"trigger"`
	Directive  DirectiveInfo       `json:"directive"`
	Metadata   map[string][]string `json:"metadata,omitempty"`
	Response   *Response           `json:"response,omitempty"`
}

type Decision struct {
	Action  Action `json:"action"`
	AfterMS int64  `json:"after_ms,omitempty"`
}

type Controller interface {
	Decide(context.Context, ControllerSpec, Event) (Decision, error)
}
