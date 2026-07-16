package handler

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/lwmacct/260628-directive-proxy/internal/core/exchange"
)

type proxyRequestHandler struct {
	query    ExchangeQuery
	commands ExchangeCommands
}

func RegisterProxyRequest(api huma.API, query ExchangeQuery, commands ExchangeCommands) {
	handler := &proxyRequestHandler{query: query, commands: commands}
	huma.Register(api, huma.Operation{
		OperationID: "list-active-proxy-requests",
		Method:      http.MethodGet,
		Path:        "/api/admin/proxy-requests",
		Summary:     "List active proxy requests across directive resolution and upstream wait states",
	}, handler.list)
	huma.Register(api, huma.Operation{
		OperationID: "get-active-proxy-request",
		Method:      http.MethodGet,
		Path:        "/api/admin/proxy-requests/{trace_id}",
		Summary:     "Get one active proxy request",
	}, handler.get)
	huma.Register(api, huma.Operation{
		OperationID: "retry-active-proxy-request",
		Method:      http.MethodPut,
		Path:        "/api/admin/proxy-requests/{trace_id}/retry",
		Summary:     "Retry an active upstream request attempt by trace ID",
	}, handler.retry)
}

func (h *proxyRequestHandler) list(_ context.Context, _ *struct{}) (*ListActiveProxyRequestsOutputDTO, error) {
	now := utilNowUTC()
	if h == nil || h.query == nil {
		return &ListActiveProxyRequestsOutputDTO{Body: ActiveProxyRequestSnapshotDTO{ServerTime: now, Items: []ActiveProxyRequestDTO{}}}, nil
	}
	items := h.query.ListActive()
	result := make([]ActiveProxyRequestDTO, 0, len(items))
	for _, item := range items {
		result = append(result, ToActiveProxyRequestDTO(item, now))
	}
	return &ListActiveProxyRequestsOutputDTO{Body: ActiveProxyRequestSnapshotDTO{ServerTime: now, Items: result}}, nil
}

func (h *proxyRequestHandler) get(_ context.Context, input *GetActiveProxyRequestInputDTO) (*GetActiveProxyRequestOutputDTO, error) {
	if h == nil || h.query == nil || input == nil {
		return nil, huma.Error404NotFound("proxy request not found")
	}
	item, ok := h.query.GetActive(input.TraceID)
	if !ok {
		return nil, huma.Error404NotFound("proxy request not found")
	}
	return &GetActiveProxyRequestOutputDTO{Body: ToActiveProxyRequestDTO(item, utilNowUTC())}, nil
}

func (h *proxyRequestHandler) retry(_ context.Context, input *RetryActiveProxyRequestInputDTO) (*RetryActiveProxyRequestOutputDTO, error) {
	if h == nil || h.commands == nil || input == nil {
		return nil, huma.Error404NotFound("proxy request not found")
	}
	currentAttempt, err := utilParseAttemptETag(input.IfMatch)
	if err != nil {
		return nil, utilNewAPIError(http.StatusBadRequest, "invalid_retry_precondition", "If-Match must identify the current attempt")
	}
	result, err := h.commands.RetryByTraceID(input.TraceID, currentAttempt, exchange.TriggerAdminAPI)
	if err != nil {
		return nil, utilRetryAPIError(err)
	}
	return &RetryActiveProxyRequestOutputDTO{
		Status: http.StatusAccepted,
		Body: RetryActiveProxyRequestResponseDTO{
			Request: ToActiveProxyRequestDTO(result.Exchange, utilNowUTC()),
		},
	}, nil
}
