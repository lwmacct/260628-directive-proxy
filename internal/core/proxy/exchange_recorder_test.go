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
	recorder.Configure(true, 0, -1)
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
		HandlerOptions{Recorder: recorder},
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
	if record.StatusCode != http.StatusCreated {
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

func TestExchangeRecorderIsDisabledByDefault(t *testing.T) {
	recorder := NewExchangeRecorder(10, 1024)
	handler := NewHandler(
		resolverFunc(func(*http.Request) (*Plan, error) {
			target, err := url.Parse("https://api.example.test")
			if err != nil {
				t.Fatalf("parse target failed: %v", err)
			}
			return &Plan{Target: target}, nil
		}),
		roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Body:       io.NopCloser(strings.NewReader("ok")),
				Request:    req,
			}, nil
		}),
		HandlerOptions{Recorder: recorder},
	)

	req := httptest.NewRequest(http.MethodGet, "http://proxy.local/v1/chat", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, req)

	if response.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", response.Code)
	}
	snapshot := recorder.Snapshot(0)
	if snapshot.Enabled || len(snapshot.Items) != 0 || snapshot.Total != 0 {
		t.Fatalf("unexpected disabled snapshot: %#v", snapshot)
	}
}

func TestExchangeRecorderClearResetsRecords(t *testing.T) {
	recorder := NewExchangeRecorder(2, 16)
	recorder.Configure(true, 0, -1)
	recorder.add(ExchangeRecord{ID: 1, Method: http.MethodGet})

	snapshot := recorder.Clear()

	if !snapshot.Enabled || snapshot.Total != 0 || len(snapshot.Items) != 0 {
		t.Fatalf("unexpected clear snapshot: %#v", snapshot)
	}
}
