package handler

import "time"

type HealthResponseDTO struct {
	Status    string    `json:"status" example:"ok"`
	Timestamp time.Time `json:"timestamp"`
}

type HealthOutputDTO struct {
	Body HealthResponseDTO
}
