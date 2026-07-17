package proxy

import (
	"net/http"
	"net/url"
	"strings"

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

func BuildOutboundURL(target, inbound *url.URL, joinPath bool) *url.URL {
	out := cloneURL(target)
	if out == nil {
		return nil
	}
	if inbound == nil || !joinPath {
		return out
	}
	out.RawQuery = joinRawQuery(target.RawQuery, inbound.RawQuery)
	if target.RawPath != "" || inbound.RawPath != "" {
		out.Path = singleJoiningSlash(target.EscapedPath(), inbound.EscapedPath())
		parsed, err := url.Parse(out.Path)
		if err == nil {
			out.Path = parsed.Path
			out.RawPath = parsed.RawPath
		}
		return out
	}
	out.Path = singleJoiningSlash(target.Path, inbound.Path)
	return out
}

func singleJoiningSlash(left, right string) string {
	leftSlash := strings.HasSuffix(left, "/")
	rightSlash := strings.HasPrefix(right, "/")
	switch {
	case leftSlash && rightSlash:
		return left + right[1:]
	case !leftSlash && !rightSlash:
		return left + "/" + right
	default:
		return left + right
	}
}

func joinRawQuery(targetQuery, inboundQuery string) string {
	switch {
	case targetQuery == "":
		return inboundQuery
	case inboundQuery == "":
		return targetQuery
	default:
		return targetQuery + "&" + inboundQuery
	}
}
