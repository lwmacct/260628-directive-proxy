package handler

type RequestRetryInputDTO struct {
	Body RequestRetryRequestDTO
}

type RequestRetryRequestDTO struct {
	Metadata        map[string]string `json:"metadata"`
	ExpectedAttempt int               `json:"expected_attempt"`
}

type RequestRetryOutputDTO struct {
	Status int `status:"202"`
	Body   RequestRetryResponseDTO
}

type RequestRetryResponseDTO struct {
	TraceID         string `json:"trace_id"`
	PreviousAttempt int    `json:"previous_attempt"`
	NextAttempt     int    `json:"next_attempt"`
	State           string `json:"state"`
}
