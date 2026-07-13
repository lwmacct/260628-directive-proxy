package proxy

import (
	"net/url"
	"time"
)

type Plan struct {
	Target     *url.URL
	Proxy      *url.URL
	HeaderMode HeaderMode
	HeaderOps  []HeaderOp
	JoinPath   bool
}

type Resolution struct {
	Plan   *Plan
	Source SourceMetadata
}

type SourceMetadata struct {
	Mode     string
	Backend  string
	Endpoint string
	Key      string
	Duration time.Duration
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
