package directive

import "github.com/lwmacct/260628-directive-proxy/internal/core/module"

const (
	TokenFamily  = "dproxy"
	TokenVersion = "18"
	TokenInline  = "i"
	TokenRemote  = "r"
)

const (
	KindInline = "inline"
	KindRemote = "remote"
)

type Document struct {
	Kind     string          `json:"kind" enum:"inline,remote"`
	Payload  *Payload        `json:"payload,omitempty"`
	Remote   *RemoteDocument `json:"remote,omitempty"`
	Recovery *RecoverySpec   `json:"recovery,omitempty"`
}

type RemoteDocument struct {
	Source  RemoteSpec     `json:"source"`
	Program module.Program `json:"program,omitempty"`
}

type RecoverySpec struct {
	Controller RecoveryControllerSpec `json:"controller"`
	Triggers   RecoveryTriggerSpec    `json:"triggers"`
	Budget     RecoveryBudgetSpec     `json:"budget"`
}

type RecoveryControllerSpec struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	Timeout string            `json:"timeout,omitempty"`
}

type RecoveryTriggerSpec struct {
	ResponseHeaderTimeout string                        `json:"response_header_timeout,omitempty"`
	UnexpectedStatus      *RecoveryUnexpectedStatusSpec `json:"unexpected_status,omitempty"`
	TransportError        bool                          `json:"transport_error,omitempty"`
	DirectiveError        bool                          `json:"directive_error,omitempty"`
}

type RecoveryUnexpectedStatusSpec struct {
	Expected         []RecoveryStatusRangeSpec `json:"expected"`
	CaptureBodyBytes int64                     `json:"capture_body_bytes,omitempty"`
}

type RecoveryStatusRangeSpec struct {
	From int `json:"from"`
	To   int `json:"to"`
}

type RecoveryBudgetSpec struct {
	MaxAttempts int    `json:"max_attempts"`
	MaxElapsed  string `json:"max_elapsed,omitempty"`
}

const (
	RemoteTypeHTTP  = "http"
	RemoteTypeRedis = "redis"
)

type RemoteSpec struct {
	Type           string            `json:"type"`
	URL            string            `json:"url"`
	Key            string            `json:"key,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
	RequestHeaders []string          `json:"request_headers,omitempty"`
}

type Payload struct {
	Target  TargetSection  `json:"target"`
	Proxy   string         `json:"proxy,omitempty"`
	Headers *HeaderSection `json:"headers,omitempty"`
	Program module.Program `json:"program,omitempty"`
}

type TargetSection struct {
	URL      string `json:"url"`
	JoinPath *bool  `json:"join_path,omitempty"`
}

type HeaderSection struct {
	Request  *RequestHeaderSection  `json:"request,omitempty"`
	Response *ResponseHeaderSection `json:"response,omitempty"`
}

type RequestHeaderSection struct {
	Mode                    string     `json:"mode,omitempty"`
	PreserveProxyDisclosure bool       `json:"preserve_proxy_disclosure,omitempty"`
	Ops                     []HeaderOp `json:"ops,omitempty"`
}

type ResponseHeaderSection struct {
	Ops []HeaderOp `json:"ops,omitempty"`
}

type HeaderOp struct {
	Op     string   `json:"op"`
	Name   string   `json:"name,omitempty"`
	Glob   string   `json:"glob,omitempty"`
	Values []string `json:"values,omitempty"`
}
