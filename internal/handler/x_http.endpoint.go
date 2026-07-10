package handler

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

type Endpoint struct {
	services Services
}

func NewEndpoint(services Services) *Endpoint {
	return &Endpoint{services: services}
}

func (e *Endpoint) Handler() http.Handler {
	mux := http.NewServeMux()
	api := humago.New(mux, utilHTTPConfig())
	e.Register(api)
	return mux
}

func (e *Endpoint) Register(api huma.API) {
	RegisterHealth(api)
	RegisterProxyExchange(api, e.services.Exchanges)
}
