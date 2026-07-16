package handler

import (
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/exchange"
)

func ToActiveProxyRequestDTO(item exchange.Snapshot, now time.Time) ActiveProxyRequestDTO {
	waitStartedAt := item.AttemptStartedAt
	if waitStartedAt.IsZero() {
		waitStartedAt = item.StartedAt
	}
	var upstreamStartedAt *time.Time
	if !item.UpstreamStartedAt.IsZero() {
		value := item.UpstreamStartedAt
		upstreamStartedAt = &value
		waitStartedAt = value
	}
	waiting := now.Sub(waitStartedAt).Milliseconds()
	if waiting < 0 {
		waiting = 0
	}
	return ActiveProxyRequestDTO{
		TraceID:           item.TraceID,
		HasRetryID:        item.HasRetryID,
		Metadata:          map[string][]string(item.Metadata),
		State:             string(item.Phase),
		Method:            item.Method,
		URL:               item.URL,
		TargetURL:         item.TargetURL,
		StartedAt:         item.StartedAt,
		Attempt:           item.Attempt,
		AttemptStartedAt:  item.AttemptStartedAt,
		UpstreamStartedAt: upstreamStartedAt,
		WaitingMillis:     waiting,
		Retryable:         item.Phase == exchange.PhaseAwaitingResponse && item.Attempt < item.MaxAttempts,
		MaxAttempts:       item.MaxAttempts,
	}
}
