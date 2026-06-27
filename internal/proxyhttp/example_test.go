package proxyhttp_test

import (
	"net/http"
	"net/url"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/proxyhttp"
	"github.com/lwmacct/260628-llm-relay-dproxy/internal/proxyplan"
)

type staticResolver struct{}

func (staticResolver) Resolve(*http.Request) (*proxyplan.Plan, error) {
	target, _ := url.Parse("https://api.example.com/v1")
	return &proxyplan.Plan{
		Target:   target,
		JoinPath: true,
		HeaderOps: []proxyplan.HeaderOp{{
			Action: proxyplan.HeaderSet,
			Name:   "Authorization",
			Values: []string{"Bearer upstream-token"},
		}},
	}, nil
}

func ExampleNewHandler() {
	handler := proxyhttp.NewHandler(staticResolver{}, http.DefaultTransport, proxyhttp.HandlerOptions{})
	_ = handler
}
