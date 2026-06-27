package proxy

import (
	"net/http"
	"net/url"
)

type staticResolver struct{}

func (staticResolver) Resolve(*http.Request) (*Plan, error) {
	target, _ := url.Parse("https://api.example.com/v1")
	return &Plan{
		Target:   target,
		JoinPath: true,
		HeaderOps: []HeaderOp{{
			Action: HeaderSet,
			Name:   "Authorization",
			Values: []string{"Bearer upstream-token"},
		}},
	}, nil
}

func ExampleNewHandler() {
	handler := NewHandler(staticResolver{}, http.DefaultTransport, HandlerOptions{})
	_ = handler
}
