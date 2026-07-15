package server

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	proxyrequestadapter "github.com/lwmacct/260628-directive-proxy/internal/adapter/proxyrequest"
	"github.com/lwmacct/260628-directive-proxy/internal/config"
	"github.com/lwmacct/260628-directive-proxy/internal/core/directive"
	"github.com/lwmacct/260628-directive-proxy/internal/core/observability"
	"github.com/lwmacct/260628-directive-proxy/internal/core/proxy"
	captureplugin "github.com/lwmacct/260628-directive-proxy/internal/plugin/capture"
	llmusageplugin "github.com/lwmacct/260628-directive-proxy/internal/plugin/llmusage"
	recordoutput "github.com/lwmacct/260628-directive-proxy/internal/testutil/recordoutput"
)

func TestProxySSELeavesRetryRegistryAfterHeadersAndCapturesEachEvent(t *testing.T) {
	firstSent := make(chan struct{})
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if value := r.Header.Get("X-Dproxy-Request-ID"); value != "" {
			t.Errorf("request metadata leaked upstream: %q", value)
		}
		if r.Header.Get("Dproxy-Retry-ID") != "" {
			t.Errorf("retry identity leaked upstream: %#v", r.Header)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "id: 1\nevent: token\ndata: hello\n\n")
		_ = http.NewResponseController(w).Flush()
		close(firstSent)
		<-release
		_, _ = io.WriteString(w, "data: done\n\n")
	}))
	defer upstream.Close()

	output := recordoutput.New("memory")
	pipeline, err := observability.NewPipeline(context.Background(), []observability.Plugin{captureplugin.New(captureplugin.Config{
		BodyChunkBytes: 8, MaxSSEEventBytes: 1024,
	})}, observability.SinkConfig{Sink: output, QueueCapacity: 1024, QueueMaxBytes: 8 << 20})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pipeline.Close(context.Background()) })
	tracker := proxyrequestadapter.NewProxyRequestService(proxyrequestadapter.ProxyRequestOptions{
		MaxAttempts: 3,
	}, pipeline)
	transport, err := proxy.NewRetryTransport(http.DefaultTransport, proxy.RetryTransportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	rt := &runtime{requests: tracker, bodyMemory: newTestBodyMemory(cfg.Proxy.BodyMemory), proxyTransport: transport, observability: pipeline}
	proxyServer := httptest.NewServer(newHTTPServer(&cfg, rt).Handler)
	defer proxyServer.Close()
	token, err := directive.Encode(directive.Payload{
		Target:  directive.TargetSection{URL: upstream.URL},
		Plugins: map[string]json.RawMessage{"capture": json.RawMessage(`{}`)},
		Headers: &directive.HeaderSection{Ops: []directive.HeaderOp{{
			Op: "=", Name: "X-Dproxy-Request-ID", Values: []string{"capture-request"},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, proxyServer.URL+"/events", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	setTestRetryID(req, 1)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := response.Body.Close(); closeErr != nil {
			t.Errorf("close proxy response body: %v", closeErr)
		}
	}()
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
	var metadataCaptured bool
	for time.Now().Before(deadline) {
		values = values[:0]
		for _, event := range output.Records() {
			if event.Topic == "capture.response.sse.event" {
				values = append(values, event.Data["data"].(string))
			}
			if event.Topic == "capture.request.metadata.bound" {
				metadata := event.Data["metadata"].(map[string][]string)
				metadataCaptured = len(metadata["X-Dproxy-Request-Id"]) == 1 && metadata["X-Dproxy-Request-Id"][0] == "capture-request"
			}
		}
		if len(values) == 2 && metadataCaptured {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if len(values) != 2 || values[0] != "hello" || values[1] != "done" || !metadataCaptured {
		allEvents := output.Records()
		t.Fatalf("unexpected captured events: values=%#v metadata=%t events=%#v", values, metadataCaptured, allEvents)
	}
}

func TestProxyLLMUsagePluginEmitsNormalizedUsageFromUpstreamBody(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"resp_proxy","object":"response","model":"gpt-test","usage":{"input_tokens":8,"output_tokens":5,"total_tokens":13}}`)
	}))
	defer upstream.Close()

	output := recordoutput.New("memory")
	pipeline, err := observability.NewPipeline(context.Background(), []observability.Plugin{llmusageplugin.New(llmusageplugin.Config{})}, observability.SinkConfig{Sink: output, QueueCapacity: 128, QueueMaxBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pipeline.Close(context.Background()) })
	tracker := proxyrequestadapter.NewProxyRequestService(proxyrequestadapter.ProxyRequestOptions{MaxAttempts: 2}, pipeline)
	transport, err := proxy.NewRetryTransport(http.DefaultTransport, proxy.RetryTransportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	rt := &runtime{requests: tracker, bodyMemory: newTestBodyMemory(cfg.Proxy.BodyMemory), proxyTransport: transport, observability: pipeline}
	proxyServer := httptest.NewServer(newHTTPServer(&cfg, rt).Handler)
	defer proxyServer.Close()
	token, err := directive.Encode(directive.Payload{
		Target: directive.TargetSection{URL: upstream.URL},
		Plugins: map[string]json.RawMessage{
			llmusageplugin.DirectiveName: json.RawMessage(`{"protocol":"openai.responses","labels":{"provider":"test"}}`),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, proxyServer.URL, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	setTestRetryID(req, 2)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil || !strings.Contains(string(body), "resp_proxy") {
		t.Fatalf("unexpected proxy response: body=%q err=%v", body, readErr)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		for _, record := range output.Records() {
			if record.Topic != "llm.usage.observed" {
				continue
			}
			usage := record.Data["usage"].(map[string]any)
			if usage["total_tokens"] != int64(13) || record.Data["response_id"] != "resp_proxy" {
				t.Fatalf("unexpected usage record: %#v", record)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("usage record was not emitted: %#v", output.Records())
}
