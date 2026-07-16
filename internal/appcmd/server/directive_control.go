package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/lwmacct/260628-directive-proxy/internal/core/directive"
)

const directiveAPIPath = "/api/admin/directives/"

type directiveHandler struct{}

type directiveTokenRequest struct {
	Token string `json:"token"`
}

type directiveCodecResponse struct {
	Token    string             `json:"token,omitempty"`
	Document directive.Document `json:"document"`
}

type directiveValidationResponse struct {
	Valid    bool               `json:"valid"`
	Document directive.Document `json:"document"`
}

type errorResponse struct {
	Detail string `json:"detail"`
}

func newDirectiveHandler() http.Handler {
	return &directiveHandler{}
}

func (*directiveHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request == nil {
		http.NotFound(writer, request)
		return
	}
	var handle func(http.ResponseWriter, *http.Request)
	switch request.URL.Path {
	case directiveAPIPath + "encode":
		handle = handleDirectiveEncode
	case directiveAPIPath + "decode":
		handle = handleDirectiveDecode
	case directiveAPIPath + "validate":
		handle = handleDirectiveValidate
	default:
		http.NotFound(writer, request)
		return
	}
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", http.MethodPost)
		writeJSON(writer, http.StatusMethodNotAllowed, errorResponse{Detail: "method not allowed"})
		return
	}
	handle(writer, request)
}

func handleDirectiveEncode(writer http.ResponseWriter, request *http.Request) {
	var document directive.Document
	if err := decodeJSON(request, &document); err != nil {
		writeDirectiveInputError(writer, err, "invalid directive document")
		return
	}
	normalized, err := directive.ValidateDocument(document)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Detail: "invalid directive document"})
		return
	}
	token, err := directive.EncodeDocument(normalized)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Detail: "invalid directive document"})
		return
	}
	writeJSON(writer, http.StatusOK, directiveCodecResponse{Token: token, Document: normalized})
}

func handleDirectiveDecode(writer http.ResponseWriter, request *http.Request) {
	var input directiveTokenRequest
	if err := decodeJSON(request, &input); err != nil {
		writeDirectiveInputError(writer, err, "invalid directive token")
		return
	}
	if input.Token == "" {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Detail: "invalid directive token"})
		return
	}
	document, err := directive.Decode(input.Token)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Detail: "invalid directive token"})
		return
	}
	token, err := directive.EncodeDocument(document)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Detail: "invalid directive token"})
		return
	}
	writeJSON(writer, http.StatusOK, directiveCodecResponse{Token: token, Document: document})
}

func handleDirectiveValidate(writer http.ResponseWriter, request *http.Request) {
	var document directive.Document
	if err := decodeJSON(request, &document); err != nil {
		writeDirectiveInputError(writer, err, "invalid directive document")
		return
	}
	normalized, err := directive.ValidateDocument(document)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Detail: "invalid directive document"})
		return
	}
	writeJSON(writer, http.StatusOK, directiveValidationResponse{Valid: true, Document: normalized})
}

func decodeJSON(request *http.Request, destination any) error {
	if request.Body == nil {
		return io.EOF
	}
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body contains multiple JSON values")
	}
	return nil
}

func writeDirectiveInputError(writer http.ResponseWriter, err error, detail string) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeJSON(writer, http.StatusRequestEntityTooLarge, errorResponse{Detail: "request body too large"})
		return
	}
	writeJSON(writer, http.StatusBadRequest, errorResponse{Detail: detail})
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
