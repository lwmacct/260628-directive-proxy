package handler

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

type SystemEndpoint struct{ services Services }

func NewSystemEndpoint(services Services) *SystemEndpoint { return &SystemEndpoint{services: services} }

func (e *SystemEndpoint) Handler() http.Handler {
	mux := http.NewServeMux()
	api := humago.New(mux, utilHTTPConfig("Directive Proxy System API", ""))
	e.Register(api)
	return mux
}

func (e *SystemEndpoint) Register(api huma.API) {
	RegisterHealth(api, e.services.Observability)
}
