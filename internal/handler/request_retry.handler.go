package handler

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/lwmacct/260628-directive-proxy/internal/core/exchange"
	"github.com/lwmacct/260628-directive-proxy/internal/core/retry"
)

type requestRetryHandler struct{ commands ExchangeCommands }

func RegisterRequestRetry(api huma.API, commands ExchangeCommands) {
	handler := &requestRetryHandler{commands: commands}
	huma.Register(api, huma.Operation{
		OperationID: "request-proxy-retry",
		Method:      http.MethodPut,
		Path:        "/api/public/retry",
		Summary:     "Retry an owned active proxy request using its retry ID",
	}, handler.retry)
}

func (h *requestRetryHandler) retry(_ context.Context, input *RequestRetryInputDTO) (*RequestRetryOutputDTO, error) {
	if h == nil || h.commands == nil || input == nil {
		return nil, utilNewAPIError(http.StatusServiceUnavailable, "exchange_manager_unavailable", "exchange manager is unavailable")
	}
	currentAttempt, err := utilParseAttemptETag(input.IfMatch)
	if err != nil {
		return nil, utilNewAPIError(http.StatusBadRequest, "invalid_retry_precondition", "If-Match must identify the current attempt")
	}
	digest, err := retry.IDDigest(input.RetryID)
	if err != nil {
		return nil, utilNewAPIError(http.StatusNotFound, "proxy_request_not_found", "proxy request was not found")
	}
	result, err := h.commands.RetryByRetryID(digest, currentAttempt, exchange.TriggerRequesterAPI)
	if err != nil {
		return nil, utilRetryAPIError(err)
	}
	return &RequestRetryOutputDTO{
		Status: http.StatusAccepted,
		Body: RequestRetryResponseDTO{
			TraceID:        result.Exchange.TraceID,
			RetryID:        input.RetryID,
			CurrentAttempt: currentAttempt,
			NextAttempt:    result.NextAttempt,
			State:          string(result.Exchange.Phase),
		},
	}, nil
}
