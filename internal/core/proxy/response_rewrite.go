package proxy

import (
	"context"
	"net/http"
	"strings"

	"github.com/lwmacct/260628-directive-proxy/internal/core/httpheader"
)

type responseHeaderPlanContextKey struct{}

var protectedResponseHeaders = map[string]struct{}{
	"connection":          {},
	"content-length":      {},
	"date":                {},
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
	if httpheader.IsSystemHeader(name) {
		return true
	}
	_, protected := protectedResponseHeaders[name]
	return protected
}

func bindResponseHeaderPlan(response *http.Response, request *http.Request, plan httpheader.ResponsePlan) {
	if response == nil {
		return
	}
	if response.Request == nil {
		response.Request = request
	}
	if response.Request == nil {
		return
	}
	ctx := context.WithValue(response.Request.Context(), responseHeaderPlanContextKey{}, httpheader.CloneOps(plan.Ops))
	response.Request = response.Request.WithContext(ctx)
}

func modifyResponse(response *http.Response) error {
	if response == nil {
		return nil
	}
	if response.Header == nil {
		response.Header = make(http.Header)
	}
	var ops []httpheader.Op
	if response.Request != nil {
		ops, _ = response.Request.Context().Value(responseHeaderPlanContextKey{}).([]httpheader.Op)
	}
	for _, op := range ops {
		for _, name := range httpheader.MatchingNames(response.Header, op.Selector) {
			if !IsResponseHeaderProtected(name) {
				httpheader.ApplyOp(response.Header, name, op)
			}
		}
	}
	httpheader.StripSystemHeaders(response.Header)
	if response.StatusCode != http.StatusSwitchingProtocols {
		httpheader.StripHopByHop(response.Header)
	}
	return nil
}
