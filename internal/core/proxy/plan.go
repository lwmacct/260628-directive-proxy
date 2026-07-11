package proxy

import "net/url"

type Plan struct {
	Target     *url.URL
	Proxy      *url.URL
	HeaderMode HeaderMode
	HeaderOps  []HeaderOp
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

type HeaderSelectorKind string

const (
	HeaderSelectorExact HeaderSelectorKind = "exact"
	HeaderSelectorGlob  HeaderSelectorKind = "glob"
)

type HeaderSelector struct {
	Kind    HeaderSelectorKind
	Pattern string
}

type HeaderOp struct {
	Action   HeaderAction
	Selector HeaderSelector
	Values   []string
}
