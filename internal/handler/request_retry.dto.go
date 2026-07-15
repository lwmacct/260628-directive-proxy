package handler

type RequestRetryInputDTO struct {
	RequestID     string `path:"request_id" pattern:"^[A-Za-z0-9_-]{22}$"`
	NextAttempt   int    `path:"next_attempt" minimum:"2"`
	Authorization string `header:"Authorization"`
	IfMatch       string `header:"If-Match"`
}

type RequestRetryOutputDTO struct {
	Status int `status:"202"`
	Body   RequestRetryResponseDTO
}

type RequestRetryResponseDTO struct {
	TraceID        string `json:"trace_id"`
	RequestID      string `json:"request_id"`
	CurrentAttempt int    `json:"current_attempt"`
	NextAttempt    int    `json:"next_attempt"`
	State          string `json:"state"`
}
