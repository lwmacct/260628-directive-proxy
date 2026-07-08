package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/core/proxy"
)

type Endpoint struct {
	config   Config
	services Services
}

func NewEndpoint(config Config, services Services) *Endpoint {
	return &Endpoint{config: config, services: services}
}

func (e *Endpoint) Handler() http.Handler {
	mux := http.NewServeMux()
	api := humago.New(mux, utilHTTPConfig())
	e.Register(api)
	return mux
}

func (e *Endpoint) Register(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "get-health",
		Method:      http.MethodGet,
		Path:        "/health",
		Summary:     "Get service health",
	}, func(ctx context.Context, input *struct{}) (*HealthOutputDTO, error) {
		return &HealthOutputDTO{
			Body: HealthResponseDTO{
				Status:    "ok",
				Timestamp: time.Now().UTC(),
			},
		}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-proxy-exchanges",
		Method:      http.MethodGet,
		Path:        "/proxy-exchanges",
		Summary:     "List recent proxy request and response exchanges",
	}, func(ctx context.Context, input *ListProxyExchangesInputDTO) (*ListProxyExchangesOutputDTO, error) {
		limit := 0
		if input != nil {
			limit = input.Limit
		}
		if e.services.Exchanges == nil {
			return &ListProxyExchangesOutputDTO{
				Body: emptyProxyExchangesResponse(),
			}, nil
		}
		return &ListProxyExchangesOutputDTO{
			Body: e.services.Exchanges.Snapshot(limit),
		}, nil
	})
}

func emptyProxyExchangesResponse() proxy.ExchangeSnapshot {
	return proxy.ExchangeSnapshot{
		Enabled: false,
		Items:   []proxy.ExchangeRecord{},
	}
}
