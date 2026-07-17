package proxy

import (
	"net/url"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/httpheader"
	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
	"github.com/lwmacct/260628-directive-proxy/internal/core/recovery"
	"github.com/lwmacct/260628-directive-proxy/internal/core/requestmeta"
)

type Plan struct {
	Target   *url.URL
	Proxy    *url.URL
	Headers  httpheader.Plan
	Metadata requestmeta.Metadata
	Modules  []module.Spec
	Recovery *recovery.Policy
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
	out.Headers = httpheader.ClonePlan(in.Headers)
	out.Metadata = requestmeta.Clone(in.Metadata)
	out.Modules = cloneModuleSpecs(in.Modules)
	out.Recovery = recovery.ClonePolicy(in.Recovery)
	return &out
}

func cloneModuleSpecs(in []module.Spec) []module.Spec {
	out := make([]module.Spec, len(in))
	for index, spec := range in {
		out[index] = spec
		out[index].Config = append([]byte(nil), spec.Config...)
	}
	return out
}
