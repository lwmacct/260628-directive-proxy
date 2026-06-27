package capture

import (
	"net/http"
	"time"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/eventbus"
)

const (
	EventTypeRequest      = eventbus.TypeCaptureRequest
	EventTypeRequestBody  = eventbus.TypeCaptureRequestBody
	EventTypeResponse     = eventbus.TypeCaptureResponse
	EventTypeResponseBody = eventbus.TypeCaptureResponseBody
	EventTypeStreamEvent  = eventbus.TypeStreamEvent
	EventTypeStreamEnd    = eventbus.TypeStreamEnd
)

type Body struct {
	Content     any    `json:"content"`
	Size        int    `json:"size"`
	ContentType string `json:"content_type"`
	Encoding    string `json:"encoding"`
}

type RequestData struct {
	Method        string      `json:"method"`
	URL           string      `json:"url"`
	RemoteAddr    string      `json:"remote_addr"`
	HTTPVersion   string      `json:"http_version"`
	Headers       http.Header `json:"headers,omitempty"`
	Size          int         `json:"size"`
	CaptureReason string      `json:"capture_reason,omitempty"`
	AbnormalType  string      `json:"abnormal_type,omitempty"`
}

type RequestBodyData struct {
	ContentType   string `json:"content_type"`
	Body          *Body  `json:"body,omitempty"`
	Size          int    `json:"size"`
	CaptureReason string `json:"capture_reason,omitempty"`
	AbnormalType  string `json:"abnormal_type,omitempty"`
}

type ResponseData struct {
	StatusCode    int           `json:"status_code"`
	Headers       http.Header   `json:"headers,omitempty"`
	Duration      time.Duration `json:"duration"`
	IsStream      bool          `json:"is_stream"`
	IsUpgrade     bool          `json:"is_upgrade,omitempty"`
	Size          int           `json:"size"`
	CaptureReason string        `json:"capture_reason,omitempty"`
	AbnormalType  string        `json:"abnormal_type,omitempty"`
	Error         string        `json:"error,omitempty"`
}

type ResponseBodyData struct {
	ContentType   string        `json:"content_type"`
	Body          *Body         `json:"body,omitempty"`
	Size          int           `json:"size"`
	Duration      time.Duration `json:"duration"`
	Complete      bool          `json:"complete"`
	CaptureReason string        `json:"capture_reason,omitempty"`
	AbnormalType  string        `json:"abnormal_type,omitempty"`
	Error         string        `json:"error,omitempty"`
}

type StreamEventData struct {
	Sequence  int    `json:"sequence"`
	EventName string `json:"event_type,omitempty"`
	SSEID     string `json:"event_id,omitempty"`
	Retry     string `json:"retry,omitempty"`
	Payload   any    `json:"payload,omitempty"`
	Size      int    `json:"size"`
}

type StreamEndData struct {
	TotalReads   int           `json:"total_reads"`
	TotalBytes   int64         `json:"total_bytes"`
	Duration     time.Duration `json:"duration"`
	EventRecords int           `json:"event_records,omitempty"`
	EventBytes   int64         `json:"event_bytes,omitempty"`
	Error        string        `json:"error,omitempty"`
}
