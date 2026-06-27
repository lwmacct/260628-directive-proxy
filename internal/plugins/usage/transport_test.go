package usage

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/eventbus"
	"github.com/lwmacct/260628-llm-relay-dproxy/internal/plugins/capture"
)

type memorySink struct {
	events []eventbus.Event
}

func (s *memorySink) Publish(_ context.Context, event eventbus.Event) error {
	s.events = append(s.events, event)
	return nil
}

func (s *memorySink) Close(context.Context) error {
	return nil
}

type contextMemorySink struct {
	events      []eventbus.Event
	ctxCanceled []bool
}

func (s *contextMemorySink) Publish(ctx context.Context, event eventbus.Event) error {
	s.events = append(s.events, event)
	s.ctxCanceled = append(s.ctxCanceled, ctx != nil && ctx.Err() != nil)
	return nil
}

func (s *contextMemorySink) Close(context.Context) error {
	return nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func testOptions() Options {
	return Options{
		Mode: ModeInclude,
		Fields: []string{
			"id",
			"model",
			"completed_at",
			"created_at",
			"tool_choice",
			"top_logprobs",
			"top_p",
			"usage",
		},
	}
}

func TestTransportExtractsUsageWithoutCapturePolicy(t *testing.T) {
	body := strings.Join([]string{
		"event: response.output_text.delta",
		`data: {"type":"response.output_text.delta","delta":"ignored"}`,
		"",
		"event: response.completed",
		`data: {"type":"response.completed","response":{"id":"resp_1","model":"gpt-5.5","completed_at":1777283704,"created_at":1777283686,"tool_choice":"auto","top_logprobs":0,"top_p":0.98,"output":[{"big":"ignored"}],"usage":{"input_tokens":10,"input_tokens_details":{"cached_tokens":7},"output_tokens":3,"output_tokens_details":{"reasoning_tokens":2},"total_tokens":13,"new_detail":{"foo":true}}}}`,
		"",
	}, "\n")

	sink := &memorySink{}
	transport := NewTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	}), sink, testOptions())

	req, err := http.NewRequest(http.MethodPost, "https://api.example.com/v1/responses", nil)
	if err != nil {
		t.Fatalf("new request failed: %v", err)
	}
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("round trip failed: %v", err)
	}
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body failed: %v", err)
	}
	if string(got) != body {
		t.Fatalf("body changed: %q", string(got))
	}
	if len(sink.events) != 1 {
		t.Fatalf("expected one usage event, got %d", len(sink.events))
	}
	event := sink.events[0]
	if event.Type != EventTypeUsage {
		t.Fatalf("unexpected event metadata: %#v", event)
	}
	data := usageDataFromEvent(t, event)
	var usage map[string]any
	if err := json.Unmarshal(data["usage"], &usage); err != nil {
		t.Fatalf("unmarshal usage failed: %v", err)
	}
	if usage["input_tokens"] != float64(10) || usage["total_tokens"] != float64(13) {
		t.Fatalf("unexpected usage: %#v", usage)
	}
	if _, ok := usage["new_detail"].(map[string]any); !ok {
		t.Fatalf("expected flexible usage details, got %#v", usage)
	}
	if string(data["id"]) != `"resp_1"` ||
		string(data["model"]) != `"gpt-5.5"` ||
		string(data["completed_at"]) != "1777283704" ||
		string(data["created_at"]) != "1777283686" ||
		string(data["tool_choice"]) != `"auto"` ||
		string(data["top_logprobs"]) != "0" ||
		string(data["top_p"]) != "0.98" {
		t.Fatalf("unexpected data fields: %#v", data)
	}
	rawEvent, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event failed: %v", err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(rawEvent, &envelope); err != nil {
		t.Fatalf("unmarshal event failed: %v", err)
	}
	dataObject, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data object, got %#v", envelope["data"])
	}
	if _, ok := dataObject["fields"]; ok {
		t.Fatalf("did not expect nested fields in data: %s", rawEvent)
	}
	if dataObject["id"] != "resp_1" {
		t.Fatalf("unexpected flattened data: %s", rawEvent)
	}
}

func TestUsageTransportWorksBehindCaptureTransportWhenCaptureDisabled(t *testing.T) {
	body := strings.Join([]string{
		"event: response.completed",
		`data: {"type":"response.completed","response":{"usage":{"input_tokens":4,"output_tokens":5,"total_tokens":9}}}`,
		"",
	}, "\n")

	sink := &memorySink{}
	usageTransport := NewTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	}), sink, testOptions())
	transport := capture.NewTransport(usageTransport, capture.NopSink{}, capture.Options{})

	req, err := http.NewRequest(http.MethodPost, "https://api.example.com/v1/responses", nil)
	if err != nil {
		t.Fatalf("new request failed: %v", err)
	}
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("round trip failed: %v", err)
	}
	_, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	if len(sink.events) != 1 {
		t.Fatalf("expected one usage event, got %d", len(sink.events))
	}
	var usage map[string]any
	if err := json.Unmarshal(usageDataFromEvent(t, sink.events[0])["usage"], &usage); err != nil {
		t.Fatalf("unmarshal usage failed: %v", err)
	}
	if usage["total_tokens"] != float64(9) {
		t.Fatalf("unexpected usage: %#v", usage)
	}
}

func TestTransportPublishesUsageAfterRequestContextCanceled(t *testing.T) {
	body := strings.Join([]string{
		"event: response.completed",
		`data: {"type":"response.completed","response":{"usage":{"total_tokens":9}}}`,
		"",
	}, "\n")

	sink := &contextMemorySink{}
	transport := NewTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	}), sink, Options{Mode: ModeInclude, Fields: []string{"usage"}})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.example.com/v1/responses", nil)
	if err != nil {
		t.Fatalf("new request failed: %v", err)
	}
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("round trip failed: %v", err)
	}
	_, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	if len(sink.events) != 1 {
		t.Fatalf("expected one usage event, got %d", len(sink.events))
	}
	if sink.ctxCanceled[0] {
		t.Fatal("usage event was published with canceled context")
	}
}

func TestTransportExtractsUsageWithExcludeFields(t *testing.T) {
	body := strings.Join([]string{
		"event: response.completed",
		`data: {"type":"response.completed","response":{"id":"resp_1","instructions":"large prompt","tools":[{"type":"function"}],"output":[{"big":"kept"}],"usage":{"input_tokens":4,"output_tokens":5,"total_tokens":9}}}`,
		"",
	}, "\n")

	sink := &memorySink{}
	transport := NewTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	}), sink, Options{
		Mode:   ModeExclude,
		Fields: []string{"instructions", "tools"},
	})

	req, err := http.NewRequest(http.MethodPost, "https://api.example.com/v1/responses", nil)
	if err != nil {
		t.Fatalf("new request failed: %v", err)
	}
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("round trip failed: %v", err)
	}
	_, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	if len(sink.events) != 1 {
		t.Fatalf("expected one usage event, got %d", len(sink.events))
	}
	data := usageDataFromEvent(t, sink.events[0])
	if _, ok := data["instructions"]; ok {
		t.Fatalf("did not expect instructions: %#v", data)
	}
	if _, ok := data["tools"]; ok {
		t.Fatalf("did not expect tools: %#v", data)
	}
	if string(data["output"]) != `[{"big":"kept"}]` {
		t.Fatalf("expected output to be kept: %#v", data)
	}
	if string(data["usage"]) == "" {
		t.Fatalf("expected usage: %#v", data)
	}
}

func usageDataFromEvent(t *testing.T, event eventbus.Event) Data {
	t.Helper()
	data, ok := event.Data.(Data)
	if !ok {
		t.Fatalf("expected usage data, got %T", event.Data)
	}
	return data
}

func TestExtractCompletedUsageSkipsLargeFields(t *testing.T) {
	large := strings.Repeat("x", 128*1024)
	raw := `{"type":"response.completed","response":{"output":[{"text":"` + large + `"}],"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}}`

	result, ok, err := extractCompletedUsage(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}
	if !ok {
		t.Fatal("expected usage")
	}
	var usage map[string]any
	if err := json.Unmarshal(result.Fields["usage"], &usage); err != nil {
		t.Fatalf("unmarshal usage failed: %v", err)
	}
	if usage["total_tokens"] != float64(3) {
		t.Fatalf("unexpected usage: %#v", result.Fields["usage"])
	}
}
