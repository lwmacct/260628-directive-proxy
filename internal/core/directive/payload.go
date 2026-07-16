package directive

import "github.com/lwmacct/260628-directive-proxy/internal/core/module"

const (
	TokenFamily  = "dproxy"
	TokenVersion = "17"
	TokenInline  = "i"
	TokenRemote  = "r"
)

const (
	KindInline = "inline"
	KindRemote = "remote"
)

type Document struct {
	Kind    string          `json:"kind" enum:"inline,remote"`
	Payload *Payload        `json:"payload,omitempty"`
	Remote  *RemoteDocument `json:"remote,omitempty"`
}

type RemoteDocument struct {
	Source  RemoteSpec     `json:"source"`
	Program module.Program `json:"program,omitempty"`
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
