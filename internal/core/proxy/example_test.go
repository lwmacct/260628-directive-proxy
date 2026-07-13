package proxy

import (
	"net/http"
	"net/url"
)

type staticResolver struct{}

func (staticResolver) Resolve(*http.Request) (Resolution, error) {
	target, _ := url.Parse("https://api.example.com/v1")
	return Resolution{Plan: &Plan{
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
	}}, nil
}

func ExampleNewHandler() {
	handler := NewHandler(staticResolver{}, http.DefaultTransport, HandlerOptions{})
	_ = handler
}
