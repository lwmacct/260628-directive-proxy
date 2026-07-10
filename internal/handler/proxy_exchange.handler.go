package handler

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/service"
)

type proxyExchangeHandler struct {
	service *service.ExchangeService
}

func RegisterProxyExchange(api huma.API, exchangeService *service.ExchangeService) {
	handler := &proxyExchangeHandler{service: exchangeService}
	huma.Register(api, huma.Operation{
		OperationID: "list-proxy-exchanges",
		Method:      http.MethodGet,
		Path:        "/proxy-exchanges",
		Summary:     "List recent proxy request and response exchanges",
	}, handler.list)
	huma.Register(api, huma.Operation{
		OperationID: "get-proxy-exchange",
		Method:      http.MethodGet,
		Path:        "/proxy-exchanges/{id}",
		Summary:     "Get one proxy request and response exchange",
	}, handler.get)
	huma.Register(api, huma.Operation{
		OperationID: "update-proxy-exchange-settings",
		Method:      http.MethodPut,
		Path:        "/proxy-exchanges/settings",
		Summary:     "Enable or disable proxy exchange capture",
	}, handler.updateSettings)
	huma.Register(api, huma.Operation{
		OperationID: "clear-proxy-exchanges",
		Method:      http.MethodDelete,
		Path:        "/proxy-exchanges",
		Summary:     "Clear retained proxy request and response exchanges",
	}, handler.clear)
}

func (h *proxyExchangeHandler) list(_ context.Context, input *ListProxyExchangesInputDTO) (*ListProxyExchangesOutputDTO, error) {
	limit := 0
	if input != nil {
		limit = input.Limit
	}
	return &ListProxyExchangesOutputDTO{Body: ToProxyExchangeSnapshotDTO(h.service.Snapshot(limit))}, nil
}

func (h *proxyExchangeHandler) get(_ context.Context, input *GetProxyExchangeInputDTO) (*GetProxyExchangeOutputDTO, error) {
	if input == nil {
		return nil, huma.Error404NotFound("proxy exchange not found")
	}
	record, ok := h.service.Get(input.ID)
	if !ok {
		return nil, huma.Error404NotFound("proxy exchange not found")
	}
	return &GetProxyExchangeOutputDTO{Body: ToProxyExchangeRecordDTO(record)}, nil
}

func (h *proxyExchangeHandler) updateSettings(_ context.Context, input *UpdateProxyExchangeSettingsInputDTO) (*UpdateProxyExchangeSettingsOutputDTO, error) {
	if input == nil {
		return &UpdateProxyExchangeSettingsOutputDTO{Body: ToProxyExchangeSnapshotDTO(h.service.Snapshot(0))}, nil
	}
	snapshot := h.service.Configure(input.Body.Enabled, utilOptionalInt(input.Body.Capacity), utilOptionalInt64(input.Body.MaxBodyBytes))
	return &UpdateProxyExchangeSettingsOutputDTO{Body: ToProxyExchangeSnapshotDTO(snapshot)}, nil
}

func (h *proxyExchangeHandler) clear(_ context.Context, _ *struct{}) (*ClearProxyExchangesOutputDTO, error) {
	return &ClearProxyExchangesOutputDTO{Body: ToProxyExchangeSnapshotDTO(h.service.Clear())}, nil
}
