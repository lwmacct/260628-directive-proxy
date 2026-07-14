package handler

import (
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/proxyrequest"
)

func ToActiveProxyRequestDTO(item proxyrequest.ActiveRequest, now time.Time) ActiveProxyRequestDTO {
	waitStartedAt := item.AttemptStartedAt
	var upstreamStartedAt *time.Time
	var retryableAt *time.Time
	if !item.UpstreamStartedAt.IsZero() {
		value := item.UpstreamStartedAt
		upstreamStartedAt = &value
		waitStartedAt = value
	}
	if !item.RetryableAt.IsZero() {
		value := item.RetryableAt
		retryableAt = &value
	}
	waiting := now.Sub(waitStartedAt).Milliseconds()
	if waiting < 0 {
		waiting = 0
	}
	return ActiveProxyRequestDTO{
		TraceID:           item.TraceID,
		Metadata:          map[string][]string(item.Metadata),
		State:             string(item.State),
		Method:            item.Method,
		URL:               item.URL,
		TargetURL:         item.TargetURL,
		StartedAt:         item.StartedAt,
		Attempt:           item.Attempt,
		AttemptStartedAt:  item.AttemptStartedAt,
		UpstreamStartedAt: upstreamStartedAt,
		WaitingMillis:     waiting,
		RetryableAt:       retryableAt,
		Retryable:         retryableAt != nil && !now.Before(*retryableAt) && item.State == proxyrequest.StateAwaitingResponse && item.Attempt < item.MaxAttempts,
		MaxAttempts:       item.MaxAttempts,
	}
}
