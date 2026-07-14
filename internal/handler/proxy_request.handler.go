package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/lwmacct/260628-directive-proxy/internal/core/proxyrequest"
)

type proxyRequestHandler struct {
	tracker proxyrequest.Tracker
}

func RegisterProxyRequest(api huma.API, tracker proxyrequest.Tracker) {
	handler := &proxyRequestHandler{tracker: tracker}
	huma.Register(api, huma.Operation{
		OperationID: "list-proxy-requests-awaiting-response",
		Method:      http.MethodGet,
		Path:        "/proxy-requests/awaiting-response",
		Summary:     "List proxy requests awaiting final upstream response headers",
	}, handler.list)
	huma.Register(api, huma.Operation{
		OperationID: "get-active-proxy-request",
		Method:      http.MethodGet,
		Path:        "/proxy-requests/{trace_id}",
		Summary:     "Get one active proxy request",
	}, handler.get)
	huma.Register(api, huma.Operation{
		OperationID: "retry-active-proxy-request",
		Method:      http.MethodPost,
		Path:        "/proxy-requests/{trace_id}/retry",
		Summary:     "Cancel and retry an active upstream request attempt",
	}, handler.retry)
}

func (h *proxyRequestHandler) list(_ context.Context, _ *struct{}) (*ListActiveProxyRequestsOutputDTO, error) {
	now := utilNowUTC()
	if h == nil || h.tracker == nil {
		return &ListActiveProxyRequestsOutputDTO{Body: ActiveProxyRequestSnapshotDTO{ServerTime: now, Items: []ActiveProxyRequestDTO{}}}, nil
	}
	items := h.tracker.ListActive()
	result := make([]ActiveProxyRequestDTO, 0, len(items))
	for _, item := range items {
		result = append(result, ToActiveProxyRequestDTO(item, now))
	}
	return &ListActiveProxyRequestsOutputDTO{Body: ActiveProxyRequestSnapshotDTO{ServerTime: now, Items: result}}, nil
}

func (h *proxyRequestHandler) get(_ context.Context, input *GetActiveProxyRequestInputDTO) (*GetActiveProxyRequestOutputDTO, error) {
	if h == nil || h.tracker == nil || input == nil {
		return nil, huma.Error404NotFound("proxy request not found")
	}
	item, ok := h.tracker.GetActive(input.TraceID)
	if !ok {
		return nil, huma.Error404NotFound("proxy request not found")
	}
	return &GetActiveProxyRequestOutputDTO{Body: ToActiveProxyRequestDTO(item, utilNowUTC())}, nil
}

func (h *proxyRequestHandler) retry(_ context.Context, input *RetryActiveProxyRequestInputDTO) (*RetryActiveProxyRequestOutputDTO, error) {
	if h == nil || h.tracker == nil || input == nil {
		return nil, huma.Error404NotFound("proxy request not found")
	}
	result, err := h.tracker.Retry(input.TraceID, input.Body.ExpectedAttempt)
	if err != nil {
		switch {
		case errors.Is(err, proxyrequest.ErrNotFound):
			return nil, huma.Error404NotFound("proxy request not found")
		case errors.Is(err, proxyrequest.ErrMaxAttempts):
			return nil, huma.Error429TooManyRequests("proxy request maximum attempts reached")
		case errors.Is(err, proxyrequest.ErrRetryNotReady):
			return nil, huma.Error409Conflict("proxy request has not reached its retry threshold")
		default:
			return nil, huma.Error409Conflict("proxy request state changed")
		}
	}
	return &RetryActiveProxyRequestOutputDTO{
		Status: http.StatusAccepted,
		Body: RetryActiveProxyRequestResponseDTO{
			Request:     ToActiveProxyRequestDTO(result.Request, utilNowUTC()),
			NextAttempt: result.NextAttempt,
		},
	}, nil
}
