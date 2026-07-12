package proxy

import "net/url"

type Plan struct {
	Target                    *url.URL
	Proxy                     *url.URL
	HeaderMode                HeaderMode
	HeaderOps                 []HeaderOp
	JoinPath                  bool
	DirectiveMode             string
	DirectiveBackend          string
	DirectiveEndpoint         string
	DirectiveKey              string
	DirectiveResolutionMillis int64
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
	HeaderSelectorExact  HeaderSelectorKind = "exact"
	HeaderSelectorGlob   HeaderSelectorKind = "glob"
	HeaderSelectorPreset HeaderSelectorKind = "preset"
)

const HeaderPresetProxyDisclosure = "proxy-disclosure"

type HeaderSelector struct {
	Kind    HeaderSelectorKind
	Pattern string
}

type HeaderOp struct {
	Action   HeaderAction
	Selector HeaderSelector
	Values   []string
}
