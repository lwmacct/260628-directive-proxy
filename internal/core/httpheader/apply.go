package httpheader

import (
	"net/http"
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

type RequestOptions struct {
	PreserveTransport bool
}

func ApplyRequest(out *http.Request, originalHeaders http.Header, plan RequestPlan, options RequestOptions) {
	if out == nil {
		return
	}
	var transportHeaders http.Header
	if options.PreserveTransport {
		transportHeaders = trustedTransportHeaders(originalHeaders)
	}
	out.Host = ""
	replaceHeaders := plan.Mode == ModeReplace
	if replaceHeaders {
		out.Header = make(http.Header)
	} else {
		out.Header = cloneEndToEndHeaders(originalHeaders)
	}
	for _, name := range plan.StripBeforeOps {
		out.Header.Del(name)
	}
	if !plan.PreserveProxyDisclosure {
		stripProxyDisclosureHeaders(out.Header)
	}
	applyRequestOps(out, plan.Ops)
	StripDproxy(out.Header)
	StripHopByHop(out.Header)
	if len(transportHeaders) > 0 {
		copyHeaders(out.Header, transportHeaders)
	}
	if replaceHeaders {
		suppressDefaultUserAgent(out.Header)
	}
}

func Apply(headers http.Header, ops []Op) {
	for _, op := range ops {
		for _, name := range MatchingNames(headers, op.Selector) {
			ApplyOp(headers, name, op)
		}
	}
}

func MatchingNames(headers http.Header, selector Selector) []string {
	pattern := strings.TrimSpace(selector.Pattern)
	if pattern == "" {
		return nil
	}
	if selector.Kind == SelectorExact {
		return []string{http.CanonicalHeaderKey(pattern)}
	}
	if selector.Kind != SelectorGlob {
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

func ApplyOp(headers http.Header, name string, op Op) {
	switch op.Action {
	case ActionAdd:
		for _, value := range op.Values {
			headers.Add(name, value)
		}
	case ActionSet:
		if len(op.Values) == 0 {
			return
		}
		headers.Set(name, op.Values[0])
		for _, value := range op.Values[1:] {
			headers.Add(name, value)
		}
	case ActionRemove:
		headers.Del(name)
	}
}

func StripDproxy(headers http.Header) {
	for name := range headers {
		if strings.HasPrefix(strings.ToLower(name), "x-dproxy-") {
			delete(headers, name)
		}
	}
}

func StripHopByHop(headers http.Header) {
	for _, value := range headers.Values("Connection") {
		for _, name := range strings.Split(value, ",") {
			headers.Del(strings.TrimSpace(name))
		}
	}
	for _, name := range hopByHopHeaders {
		headers.Del(name)
	}
}

func stripProxyDisclosureHeaders(headers http.Header) {
	for name := range headers {
		if isProxyDisclosureHeader(name) {
			headers.Del(name)
		}
	}
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

func cloneEndToEndHeaders(in http.Header) http.Header {
	headers := in.Clone()
	if headers == nil {
		headers = make(http.Header)
	}
	StripHopByHop(headers)
	return headers
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

func applyRequestOps(req *http.Request, ops []Op) {
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	for _, op := range ops {
		if op.Selector.Kind != SelectorExact || !strings.EqualFold(strings.TrimSpace(op.Selector.Pattern), "Host") {
			Apply(req.Header, []Op{op})
			continue
		}
		applyHostOp(req, op)
	}
}

func applyHostOp(req *http.Request, op Op) {
	if len(op.Values) == 0 {
		req.Host = ""
		req.Header.Del("Host")
		return
	}
	value := strings.TrimSpace(op.Values[0])
	switch op.Action {
	case ActionSet, ActionAdd:
		req.Host = value
	case ActionRemove:
		if req.Host == "" || req.Host == value {
			req.Host = ""
		}
	}
	req.Header.Del("Host")
}

func suppressDefaultUserAgent(headers http.Header) {
	if _, exists := headers["User-Agent"]; !exists {
		headers["User-Agent"] = nil
	}
}
