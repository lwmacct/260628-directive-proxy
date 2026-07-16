package proxy

import (
	"net/url"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
	"github.com/lwmacct/260628-directive-proxy/internal/core/requestmeta"
)

type Plan struct {
	Target   *url.URL
	Proxy    *url.URL
	Headers  HeaderPlan
	Metadata requestmeta.Metadata
	Modules  []module.Spec
	JoinPath bool
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
	out.Headers = cloneHeaderPlan(in.Headers)
	out.Metadata = requestmeta.Clone(in.Metadata)
	out.Modules = cloneModuleSpecs(in.Modules)
	return &out
}

func cloneHeaderPlan(in HeaderPlan) HeaderPlan {
	out := in
	out.Request.StripBeforeOps = append([]string(nil), in.Request.StripBeforeOps...)
	out.Request.Ops = cloneHeaderOps(in.Request.Ops)
	out.Response.Ops = cloneHeaderOps(in.Response.Ops)
	return out
}

func cloneHeaderOps(in []HeaderOp) []HeaderOp {
	out := make([]HeaderOp, len(in))
	for i, op := range in {
		out[i] = op
		out[i].Values = append([]string(nil), op.Values...)
	}
	return out
}

func cloneModuleSpecs(in []module.Spec) []module.Spec {
	out := make([]module.Spec, len(in))
	for index, spec := range in {
		out[index] = spec
		out[index].Config = append([]byte(nil), spec.Config...)
	}
	return out
}

type HeaderMode string

const (
	HeaderModePatch   HeaderMode = "patch"
	HeaderModeReplace HeaderMode = "replace"
)

type HeaderPlan struct {
	Request  RequestHeaderPlan
	Response ResponseHeaderPlan
}

type RequestHeaderPlan struct {
	Mode                    HeaderMode
	PreserveProxyDisclosure bool
	StripBeforeOps          []string
	Ops                     []HeaderOp
}

type ResponseHeaderPlan struct {
	Ops []HeaderOp
}

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
