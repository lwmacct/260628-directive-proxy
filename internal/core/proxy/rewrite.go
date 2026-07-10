package proxy

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

var proxyDisclosureHeaders = []string{
	"Forwarded",
	"Via",
	"X-Forwarded-For",
	"X-Forwarded-Host",
	"X-Forwarded-Proto",
	"X-Forwarded-Port",
	"X-Forwarded-Scheme",
	"X-Forwarded-Server",
	"X-Original-Forwarded-For",
	"X-Real-IP",
	"Client-IP",
	"X-Client-IP",
	"True-Client-IP",
	"CF-Connecting-IP",
	"CF-Connecting-IPv6",
	"Fastly-Client-IP",
	"X-Cluster-Client-IP",
	"X-ProxyUser-IP",
	"Proxy-Client-IP",
	"WL-Proxy-Client-IP",
	"CDN-Loop",
}

func applyRewrite(r *httputil.ProxyRequest, d *Plan) {
	if r == nil || d == nil || d.Target == nil {
		return
	}

	r.Out.URL = BuildOutboundURL(d.Target, r.In.URL, d.JoinPath)
	r.Out.Host = ""
	if r.Out.Header == nil {
		r.Out.Header = make(http.Header)
	}
	replaceHeaders := d.HeaderMode == HeaderModeReplace
	if replaceHeaders {
		r.Out.Header = make(http.Header)
		r.Out.Host = ""
	}
	stripProxyDisclosureHeaders(r.Out.Header)
	applyRequestHeaderOps(r.Out, d.HeaderOps)
	if replaceHeaders {
		suppressDefaultUserAgent(r.Out.Header)
	}
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

func applyHeaderOps(headers http.Header, ops []HeaderOp) {
	for _, op := range ops {
		headerName := http.CanonicalHeaderKey(strings.TrimSpace(op.Name))
		if headerName == "" {
			continue
		}
		if len(op.Values) == 0 {
			headers.Del(headerName)
			continue
		}
		switch op.Action {
		case HeaderAdd:
			for _, value := range op.Values {
				headers.Add(headerName, value)
			}
		case HeaderSet:
			headers.Set(headerName, op.Values[0])
			for _, value := range op.Values[1:] {
				headers.Add(headerName, value)
			}
		case HeaderRemove:
			current := headers.Values(headerName)
			if len(current) == 0 {
				continue
			}
			headers[headerName] = filterHeaderValues(current, op.Values)
			if len(headers[headerName]) == 0 {
				headers.Del(headerName)
			}
		}
	}
}

func ApplyHeaderOps(headers http.Header, ops []HeaderOp) {
	applyHeaderOps(headers, ops)
}

func stripProxyDisclosureHeaders(headers http.Header) {
	if headers == nil {
		return
	}
	for _, name := range proxyDisclosureHeaders {
		headers.Del(name)
	}
}

func applyRequestHeaderOps(req *http.Request, ops []HeaderOp) {
	if req == nil {
		return
	}
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	for _, op := range ops {
		if !strings.EqualFold(strings.TrimSpace(op.Name), "Host") {
			applyHeaderOps(req.Header, []HeaderOp{op})
			continue
		}
		applyHostOp(req, op)
	}
}

func applyHostOp(req *http.Request, op HeaderOp) {
	if len(op.Values) == 0 {
		req.Host = ""
		req.Header.Del("Host")
		return
	}
	value := strings.TrimSpace(op.Values[0])
	switch op.Action {
	case HeaderSet, HeaderAdd:
		req.Host = value
	case HeaderRemove:
		if req.Host == "" || req.Host == value {
			req.Host = ""
		}
	}
	req.Header.Del("Host")
}

func suppressDefaultUserAgent(headers http.Header) {
	if headers == nil {
		return
	}
	if _, exists := headers["User-Agent"]; !exists {
		headers["User-Agent"] = nil
	}
}

func filterHeaderValues(values []string, remove []string) []string {
	if len(values) == 0 || len(remove) == 0 {
		return values
	}
	removeSet := make(map[string]struct{}, len(remove))
	for _, value := range remove {
		removeSet[value] = struct{}{}
	}
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		if _, exists := removeSet[value]; !exists {
			filtered = append(filtered, value)
		}
	}
	return filtered
}

func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	}
	return a + b
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
