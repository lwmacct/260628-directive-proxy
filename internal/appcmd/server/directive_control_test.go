package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lwmacct/260628-directive-proxy/internal/core/directive"
)

func TestDirectiveHandlerRoundTripsRecoveryDocument(t *testing.T) {
	handler := newDirectiveHandler()
	document := `{
		"kind":"remote",
		"remote":{"source":{"type":"http","url":"https://resolver.example/v1/directive","key":"team-a/service-a"}},
		"recovery":{
			"controller":{"url":"https://controller.example/recovery"},
			"triggers":{"unexpected_status":{"expected":[{"from":200,"to":299}]}},
			"budget":{"max_attempts":3}
		}
	}`
	encodeRecorder := httptest.NewRecorder()
	handler.ServeHTTP(encodeRecorder, httptest.NewRequest(http.MethodPost, directiveAPIPath+"encode", strings.NewReader(document)))
	if encodeRecorder.Code != http.StatusOK {
		t.Fatalf("encode failed: status=%d body=%s", encodeRecorder.Code, encodeRecorder.Body.String())
	}
	var encoded directiveCodecResponse
	if err := json.Unmarshal(encodeRecorder.Body.Bytes(), &encoded); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encoded.Token, "dproxy.18.r.") || encoded.Document.Recovery == nil {
		t.Fatalf("unexpected encoded response: %#v", encoded)
	}

	decodeRecorder := httptest.NewRecorder()
	decodeBody := `{"token":"` + encoded.Token + `"}`
	handler.ServeHTTP(decodeRecorder, httptest.NewRequest(http.MethodPost, directiveAPIPath+"decode", strings.NewReader(decodeBody)))
	if decodeRecorder.Code != http.StatusOK {
		t.Fatalf("decode failed: status=%d body=%s", decodeRecorder.Code, decodeRecorder.Body.String())
	}
	var decoded directiveCodecResponse
	if err := json.Unmarshal(decodeRecorder.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Token != encoded.Token || decoded.Document.Recovery == nil || decoded.Document.Recovery.Budget.MaxElapsed != "30s" {
		t.Fatalf("unexpected decoded response: %#v", decoded)
	}
}

func TestDirectiveHandlerRejectsInvalidOrUnknownInput(t *testing.T) {
	handler := newDirectiveHandler()
	tests := []struct {
		name string
		path string
		body string
	}{
		{name: "invalid document", path: "encode", body: `{"kind":"inline","payload":{"target":{"url":"file:///tmp/upstream"}}}`},
		{name: "unknown field", path: "encode", body: `{"kind":"inline","payload":{"target":{"url":"https://example.com"}},"extra":true}`},
		{name: "invalid token", path: "decode", body: `{"token":"dproxy.999.i.invalid"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, directiveAPIPath+test.path, strings.NewReader(test.body)))
			if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), `"detail":`) {
				t.Fatalf("unexpected response: status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestDirectiveHandlerValidatesNormalizedDocument(t *testing.T) {
	handler := newDirectiveHandler()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, directiveAPIPath+"validate", strings.NewReader(
		`{"kind":"inline","payload":{"target":{"url":"https://example.com"}}}`,
	)))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"valid":true`) {
		t.Fatalf("unexpected validate response: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response directiveValidationResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Document.Kind != directive.KindInline {
		t.Fatalf("unexpected normalized document: %#v", response.Document)
	}
}

func TestDirectiveHandlerRejectsOversizedBody(t *testing.T) {
	handler := limitRequestBody(newDirectiveHandler(), 8)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, directiveAPIPath+"encode", strings.NewReader(
		`{"kind":"inline","payload":{"target":{"url":"https://example.com"}}}`,
	)))
	if recorder.Code != http.StatusRequestEntityTooLarge || !strings.Contains(recorder.Body.String(), "request body too large") {
		t.Fatalf("unexpected oversized response: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
