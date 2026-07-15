package observability

import (
	"net/http"
	"net/url"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/bodymemory"
	"github.com/lwmacct/260628-directive-proxy/internal/core/requestmeta"
)

// Signal is an in-process observation. Streaming body slices are borrowed and
// only valid for Trace.Observe. RequestBodyAvailable exposes an immutable body
// whose lifetime must be extended with a lease before it is retained.
type Signal struct {
	Attempt    int
	ObservedAt time.Time
	Value      any
}

type RequestStarted struct {
	Method string
	URL    string
	Host   string
	Header http.Header
}

type RequestBodyAvailable struct{ Body *bodymemory.Body }

type RequestBodyEnded struct {
	Total    int64
	SHA256   string
	Complete bool
}

type AttemptStarted struct {
	Mode     string
	Backend  string
	Endpoint string
	Key      string
}

type AttemptRejected struct{ Reason string }

type DirectiveResolved struct {
	Duration      time.Duration
	PayloadSHA256 string
	Target        *url.URL
	TargetChanged bool
	PlanChanged   bool
	Metadata      requestmeta.Metadata
}

type DirectiveFailed struct {
	Duration time.Duration
	Code     string
}

type MetadataBound struct{ Metadata requestmeta.Metadata }

type MetadataChanged struct {
	Bound    requestmeta.Metadata
	Observed requestmeta.Metadata
}

type UpstreamStarted struct {
	TargetURL string
	Header    http.Header
}

type AttemptFinished struct{ Outcome string }

type RetryRequested struct {
	Trigger          string
	NextAttempt      int
	SelectorMetadata requestmeta.Metadata
}

type UpstreamResponseStarted struct {
	StatusCode      int
	Header          http.Header
	AttemptMetadata requestmeta.Metadata
}

type DownstreamResponseStarted struct {
	StatusCode int
	Header     http.Header
}

type UpstreamBodyChunk struct{ Data []byte }

type UpstreamBodyEnded struct{ Cause error }

type DownstreamBodyChunk struct{ Data []byte }

type DownstreamBodyEnded struct{}

type RequestCompleted struct {
	Outcome    string
	StatusCode int
	Duration   time.Duration
}
