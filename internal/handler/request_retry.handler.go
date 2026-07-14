package handler

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/lwmacct/260628-directive-proxy/internal/core/proxyrequest"
	"github.com/lwmacct/260628-directive-proxy/internal/core/requestmeta"
)

type requestRetryHandler struct{ tracker proxyrequest.Tracker }

func RegisterRequestRetry(api huma.API, tracker proxyrequest.Tracker) {
	handler := &requestRetryHandler{tracker: tracker}
	huma.Register(api, huma.Operation{
		OperationID: "request-proxy-retry",
		Method:      http.MethodPost,
		Path:        "/api/public/request-retries",
		Summary:     "Retry an active proxy request selected by directive metadata",
	}, handler.retry)
}

func (h *requestRetryHandler) retry(_ context.Context, input *RequestRetryInputDTO) (*RequestRetryOutputDTO, error) {
	if h == nil || h.tracker == nil || input == nil {
		return nil, utilNewAPIError(http.StatusServiceUnavailable, "request_tracker_unavailable", "proxy request tracker is unavailable")
	}
	if input.Body.ExpectedAttempt < 1 {
		return nil, utilNewAPIError(http.StatusBadRequest, "invalid_request", "expected_attempt must be greater than zero")
	}
	selector, err := requestmeta.NormalizeSelector(input.Body.Metadata)
	if err != nil {
		return nil, utilNewAPIError(http.StatusBadRequest, "invalid_metadata", "proxy request metadata is invalid")
	}
	result, err := h.tracker.RetryByMetadata(selector, input.Body.ExpectedAttempt, proxyrequest.RetryTriggerRequesterAPI)
	if err != nil {
		return nil, utilRetryAPIError(err)
	}
	return &RequestRetryOutputDTO{
		Status: http.StatusAccepted,
		Body: RequestRetryResponseDTO{
			TraceID:         result.Request.TraceID,
			PreviousAttempt: input.Body.ExpectedAttempt,
			NextAttempt:     result.NextAttempt,
			State:           string(result.Request.State),
		},
	}, nil
}
