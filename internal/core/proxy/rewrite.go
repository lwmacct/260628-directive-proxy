package proxy

import (
	"net/http"
	"net/url"
	"path"
	"sort"
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

var hopByHopHeaders = []string{
	"Connection",
	"Proxy-Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te",
	"Trailer",
	"Transfer-Encoding",
	"Upgrade",
}

func applyPlan(out *http.Request, originalHeaders http.Header, d *Plan) {
	if out == nil || d == nil || d.Target == nil {
		return
	}

	transportHeaders := trustedTransportHeaders(originalHeaders)
	out.Host = ""
	requestHeaders := d.Headers.Request
	replaceHeaders := requestHeaders.Mode == HeaderModeReplace
	if replaceHeaders {
		out.Header = make(http.Header)
		out.Host = ""
	} else {
		out.Header = cloneEndToEndHeaders(originalHeaders)
	}
	for _, name := range requestHeaders.StripBeforeOps {
		out.Header.Del(name)
	}
	if !requestHeaders.PreserveProxyDisclosure {
		stripProxyDisclosureHeaders(out.Header)
	}
	applyRequestHeaderOps(out, requestHeaders.Ops)
	stripDproxyHeaders(out.Header)
	stripHopByHopHeaders(out.Header)
	copyHeaders(out.Header, transportHeaders)
	// Capture parses SSE at the downstream byte boundary. Force identity encoding so
	// event framing is observable without changing the response representation.
	out.Header.Set("Accept-Encoding", "identity")
	if replaceHeaders {
		suppressDefaultUserAgent(out.Header)
	}
}

func stripDproxyHeaders(headers http.Header) {
	for name := range headers {
		if strings.HasPrefix(strings.ToLower(name), "x-dproxy-") {
			delete(headers, name)
		}
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
		for _, headerName := range matchingHeaderNames(headers, op.Selector) {
			applyHeaderOp(headers, headerName, op)
		}
	}
}

func matchingHeaderNames(headers http.Header, selector HeaderSelector) []string {
	pattern := strings.TrimSpace(selector.Pattern)
	if pattern == "" {
		return nil
	}
	if selector.Kind == HeaderSelectorExact {
		return []string{http.CanonicalHeaderKey(pattern)}
	}
	if selector.Kind != HeaderSelectorGlob {
		return nil
	}

	names := make([]string, 0, len(headers))
	pattern = strings.ToLower(pattern)
	for name := range headers {
		if strings.EqualFold(name, "Host") {
			continue
		}
		matched, err := path.Match(pattern, strings.ToLower(name))
		if err == nil && matched {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func isProxyDisclosureHeader(name string) bool {
	if strings.HasPrefix(strings.ToLower(name), "x-forwarded-") {
		return true
	}
	for _, disclosureHeader := range proxyDisclosureHeaders {
		if strings.EqualFold(name, disclosureHeader) {
			return true
		}
	}
	return false
}

func stripProxyDisclosureHeaders(headers http.Header) {
	for name := range headers {
		if isProxyDisclosureHeader(name) {
			headers.Del(name)
		}
	}
}

func applyHeaderOp(headers http.Header, headerName string, op HeaderOp) {
	switch op.Action {
	case HeaderAdd:
		for _, value := range op.Values {
			headers.Add(headerName, value)
		}
	case HeaderSet:
		if len(op.Values) == 0 {
			return
		}
		headers.Set(headerName, op.Values[0])
		for _, value := range op.Values[1:] {
			headers.Add(headerName, value)
		}
	case HeaderRemove:
		headers.Del(headerName)
	}
}

func cloneEndToEndHeaders(in http.Header) http.Header {
	headers := in.Clone()
	if headers == nil {
		headers = make(http.Header)
	}
	stripHopByHopHeaders(headers)
	return headers
}

func stripHopByHopHeaders(headers http.Header) {
	for _, value := range headers.Values("Connection") {
		for _, name := range strings.Split(value, ",") {
			headers.Del(strings.TrimSpace(name))
		}
	}
	for _, name := range hopByHopHeaders {
		headers.Del(name)
	}
}

func trustedTransportHeaders(headers http.Header) http.Header {
	trusted := make(http.Header)
	if headerTokenContains(headers.Values("Connection"), "upgrade") && strings.TrimSpace(headers.Get("Upgrade")) != "" {
		trusted.Set("Connection", "Upgrade")
		for _, value := range headers.Values("Upgrade") {
			trusted.Add("Upgrade", value)
		}
	}
	if headerTokenContains(headers.Values("Te"), "trailers") {
		trusted.Set("Te", "trailers")
	}
	return trusted
}

func headerTokenContains(values []string, token string) bool {
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}

func copyHeaders(dst, src http.Header) {
	for name, values := range src {
		dst[name] = append([]string(nil), values...)
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
		if op.Selector.Kind != HeaderSelectorExact || !strings.EqualFold(strings.TrimSpace(op.Selector.Pattern), "Host") {
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
