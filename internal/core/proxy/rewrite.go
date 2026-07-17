package proxy

import (
	"net/http"
	"net/url"

	"github.com/lwmacct/260628-directive-proxy/internal/core/httpheader"
)

func applyPlan(out *http.Request, originalHeaders http.Header, directive *Plan) {
	if out == nil || directive == nil || directive.Target == nil {
		return
	}

	httpheader.ApplyRequest(out, originalHeaders, directive.Headers.Request, httpheader.RequestOptions{PreserveTransport: true})
	// Capture parses SSE at the downstream byte boundary. Force identity encoding so
	// event framing is observable without changing the response representation.
	out.Header.Set("Accept-Encoding", "identity")
}

func cloneURL(in *url.URL) *url.URL {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}
