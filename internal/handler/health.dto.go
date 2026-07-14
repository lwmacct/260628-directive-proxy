package handler

import "time"

type HealthResponseDTO struct {
	Status    string           `json:"status" example:"ok"`
	Timestamp time.Time        `json:"timestamp"`
	Capture   CaptureHealthDTO `json:"capture"`
}

type CaptureHealthDTO struct {
	Status        string     `json:"status"`
	LastFailureAt *time.Time `json:"last_failure_at,omitempty"`
}

type HealthOutputDTO struct {
	Body HealthResponseDTO
}
