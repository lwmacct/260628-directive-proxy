package handler

import (
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/proxyrequest"
)

func ToActiveProxyRequestDTO(item proxyrequest.ActiveRequest, now time.Time) ActiveProxyRequestDTO {
	waiting := now.Sub(item.AttemptStartedAt).Milliseconds()
	if waiting < 0 {
		waiting = 0
	}
	return ActiveProxyRequestDTO{
		TraceID:          item.TraceID,
		State:            string(item.State),
		Method:           item.Method,
		URL:              item.URL,
		TargetURL:        item.TargetURL,
		StartedAt:        item.StartedAt,
		Attempt:          item.Attempt,
		AttemptStartedAt: item.AttemptStartedAt,
		WaitingMillis:    waiting,
		RetryableAt:      item.RetryableAt,
		Retryable:        !now.Before(item.RetryableAt) && item.State == proxyrequest.StateAwaitingResponse && item.Attempt < item.MaxAttempts,
		MaxAttempts:      item.MaxAttempts,
	}
}
