package handler

type RequestRetryInputDTO struct {
	RetryID string `header:"Dproxy-Retry-ID"`
	IfMatch string `header:"If-Match"`
}

type RequestRetryOutputDTO struct {
	Status int `status:"202"`
	Body   RequestRetryResponseDTO
}

type RequestRetryResponseDTO struct {
	TraceID        string `json:"trace_id"`
	RetryID        string `json:"retry_id"`
	CurrentAttempt int    `json:"current_attempt"`
	NextAttempt    int    `json:"next_attempt"`
	State          string `json:"state"`
}
