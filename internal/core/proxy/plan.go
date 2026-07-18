package proxy

import (
	"net/url"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/httpheader"
	"github.com/lwmacct/260628-directive-proxy/internal/core/requestmeta"
)

type Plan struct {
	Target   *url.URL
	Proxy    *url.URL
	Headers  httpheader.Plan
	Metadata requestmeta.Metadata
}

type SourceMetadata struct {
	Mode          string
	Backend       string
	Endpoint      string
	Resource      string
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
	return &out
}
