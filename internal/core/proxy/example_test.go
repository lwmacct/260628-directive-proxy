package proxy

import (
	"net/http"
	"net/url"

	"github.com/lwmacct/260628-directive-proxy/internal/core/httpheader"
)

type staticResolver struct{}

func (staticResolver) Prepare(*http.Request) (PreparedDirective, error) {
	target, _ := url.Parse("https://api.example.com/v1")
	return staticPrepared{resolution: Resolution{Plan: &Plan{
		Target: target,
		Headers: httpheader.Plan{Request: httpheader.RequestPlan{Ops: []httpheader.Op{{
			Action: httpheader.ActionSet,
			Selector: httpheader.Selector{
				Kind:    httpheader.SelectorExact,
				Pattern: "Authorization",
			},
			Values: []string{"Bearer upstream-token"},
		}}}},
	}}}, nil
}

func ExampleNewHandler() {
	handler := NewHandler(staticResolver{}, http.DefaultTransport, HandlerOptions{})
	_ = handler
}
