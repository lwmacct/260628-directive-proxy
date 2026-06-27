package eventbus

import (
	"context"
	"strings"
	"time"
)

type Type string

const (
	TypeCaptureRequest      Type = "capture.request"
	TypeCaptureRequestBody  Type = "capture.request_body"
	TypeCaptureResponse     Type = "capture.response"
	TypeCaptureResponseBody Type = "capture.response_body"
	TypeStreamEvent         Type = "stream.event"
	TypeStreamEnd           Type = "stream.end"
	TypeUsage               Type = "usage"
)

type Class string

const (
	ClassCapture Class = "capture"
	ClassStream  Class = "stream"
	ClassUsage   Class = "usage"
)

type Event struct {
	EventID   string         `json:"event_id"`
	RequestID string         `json:"request_id"`
	Type      Type           `json:"type"`
	Timestamp time.Time      `json:"timestamp"`
	Labels    map[string]any `json:"labels,omitempty"`
	Runtime   Runtime        `json:"runtime"`
	Data      any            `json:"data,omitempty"`
}

type Runtime struct {
	IncomingRemoteAddr string              `json:"incoming_remote_addr,omitempty"`
	ClientRequestID    string              `json:"client_request_id,omitempty"`
	Headers            map[string][]string `json:"headers,omitempty"`
}

func (e Event) Class() Class {
	switch {
	case strings.HasPrefix(string(e.Type), "capture."):
		return ClassCapture
	case strings.HasPrefix(string(e.Type), "stream."):
		return ClassStream
	case e.Type == TypeUsage:
		return ClassUsage
	default:
		return ""
	}
}

type Publisher interface {
	Publish(context.Context, Event) error
	Close(context.Context) error
}

type NopPublisher struct{}

func (NopPublisher) Publish(context.Context, Event) error { return nil }
func (NopPublisher) Close(context.Context) error          { return nil }
