package handler

import (
	"context"
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
		Path:        "/api/public/retry",
		Summary:     "Retry an owned active proxy request using its retry ID",
	}, handler.retry)
}

func (h *requestRetryHandler) retry(_ context.Context, input *RequestRetryInputDTO) (*RequestRetryOutputDTO, error) {
	if h == nil || h.tracker == nil || input == nil {
		return nil, utilNewAPIError(http.StatusServiceUnavailable, "request_tracker_unavailable", "proxy request tracker is unavailable")
	}
	currentAttempt, err := utilParseAttemptETag(input.IfMatch)
	if err != nil {
		return nil, utilNewAPIError(http.StatusBadRequest, "invalid_retry_precondition", "If-Match must identify the current attempt")
	}
	digest, err := proxyrequest.RetryIDDigest(input.RetryID)
	if err != nil {
		return nil, utilNewAPIError(http.StatusNotFound, "proxy_request_not_found", "proxy request was not found")
	}
	result, err := h.tracker.RetryByRetryID(digest, currentAttempt, proxyrequest.RetryTriggerRequesterAPI)
	if err != nil {
		return nil, utilRetryAPIError(err)
	}
	return &RequestRetryOutputDTO{
		Status: http.StatusAccepted,
		Body: RequestRetryResponseDTO{
			TraceID:        result.Request.TraceID,
			RetryID:        input.RetryID,
			CurrentAttempt: currentAttempt,
			NextAttempt:    result.NextAttempt,
			State:          string(result.Request.State),
		},
	}, nil
}
