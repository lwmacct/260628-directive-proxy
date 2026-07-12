package proxy

import (
	"net/http"
	"net/url"
)

type staticResolver struct{}

func (staticResolver) Match(*http.Request) bool { return true }

func (staticResolver) Resolve(*http.Request) (*Plan, error) {
	target, _ := url.Parse("https://api.example.com/v1")
	return &Plan{
		Target:   target,
		JoinPath: true,
		HeaderOps: []HeaderOp{{
			Action: HeaderSet,
			Selector: HeaderSelector{
				Kind:    HeaderSelectorExact,
				Pattern: "Authorization",
			},
			Values: []string{"Bearer upstream-token"},
		}},
	}, nil
}

func ExampleNewHandler() {
	handler := NewHandler(staticResolver{}, http.DefaultTransport, HandlerOptions{})
	_ = handler
}
