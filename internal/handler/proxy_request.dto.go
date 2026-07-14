package handler

import "time"

type ListActiveProxyRequestsOutputDTO struct {
	Body ActiveProxyRequestSnapshotDTO
}

type ActiveProxyRequestSnapshotDTO struct {
	ServerTime time.Time               `json:"server_time"`
	Items      []ActiveProxyRequestDTO `json:"items"`
}

type ActiveProxyRequestDTO struct {
	TraceID           string              `json:"trace_id"`
	Metadata          map[string][]string `json:"metadata,omitempty"`
	State             string              `json:"state"`
	Method            string              `json:"method"`
	URL               string              `json:"url"`
	TargetURL         string              `json:"target_url"`
	StartedAt         time.Time           `json:"started_at"`
	Attempt           int                 `json:"attempt"`
	AttemptStartedAt  time.Time           `json:"attempt_started_at"`
	UpstreamStartedAt *time.Time          `json:"upstream_started_at,omitempty"`
	WaitingMillis     int64               `json:"waiting_millis"`
	RetryableAt       *time.Time          `json:"retryable_at,omitempty"`
	Retryable         bool                `json:"retryable"`
	MaxAttempts       int                 `json:"max_attempts"`
}

type GetActiveProxyRequestInputDTO struct {
	TraceID string `path:"trace_id" pattern:"^[0-9a-f]{32}$" doc:"Proxy request tracking ID"`
}

type GetActiveProxyRequestOutputDTO struct {
	Body ActiveProxyRequestDTO
}

type RetryActiveProxyRequestInputDTO struct {
	TraceID string `path:"trace_id" pattern:"^[0-9a-f]{32}$" doc:"Proxy request tracking ID"`
	Body    RetryActiveProxyRequestRequestDTO
}

type RetryActiveProxyRequestRequestDTO struct {
	ExpectedAttempt int `json:"expected_attempt" minimum:"1" doc:"Current attempt number used as a compare-and-swap guard"`
}

type RetryActiveProxyRequestOutputDTO struct {
	Status int `status:"202"`
	Body   RetryActiveProxyRequestResponseDTO
}

type RetryActiveProxyRequestResponseDTO struct {
	Request     ActiveProxyRequestDTO `json:"request"`
	NextAttempt int                   `json:"next_attempt"`
}
