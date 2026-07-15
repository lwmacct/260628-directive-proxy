package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDirectiveCodecEndpoints(t *testing.T) {
	handler := NewAdminEndpoint(Services{}).Handler()
	encodeBody := []byte(`{"kind":"remote","remote":{"type":"http","url":"https://policy.example.com/v1/resolve","key":"team-a/service-a","headers":{"authorization":"Bearer secret"},"request_headers":["Content-Type","X-Tenant-*"]}}`)
	encodeResponse := httptest.NewRecorder()
	handler.ServeHTTP(encodeResponse, httptest.NewRequest(http.MethodPost, "/api/admin/directives/encode", bytes.NewReader(encodeBody)))
	if encodeResponse.Code != http.StatusOK {
		t.Fatalf("unexpected encode response: status=%d body=%s", encodeResponse.Code, encodeResponse.Body.String())
	}
	var encoded DirectiveCodecResponseDTO
	if err := json.Unmarshal(encodeResponse.Body.Bytes(), &encoded); err != nil {
		t.Fatalf("decode encode response: %v", err)
	}
	if !strings.HasPrefix(encoded.Token, "dproxy.15.r.") || encoded.Document.Remote == nil ||
		encoded.Document.Remote.Headers["Authorization"] != "Bearer secret" || len(encoded.Document.Remote.RequestHeaders) != 2 {
		t.Fatalf("unexpected encoded document: %#v", encoded)
	}

	decodeBody, _ := json.Marshal(DirectiveTokenRequestDTO{Token: encoded.Token})
	decodeResponse := httptest.NewRecorder()
	handler.ServeHTTP(decodeResponse, httptest.NewRequest(http.MethodPost, "/api/admin/directives/decode", bytes.NewReader(decodeBody)))
	if decodeResponse.Code != http.StatusOK {
		t.Fatalf("unexpected decode response: status=%d body=%s", decodeResponse.Code, decodeResponse.Body.String())
	}
	var decoded DirectiveCodecResponseDTO
	if err := json.Unmarshal(decodeResponse.Body.Bytes(), &decoded); err != nil || decoded.Token != encoded.Token {
		t.Fatalf("unexpected decode result: body=%s err=%v", decodeResponse.Body.String(), err)
	}
}

func TestDirectiveCodecRejectsInvalidDocument(t *testing.T) {
	handler := NewAdminEndpoint(Services{}).Handler()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/admin/directives/validate", strings.NewReader(`{"kind":"inline","payload":{"target":{}}}`)))
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unexpected validation response: status=%d body=%s", response.Code, response.Body.String())
	}
}
