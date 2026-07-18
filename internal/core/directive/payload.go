package directive

import (
	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
	"github.com/lwmacct/260628-directive-proxy/internal/core/recovery"
)

const (
	TokenFamily  = "dp"
	TokenVersion = "22"
	TokenInline  = "inline"
	TokenRemote  = "remote"
)

const (
	KindInline = "inline"
	KindRemote = "remote"
)

type Document struct {
	Kind    string      `json:"kind" enum:"inline,remote"`
	Payload *Payload    `json:"payload,omitempty"`
	Remote  *RemoteSpec `json:"remote,omitempty"`
}

type RecoverySpec struct {
	Controller recovery.ControllerSpec `json:"controller"`
	Triggers   RecoveryTriggerSpec     `json:"triggers"`
	Budget     RecoveryBudgetSpec      `json:"budget"`
}

type RecoveryTriggerSpec struct {
	ResponseHeaderTimeout string                        `json:"response_header_timeout,omitempty"`
	UnexpectedStatus      *RecoveryUnexpectedStatusSpec `json:"unexpected_status,omitempty"`
	TransportError        bool                          `json:"transport_error,omitempty"`
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
	MaxRoundTrips int    `json:"max_round_trips"`
	MaxElapsed    string `json:"max_elapsed,omitempty"`
}

const (
	RemoteTypeHTTP  = "http"
	RemoteTypeRedis = "redis"
	RemoteTypeFile  = "file"
)

type HeaderSide string

const (
	HeaderSideRequest  HeaderSide = "request"
	HeaderSideResponse HeaderSide = "response"
)

type HeaderAction string

const (
	HeaderActionAdd HeaderAction = "add"
	HeaderActionSet HeaderAction = "set"
	HeaderActionDel HeaderAction = "del"
)

type RemoteSpec struct {
	HTTP  *HTTPRemoteSpec  `json:"http,omitempty"`
	Redis *RedisRemoteSpec `json:"redis,omitempty"`
	File  *FileRemoteSpec  `json:"file,omitempty"`
}

type HTTPRemoteSpec struct {
	URL     string        `json:"url"`
	Headers *HeaderPolicy `json:"headers,omitempty"`
}

type RedisRemoteSpec struct {
	URL string `json:"url"`
	Key string `json:"key"`
}

type FileRemoteSpec struct {
	Path string `json:"path"`
}

type Payload struct {
	Metadata map[string]string `json:"metadata,omitempty"`
	Target   TargetSection     `json:"target"`
	Proxy    string            `json:"proxy,omitempty"`
	Headers  *HeaderPolicy     `json:"headers,omitempty"`
	Modules  module.Specs      `json:"modules,omitempty"`
	Recovery *RecoverySpec     `json:"recovery,omitempty"`
}

type TargetSection struct {
	BaseURL  string `json:"base_url,omitempty"`
	ExactURL string `json:"exact_url,omitempty"`
}

type HeaderPolicy struct {
	PreserveProxyDisclosure bool             `json:"preserve_proxy_disclosure,omitempty"`
	Mutations               []HeaderMutation `json:"mutations,omitempty"`
}

type HeaderMutation struct {
	Side   HeaderSide   `json:"side"`
	Action HeaderAction `json:"action"`
	Name   string       `json:"name,omitempty"`
	Glob   string       `json:"glob,omitempty"`
	Values []string     `json:"values,omitempty"`
}
