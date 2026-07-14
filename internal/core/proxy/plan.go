package proxy

import (
	"net/url"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/requestmeta"
)

type Plan struct {
	Target     *url.URL
	Proxy      *url.URL
	HeaderMode HeaderMode
	HeaderOps  []HeaderOp
	Metadata   requestmeta.Metadata
	JoinPath   bool
}

type Resolution struct {
	Plan   *Plan
	Source SourceMetadata
}

type SourceMetadata struct {
	Mode          string
	Backend       string
	Endpoint      string
	Key           string
	Duration      time.Duration
	PayloadSHA256 string
}

func ClonePlan(in *Plan) *Plan {
	if in == nil {
		return nil
	}
	out := *in
	out.Target = cloneURL(in.Target)
	out.Proxy = cloneURL(in.Proxy)
	out.HeaderOps = make([]HeaderOp, len(in.HeaderOps))
	for i, op := range in.HeaderOps {
		out.HeaderOps[i] = op
		out.HeaderOps[i].Values = append([]string(nil), op.Values...)
	}
	out.Metadata = requestmeta.Clone(in.Metadata)
	return &out
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
