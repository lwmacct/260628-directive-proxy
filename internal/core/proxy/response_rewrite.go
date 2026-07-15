package proxy

import (
	"context"
	"net/http"
	"strings"
)

type responseHeaderPlanContextKey struct{}

var protectedResponseHeaders = map[string]struct{}{
	"connection":          {},
	"content-length":      {},
	"date":                {},
	"dproxy-retry-id":     {},
	"host":                {},
	"keep-alive":          {},
	"proxy-authenticate":  {},
	"proxy-authorization": {},
	"proxy-connection":    {},
	"te":                  {},
	"trailer":             {},
	"transfer-encoding":   {},
	"upgrade":             {},
}

func IsResponseHeaderProtected(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if strings.HasPrefix(name, "x-dproxy-") {
		return true
	}
	_, protected := protectedResponseHeaders[name]
	return protected
}

func bindResponseHeaderPlan(response *http.Response, request *http.Request, plan ResponseHeaderPlan) {
	if response == nil {
		return
	}
	if response.Request == nil {
		response.Request = request
	}
	if response.Request == nil {
		return
	}
	ctx := context.WithValue(response.Request.Context(), responseHeaderPlanContextKey{}, cloneHeaderOps(plan.Ops))
	response.Request = response.Request.WithContext(ctx)
}

func modifyResponse(response *http.Response) error {
	if response == nil {
		return nil
	}
	if response.Header == nil {
		response.Header = make(http.Header)
	}
	var ops []HeaderOp
	if response.Request != nil {
		ops, _ = response.Request.Context().Value(responseHeaderPlanContextKey{}).([]HeaderOp)
	}
	for _, op := range ops {
		for _, name := range matchingHeaderNames(response.Header, op.Selector) {
			if !IsResponseHeaderProtected(name) {
				applyHeaderOp(response.Header, name, op)
			}
		}
	}
	stripDproxyHeaders(response.Header)
	if response.StatusCode != http.StatusSwitchingProtocols {
		stripHopByHopHeaders(response.Header)
	}
	return nil
}
