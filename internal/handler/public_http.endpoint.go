package handler

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

type PublicEndpoint struct{ services Services }

func NewPublicEndpoint(services Services) *PublicEndpoint { return &PublicEndpoint{services: services} }

func (e *PublicEndpoint) Handler() http.Handler {
	mux := http.NewServeMux()
	api := humago.New(mux, utilHTTPConfig("Directive Proxy Requester API", "/api/public"))
	e.Register(api)
	return mux
}

func (e *PublicEndpoint) Register(api huma.API) {
	RegisterRequestRetry(api, e.services.ExchangeCommands)
}
