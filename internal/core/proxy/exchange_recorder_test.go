package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestExchangeRecorderCapturesProxyRequestAndResponse(t *testing.T) {
	target, err := url.Parse("https://api.example.test/base")
	if err != nil {
		t.Fatalf("parse target failed: %v", err)
	}
	recorder := NewExchangeRecorder(10, 1024)
	handler := NewHandler(
		resolverFunc(func(*http.Request) (*Plan, error) {
			return &Plan{Target: target, JoinPath: true}, nil
		}),
		roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatalf("read request body failed: %v", err)
			}
			if string(body) != "hello" {
				t.Fatalf("unexpected request body: %q", body)
			}
			return &http.Response{
				StatusCode: http.StatusCreated,
				Status:     "201 Created",
				Header:     http.Header{"Content-Type": {"text/plain"}},
				Body:       io.NopCloser(strings.NewReader("world")),
				Request:    req,
			}, nil
		}),
		HandlerOptions{Recorder: recorder, IDGenerator: fixedIDGenerator{id: "req-1"}},
	)

	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/chat", strings.NewReader("hello"))
	req.Header.Set("Authorization", "Bearer secret")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, req)

	if response.Code != http.StatusCreated {
		t.Fatalf("unexpected status: %d", response.Code)
	}
	snapshot := recorder.Snapshot(0)
	if len(snapshot.Items) != 1 {
		t.Fatalf("expected one record, got %d", len(snapshot.Items))
	}
	record := snapshot.Items[0]
	if record.RequestID != "req-1" || record.StatusCode != http.StatusCreated {
		t.Fatalf("unexpected record metadata: %#v", record)
	}
	if record.TargetURL != "https://api.example.test/base/v1/chat" {
		t.Fatalf("unexpected target url: %q", record.TargetURL)
	}
	if record.RequestBody.Text != "hello" || record.ResponseBody.Text != "world" {
		t.Fatalf("unexpected body capture: %#v %#v", record.RequestBody, record.ResponseBody)
	}
	if got := record.RequestHeaders.Get("Authorization"); got != "<redacted>" {
		t.Fatalf("authorization header was not redacted: %q", got)
	}
}
