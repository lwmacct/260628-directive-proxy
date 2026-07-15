package handler

import (
	"context"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/lwmacct/260628-directive-proxy/internal/core/proxyrequest"
)

type requestRetryHandler struct{ tracker proxyrequest.Tracker }

func RegisterRequestRetry(api huma.API, tracker proxyrequest.Tracker) {
	handler := &requestRetryHandler{tracker: tracker}
	huma.Register(api, huma.Operation{
		OperationID: "request-proxy-retry",
		Method:      http.MethodPut,
		Path:        "/api/public/proxy-requests/{request_id}/attempts/{next_attempt}",
		Summary:     "Retry an owned active proxy request using its capability",
	}, handler.retry)
}

func (h *requestRetryHandler) retry(_ context.Context, input *RequestRetryInputDTO) (*RequestRetryOutputDTO, error) {
	if h == nil || h.tracker == nil || input == nil {
		return nil, utilNewAPIError(http.StatusServiceUnavailable, "request_tracker_unavailable", "proxy request tracker is unavailable")
	}
	if input.NextAttempt < 2 || input.IfMatch != fmt.Sprintf("\"attempt:%d\"", input.NextAttempt-1) {
		return nil, utilNewAPIError(http.StatusBadRequest, "invalid_retry_precondition", "If-Match must identify the current attempt")
	}
	requestID, capability, err := proxyrequest.ParseRetryAuthorization(input.Authorization)
	if err != nil || requestID != input.RequestID {
		return nil, utilNewAPIError(http.StatusNotFound, "proxy_request_not_found", "proxy request was not found")
	}
	digest, err := proxyrequest.DigestCapability(requestID, capability)
	if err != nil {
		return nil, utilNewAPIError(http.StatusNotFound, "proxy_request_not_found", "proxy request was not found")
	}
	result, err := h.tracker.RetryByCapability(requestID, digest, input.NextAttempt-1, proxyrequest.RetryTriggerRequesterAPI)
	if err != nil {
		return nil, utilRetryAPIError(err)
	}
	return &RequestRetryOutputDTO{
		Status: http.StatusAccepted,
		Body: RequestRetryResponseDTO{
			TraceID:        result.Request.TraceID,
			RequestID:      result.Request.RequestID,
			CurrentAttempt: input.NextAttempt - 1,
			NextAttempt:    result.NextAttempt,
			State:          string(result.Request.State),
		},
	}, nil
}
