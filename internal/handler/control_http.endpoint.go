package handler

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

type ControlEndpoint struct{ services Services }

func NewControlEndpoint(services Services) *ControlEndpoint {
	return &ControlEndpoint{services: services}
}

func (e *ControlEndpoint) Handler() http.Handler {
	mux := http.NewServeMux()
	api := humago.New(mux, utilHTTPConfig("Directive Proxy Control API", "/api/control"))
	e.Register(api)
	return mux
}

func (e *ControlEndpoint) Register(api huma.API) {
	RegisterDirective(api)
	RegisterProxyRequest(api, e.services.Requests)
}
