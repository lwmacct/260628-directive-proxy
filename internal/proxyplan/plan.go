package proxyplan

import (
	"net/url"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/eventbus"
)

type Plan struct {
	Target     *url.URL
	Proxy      *url.URL
	HeaderMode HeaderMode
	HeaderOps  []HeaderOp
	Labels     map[string]any
	Runtime    eventbus.Runtime
	Capture    CapturePolicy
	JoinPath   bool
}

type HeaderMode string

const (
	HeaderModePatch   HeaderMode = "patch"
	HeaderModeReplace HeaderMode = "replace"
)

type HeaderAction string

const (
	HeaderAdd    HeaderAction = "+"
	HeaderRemove HeaderAction = "-"
	HeaderSet    HeaderAction = "="
)

type HeaderOp struct {
	Action HeaderAction
	Name   string
	Values []string
}
