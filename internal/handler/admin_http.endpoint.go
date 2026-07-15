package handler

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

type AdminEndpoint struct{ services Services }

func NewAdminEndpoint(services Services) *AdminEndpoint {
	return &AdminEndpoint{services: services}
}

func (e *AdminEndpoint) Handler() http.Handler {
	mux := http.NewServeMux()
	api := humago.New(mux, utilHTTPConfig("Directive Proxy Admin API", "/api/admin"))
	e.Register(api)
	return mux
}

func (e *AdminEndpoint) Register(api huma.API) {
	RegisterDirective(api)
	RegisterProxyRequest(api, e.services.Requests)
}
