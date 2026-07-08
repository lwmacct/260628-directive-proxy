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

	huma.Register(api, huma.Operation{
		OperationID: "get-proxy-exchange",
		Method:      http.MethodGet,
		Path:        "/proxy-exchanges/{id}",
		Summary:     "Get one proxy request and response exchange",
	}, func(ctx context.Context, input *GetProxyExchangeInputDTO) (*GetProxyExchangeOutputDTO, error) {
		if e.services.Exchanges == nil || input == nil {
			return nil, huma.Error404NotFound("proxy exchange not found")
		}
		record, ok := e.services.Exchanges.Get(input.ID)
		if !ok {
			return nil, huma.Error404NotFound("proxy exchange not found")
		}
		return &GetProxyExchangeOutputDTO{Body: record}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-proxy-exchange-settings",
		Method:      http.MethodPut,
		Path:        "/proxy-exchanges/settings",
		Summary:     "Enable or disable proxy exchange capture",
	}, func(ctx context.Context, input *UpdateProxyExchangeSettingsInputDTO) (*UpdateProxyExchangeSettingsOutputDTO, error) {
		if e.services.Exchanges == nil || input == nil {
			return &UpdateProxyExchangeSettingsOutputDTO{
				Body: emptyProxyExchangesResponse(),
			}, nil
		}
		return &UpdateProxyExchangeSettingsOutputDTO{
			Body: e.services.Exchanges.Configure(
				input.Body.Enabled,
				optionalInt(input.Body.Capacity),
				optionalInt64(input.Body.MaxBodyBytes),
			),
		}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "clear-proxy-exchanges",
		Method:      http.MethodDelete,
		Path:        "/proxy-exchanges",
		Summary:     "Clear retained proxy request and response exchanges",
	}, func(ctx context.Context, input *struct{}) (*ClearProxyExchangesOutputDTO, error) {
		if e.services.Exchanges == nil {
			return &ClearProxyExchangesOutputDTO{
				Body: emptyProxyExchangesResponse(),
			}, nil
		}
		return &ClearProxyExchangesOutputDTO{
			Body: e.services.Exchanges.Clear(),
		}, nil
	})
}

func emptyProxyExchangesResponse() proxy.ExchangeSnapshot {
	return proxy.ExchangeSnapshot{
		Enabled: false,
		Items:   []proxy.ExchangeRecord{},
	}
}

func optionalInt(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func optionalInt64(value *int64) int64 {
	if value == nil {
		return -1
	}
	return *value
}
