package server

import (
	"bufio"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	proxyrequestadapter "github.com/lwmacct/260628-directive-proxy/internal/adapter/proxyrequest"
	"github.com/lwmacct/260628-directive-proxy/internal/config"
	corecapture "github.com/lwmacct/260628-directive-proxy/internal/core/capture"
	"github.com/lwmacct/260628-directive-proxy/internal/core/directive"
	"github.com/lwmacct/260628-directive-proxy/internal/core/proxy"
)

type serverCaptureSink struct {
	mu     sync.Mutex
	events []corecapture.Event
}

func (s *serverCaptureSink) Emit(_ string, event corecapture.Event) error {
	s.mu.Lock()
	s.events = append(s.events, event)
	s.mu.Unlock()
	return nil
}

func (s *serverCaptureSink) Close() error { return nil }
func (s *serverCaptureSink) CaptureHealth() corecapture.HealthStatus {
	return corecapture.HealthStatus{Status: "ok"}
}

func TestProxySSELeavesRetryRegistryAfterHeadersAndCapturesEachEvent(t *testing.T) {
	firstSent := make(chan struct{})
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "id: 1\nevent: token\ndata: hello\n\n")
		_ = http.NewResponseController(w).Flush()
		close(firstSent)
		<-release
		_, _ = io.WriteString(w, "data: done\n\n")
	}))
	defer upstream.Close()

	sink := &serverCaptureSink{}
	tracker := proxyrequestadapter.NewProxyRequestService(proxyrequestadapter.ProxyRequestOptions{
		RetryAfter:       0,
		MaxAttempts:      3,
		BodyChunkBytes:   8,
		MaxSSEEventBytes: 1024,
	}, sink)
	transport, err := proxy.NewRetryTransport(http.DefaultTransport, proxy.RetryTransportOptions{
		TempDir:          t.TempDir(),
		MaxBodyBytes:     1024,
		MaxInflightBytes: 4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	rt := &runtime{requests: tracker, proxyTransport: transport, captureSink: sink}
	proxyServer := httptest.NewServer(newHTTPServer(&cfg, rt).Handler)
	defer proxyServer.Close()
	token, err := directive.Encode(directive.Payload{Target: directive.TargetSection{URL: upstream.URL}})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, proxyServer.URL+"/events", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	select {
	case <-firstSent:
	case <-time.After(time.Second):
		t.Fatal("SSE event was not sent")
	}
	if active := tracker.ListActive(); len(active) != 0 {
		t.Fatalf("SSE remained retryable after response headers: %#v", active)
	}
	reader := bufio.NewReader(response.Body)
	var first strings.Builder
	for {
		line, readErr := reader.ReadString('\n')
		if readErr != nil {
			t.Fatal(readErr)
		}
		first.WriteString(line)
		if line == "\n" {
			break
		}
	}
	if first.String() != "id: 1\nevent: token\ndata: hello\n\n" {
		t.Fatalf("unexpected first SSE event: %q", first.String())
	}
	close(release)
	_, _ = io.ReadAll(reader)
	deadline := time.Now().Add(time.Second)
	var values []string
	for time.Now().Before(deadline) {
		sink.mu.Lock()
		values = values[:0]
		for _, event := range sink.events {
			if event.Kind == "response.sse.event" {
				values = append(values, event.Data["data"].(string))
			}
		}
		sink.mu.Unlock()
		if len(values) == 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if len(values) != 2 || values[0] != "hello" || values[1] != "done" {
		sink.mu.Lock()
		allEvents := append([]corecapture.Event(nil), sink.events...)
		sink.mu.Unlock()
		t.Fatalf("unexpected captured SSE events: values=%#v events=%#v", values, allEvents)
	}
}
