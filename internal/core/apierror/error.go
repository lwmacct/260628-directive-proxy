package apierror

type Body struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Error struct {
	Status int  `json:"-"`
	Body   Body `json:"error"`
}

func New(status int, code, message string) *Error {
	return &Error{Status: status, Body: Body{Code: code, Message: message}}
}

func (e *Error) Error() string {
	if e == nil {
		return "api error"
	}
	return e.Body.Message
}

func (e *Error) GetStatus() int {
	if e == nil {
		return 500
	}
	return e.Status
}
