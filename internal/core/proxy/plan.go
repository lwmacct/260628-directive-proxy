package proxy

import (
	"net/url"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/httpheader"
)

type BodyPolicy struct {
	MaxBodyBytes int64         // -1 means inherit instance default.
	QueueWait    time.Duration // -1 means inherit instance default.
	ReadTimeout  time.Duration // -1 means inherit instance default.
	ChunkBytes   int           // -1 means inherit instance default.
}

type Plan struct {
	Target  *url.URL
	Proxy   *url.URL
	Headers httpheader.Plan
}

type DirectiveSource struct {
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
	return &out
}
