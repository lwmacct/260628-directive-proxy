package proxyplan

import "net/url"

type Plan struct {
	Target     *url.URL
	Proxy      *url.URL
	HeaderMode HeaderMode
	HeaderOps  []HeaderOp
	Labels     map[string]any
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
