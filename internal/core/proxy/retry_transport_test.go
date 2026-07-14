package proxy

import (
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"

	proxyrequestadapter "github.com/lwmacct/260628-directive-proxy/internal/adapter/proxyrequest"
	"github.com/lwmacct/260628-directive-proxy/internal/core/capture"
	"github.com/lwmacct/260628-directive-proxy/internal/core/proxyrequest"
)

func TestRetryTransportReplaysBodyAfterManualRetry(t *testing.T) {
	tracker := proxyrequestadapter.NewProxyRequestService(proxyrequestadapter.ProxyRequestOptions{MaxAttempts: 3}, capture.DiscardSink{})
	inbound, err := http.NewRequest(http.MethodPost, "http://proxy.local/chat", strings.NewReader("request-body"))
	if err != nil {
		t.Fatal(err)
	}
	session := tracker.Start(inbound)
	started := make(chan struct{})
	var mu sync.Mutex
	var bodies []string
	calls := 0
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, readErr := io.ReadAll(req.Body)
		if readErr != nil {
			return nil, readErr
		}
		mu.Lock()
		calls++
		call := calls
		bodies = append(bodies, string(body))
		mu.Unlock()
		if call == 1 {
			close(started)
			<-req.Context().Done()
			return nil, req.Context().Err()
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("response")),
			Request:    req,
		}, nil
	})
	tempDir := t.TempDir()
	transport, err := NewRetryTransport(base, RetryTransportOptions{
		TempDir:          tempDir,
		MaxBodyBytes:     1024,
		MaxInflightBytes: 4096,
		ChunkBytes:       4,
	})
	if err != nil {
		t.Fatal(err)
	}
	outbound := inbound.Clone(proxyrequest.ContextWithSession(inbound.Context(), session))
	result := make(chan struct {
		response *http.Response
		err      error
	}, 1)
	go func() {
		response, roundTripErr := transport.RoundTrip(outbound)
		result <- struct {
			response *http.Response
			err      error
		}{response: response, err: roundTripErr}
	}()

	<-started
	active := tracker.ListActive()
	if len(active) != 1 || active[0].Attempt != 1 {
		t.Fatalf("unexpected active request: %#v", active)
	}
	if _, err = tracker.Retry(session.TraceID(), 1); err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	completed := <-result
	if completed.err != nil {
		t.Fatalf("round trip failed: %v", completed.err)
	}
	responseBody, _ := io.ReadAll(completed.response.Body)
	_ = completed.response.Body.Close()
	if string(responseBody) != "response" {
		t.Fatalf("unexpected response: %q", responseBody)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 2 || len(bodies) != 2 || bodies[0] != "request-body" || bodies[1] != "request-body" {
		t.Fatalf("request was not replayed: calls=%d bodies=%#v", calls, bodies)
	}
	entries, _ := os.ReadDir(tempDir)
	if len(entries) != 0 {
		t.Fatalf("replay files were not cleaned up: %#v", entries)
	}
	session.Complete()
}

func TestRetryTransportRejectsOversizedReplayBodyBeforeUpstream(t *testing.T) {
	tracker := proxyrequestadapter.NewProxyRequestService(proxyrequestadapter.ProxyRequestOptions{MaxAttempts: 2}, capture.DiscardSink{})
	req, _ := http.NewRequest(http.MethodPost, "http://proxy.local/", strings.NewReader("too-large"))
	session := tracker.Start(req)
	called := false
	transport, err := NewRetryTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, nil
	}), RetryTransportOptions{MaxBodyBytes: 3, MaxInflightBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	req = req.WithContext(proxyrequest.ContextWithSession(req.Context(), session))
	if _, err = transport.RoundTrip(req); err != ErrReplayBodyTooLarge {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Fatal("upstream was called for oversized replay body")
	}
	session.Complete()
}
