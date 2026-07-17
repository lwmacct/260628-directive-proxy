package server

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/config"
	"github.com/lwmacct/260628-directive-proxy/internal/core/directive"
	"github.com/lwmacct/260628-directive-proxy/internal/core/event"
	"github.com/lwmacct/260628-directive-proxy/internal/core/exchange"
	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
	"github.com/lwmacct/260628-directive-proxy/internal/core/proxy"
	"github.com/lwmacct/260628-directive-proxy/internal/modules/capture"
	"github.com/lwmacct/260628-directive-proxy/internal/modules/llmusage"
	recordoutput "github.com/lwmacct/260628-directive-proxy/internal/testutil/recordoutput"
)

func TestProxySSECapturesEachEventAfterResponseHeaders(t *testing.T) {
	firstSent := make(chan struct{})
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if value := r.Header.Get("X-Dproxy-Request-ID"); value != "" {
			t.Errorf("request metadata leaked upstream: %q", value)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Upstream", "raw")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "id: 1\nevent: token\ndata: hello\n\n")
		_ = http.NewResponseController(w).Flush()
		close(firstSent)
		<-release
		_, _ = io.WriteString(w, "data: done\n\n")
	}))
	defer upstream.Close()

	output := recordoutput.New("memory")
	dispatcher, err := event.NewDispatcher(context.Background(), event.Config{Sink: output, QueueMaxRecords: 1024, QueueMaxBytes: 8 << 20})
	if err != nil {
		t.Fatal(err)
	}
	moduleRuntime, err := module.NewRuntime([]module.Definition{capture.New()}, dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { moduleRuntime.Close(); _ = dispatcher.Close(context.Background()) })
	manager := exchange.NewManager(exchange.ManagerOptions{
		MaxAttempts: 3,
	}, moduleRuntime)
	transport, err := proxy.NewRecoveryTransport(http.DefaultTransport, proxy.RecoveryTransportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig().Server
	rt := &runtime{exchangeFactory: manager, bodyStore: newTestBodyStore(cfg.Proxy.BodyStore), proxyTransport: transport, moduleRuntime: moduleRuntime, eventOutput: dispatcher}
	proxyServer := httptest.NewServer(newHTTPServer(&cfg, rt).Handler)
	defer proxyServer.Close()
	token, err := directive.Encode(directive.Payload{
		Target: directive.TargetSection{URL: upstream.URL},
		Program: module.Program{Request: []module.Spec{{
			ID: "capture", Module: capture.Name, Config: []byte(`{"body-chunk-bytes":8}`),
		}}},
		Headers: &directive.HeaderPolicy{Mutations: []directive.HeaderMutation{
			{Side: directive.HeaderSideRequest, Action: directive.HeaderActionSet, Name: "X-Dproxy-Request-ID", Values: []string{"capture-request"}},
			{Side: directive.HeaderSideResponse, Action: directive.HeaderActionRemove, Name: "X-Upstream"},
			{Side: directive.HeaderSideResponse, Action: directive.HeaderActionSet, Name: "X-Downstream", Values: []string{"rewritten"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, proxyServer.URL+"/events", nil)
	req.Header.Set("Authorization", "Bearer "+token)
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
	if response.Header.Get("X-Upstream") != "" || response.Header.Get("X-Downstream") != "rewritten" {
		t.Fatalf("unexpected rewritten response headers: %#v", response.Header)
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
	var responseHeadersCaptured bool
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
			if event.Topic == "capture.response.headers" {
				headers := event.Data["headers"].(map[string][]string)
				responseHeadersCaptured = len(headers["X-Downstream"]) == 1 && headers["X-Downstream"][0] == "rewritten" && len(headers["X-Upstream"]) == 0
			}
		}
		if len(values) == 2 && metadataCaptured && responseHeadersCaptured {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if len(values) != 2 || values[0] != "hello" || values[1] != "done" || !metadataCaptured || !responseHeadersCaptured {
		allEvents := output.Records()
		t.Fatalf("unexpected captured events: values=%#v metadata=%t response_headers=%t events=%#v", values, metadataCaptured, responseHeadersCaptured, allEvents)
	}
}

func TestDisabledFluentKeepsModuleRuntimeActiveAndProxiesNormally(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	moduleRuntime, err := module.NewRuntime([]module.Definition{capture.New()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	manager := exchange.NewManager(exchange.ManagerOptions{MaxAttempts: 3}, moduleRuntime)
	transport, err := proxy.NewRecoveryTransport(http.DefaultTransport, proxy.RecoveryTransportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig().Server
	rt := &runtime{exchangeFactory: manager, bodyStore: newTestBodyStore(cfg.Proxy.BodyStore), proxyTransport: transport, moduleRuntime: moduleRuntime}
	token, err := directive.Encode(directive.Payload{
		Target:  directive.TargetSection{URL: upstream.URL},
		Program: module.Program{Request: []module.Spec{{ID: "capture", Module: capture.Name, Config: []byte(`{}`)}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "http://proxy.local/resource", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()

	newHTTPServer(&cfg, rt).Handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("disabled event output affected proxying: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	health := moduleRuntime.ModuleHealth()
	if health.Status != "ok" || health.Modules[capture.Name].Status != "ok" {
		t.Fatalf("unexpected module runtime health: %#v", health)
	}
	if outputHealth := (*event.Dispatcher)(nil).EventOutputHealth(); outputHealth.Status != "disabled" {
		t.Fatalf("unexpected disabled event output health: %#v", outputHealth)
	}
}

func TestDisabledFluentKeepsModuleRuntimeWithoutCreatingDispatcher(t *testing.T) {
	cfg := config.DefaultConfig().Server.Fluent
	cfg.Endpoint = "tcp://127.0.0.1:1"
	dispatcher, err := newEventDispatcher(t.Context(), cfg)
	if err != nil {
		t.Fatalf("disabled Fluent attempted startup: %v", err)
	}
	moduleRuntime, err := newModuleRuntime(dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	if dispatcher != nil || moduleRuntime.ModuleHealth().Status != "ok" {
		t.Fatal("disabled Fluent affected the module runtime")
	}
}

func TestProxyLLMUsageModuleEmitsNormalizedUsageFromJSONProjection(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"resp_proxy","object":"response","model":"gpt-test","usage":{"input_tokens":8,"output_tokens":5,"total_tokens":13}}`)
	}))
	defer upstream.Close()

	output := recordoutput.New("memory")
	dispatcher, err := event.NewDispatcher(context.Background(), event.Config{Sink: output, QueueMaxRecords: 128, QueueMaxBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	moduleRuntime, err := module.NewRuntime([]module.Definition{llmusage.New()}, dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { moduleRuntime.Close(); _ = dispatcher.Close(context.Background()) })
	manager := exchange.NewManager(exchange.ManagerOptions{MaxAttempts: 2}, moduleRuntime)
	transport, err := proxy.NewRecoveryTransport(http.DefaultTransport, proxy.RecoveryTransportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig().Server
	rt := &runtime{exchangeFactory: manager, bodyStore: newTestBodyStore(cfg.Proxy.BodyStore), proxyTransport: transport, moduleRuntime: moduleRuntime, eventOutput: dispatcher}
	proxyServer := httptest.NewServer(newHTTPServer(&cfg, rt).Handler)
	defer proxyServer.Close()
	token, err := directive.Encode(directive.Payload{
		Target: directive.TargetSection{URL: upstream.URL},
		Program: module.Program{Attempt: []module.Spec{{
			ID: "usage", Module: llmusage.Name, Config: []byte(`{"protocol":"openai.responses","labels":{"provider":"test"}}`),
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, proxyServer.URL, nil)
	req.Header.Set("Authorization", "Bearer "+token)
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
