package capture

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/eventbus"
	"github.com/lwmacct/260628-llm-relay-dproxy/internal/proxyplan"
)

type recordingSink struct {
	events []Event
}

func (s *recordingSink) Publish(_ context.Context, event Event) error {
	s.events = append(s.events, event)
	return nil
}

func (s *recordingSink) Close(context.Context) error {
	return nil
}

type contextRecordingSink struct {
	events      []Event
	ctxCanceled []bool
}

func (s *contextRecordingSink) Publish(ctx context.Context, event Event) error {
	s.events = append(s.events, event)
	s.ctxCanceled = append(s.ctxCanceled, ctx != nil && ctx.Err() != nil)
	return nil
}

func (s *contextRecordingSink) Close(context.Context) error {
	return nil
}

type fixedIDGenerator struct {
	id string
}

func (g fixedIDGenerator) Generate() string {
	return g.id
}

type captureTransport struct {
	request *http.Request
}

func (t *captureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.request = req
	return &http.Response{
		StatusCode: http.StatusNoContent,
		Body:       io.NopCloser(strings.NewReader("")),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

type responseTransport struct {
	response *http.Response
	request  *http.Request
}

func (t *responseTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.request = req
	return t.response, nil
}

type errorTransport struct {
	err     error
	request *http.Request
}

func (t *errorTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.request = req
	return nil, t.err
}

type blockingBodyTransport struct {
	firstChunk chan []byte
	release    chan struct{}
	request    *http.Request
}

func newBlockingBodyTransport() *blockingBodyTransport {
	return &blockingBodyTransport{
		firstChunk: make(chan []byte, 1),
		release:    make(chan struct{}),
	}
}

func (t *blockingBodyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.request = req
	reader, writer := io.Pipe()
	go func() {
		_, _ = writer.Write([]byte("first"))
		t.firstChunk <- []byte("first")
		<-t.release
		_, _ = writer.Write([]byte("-second"))
		_ = writer.Close()
	}()
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       reader,
		Header:     http.Header{"Content-Type": {"text/plain"}},
		Request:    req,
	}, nil
}

func TestTransportAttachesLabelsFromDirectiveContext(t *testing.T) {
	base := &captureTransport{}
	sink := &recordingSink{}
	transport := NewTransport(base, sink, Options{})

	req, err := http.NewRequest(http.MethodPost, "https://api.example.com/v1/chat", io.NopCloser(strings.NewReader(`{"ok":true}`)))
	if err != nil {
		t.Fatalf("new request failed: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(proxyplan.ContextWithPlan(req.Context(), &proxyplan.Plan{
		Labels: map[string]any{
			"trace_id": "trace-123",
			"hop_note": "edge-a",
		},
		Capture: proxyplan.CapturePolicy{
			Configured:      true,
			RequestHeaders:  true,
			ResponseHeaders: true,
		},
	}))

	if _, err := transport.RoundTrip(req); err != nil {
		t.Fatalf("round trip failed: %v", err)
	}

	if base.request == nil {
		t.Fatal("expected outbound request")
	}
	if len(sink.events) != 2 {
		t.Fatalf("expected request/response events, got %d", len(sink.events))
	}

	if sink.events[0].Type != EventTypeRequest {
		t.Fatalf("unexpected first event type: %s", sink.events[0].Type)
	}
	if sink.events[1].Type != EventTypeResponse {
		t.Fatalf("unexpected second event type: %s", sink.events[1].Type)
	}
	requestData := requestDataFromEvent(t, sink.events[0])
	if got := requestData.Headers.Get("Labels-Trace-Id"); got != "" {
		t.Fatalf("did not expect synthetic labels header in request log, got %q", got)
	}
	if sink.events[0].RequestID != sink.events[1].RequestID {
		t.Fatalf("expected shared request id, got request=%q response=%q", sink.events[0].RequestID, sink.events[1].RequestID)
	}
	if sink.events[0].EventID == sink.events[1].EventID {
		t.Fatalf("expected distinct event ids, got %q", sink.events[0].EventID)
	}
	for _, event := range sink.events {
		if got := event.Labels["trace_id"]; got != "trace-123" {
			t.Fatalf("unexpected labels trace_id on %s: %#v", event.Type, got)
		}
	}
}

func TestTransportIgnoresClientRequestIDHeaderForRequestID(t *testing.T) {
	base := &captureTransport{}
	sink := &recordingSink{}
	transport := NewTransport(base, sink, Options{
		IDGenerator: fixedIDGenerator{id: "generated-id"},
	})

	req := httptestRequest(t, http.MethodGet, "https://api.example.com/v1/chat", "")
	req.Header.Set(proxyplan.ClientRequestIDHeader, "client-req-1")
	req = req.WithContext(proxyplan.ContextWithPlan(req.Context(), &proxyplan.Plan{
		Capture: proxyplan.CapturePolicy{
			Configured:     true,
			RequestHeaders: true,
		},
	}))

	if _, err := transport.RoundTrip(req); err != nil {
		t.Fatalf("round trip failed: %v", err)
	}

	if len(sink.events) != 1 {
		t.Fatalf("expected request event, got %d", len(sink.events))
	}
	for _, event := range sink.events {
		if event.RequestID != "generated-id" {
			t.Fatalf("expected generated request id, got %q", event.RequestID)
		}
	}
}

func TestTransportUsesRequestIDContext(t *testing.T) {
	base := &captureTransport{}
	sink := &recordingSink{}
	transport := NewTransport(base, sink, Options{
		IDGenerator: fixedIDGenerator{id: "generated-id"},
	})

	req := httptestRequest(t, http.MethodGet, "https://api.example.com/v1/chat", "")
	ctx := eventbus.ContextWithRequestID(req.Context(), "ctx-req-1")
	ctx = proxyplan.ContextWithPlan(ctx, &proxyplan.Plan{
		Capture: proxyplan.CapturePolicy{
			Configured:     true,
			RequestHeaders: true,
		},
	})
	req = req.WithContext(ctx)

	if _, err := transport.RoundTrip(req); err != nil {
		t.Fatalf("round trip failed: %v", err)
	}

	for _, event := range sink.events {
		if event.RequestID != "ctx-req-1" {
			t.Fatalf("expected context request id, got %q", event.RequestID)
		}
	}
}

func TestTransportFallsBackToGeneratedEventID(t *testing.T) {
	base := &captureTransport{}
	sink := &recordingSink{}
	transport := NewTransport(base, sink, Options{
		IDGenerator: fixedIDGenerator{id: "generated-id"},
	})

	req := httptestRequest(t, http.MethodGet, "https://api.example.com/v1/chat", "")
	req = req.WithContext(proxyplan.ContextWithPlan(req.Context(), &proxyplan.Plan{
		Capture: proxyplan.CapturePolicy{
			Configured:     true,
			RequestHeaders: true,
		},
	}))

	if _, err := transport.RoundTrip(req); err != nil {
		t.Fatalf("round trip failed: %v", err)
	}

	for _, event := range sink.events {
		if event.RequestID != "generated-id" {
			t.Fatalf("expected generated request id, got %q", event.RequestID)
		}
		if event.EventID != "generated-id" {
			t.Fatalf("expected generated event id, got %q", event.EventID)
		}
	}
}

func TestTransportCapturesHeadersWhenBodyIsEnabled(t *testing.T) {
	base := &responseTransport{
		response: &http.Response{
			StatusCode:    http.StatusCreated,
			Body:          io.NopCloser(strings.NewReader("pong")),
			Header:        http.Header{"X-Upstream": {"ok"}, "Content-Type": {"text/plain"}},
			ContentLength: 4,
			Request:       httptestRequest(t, http.MethodPost, "https://api.example.com/v1/chat", ""),
		},
	}
	sink := &recordingSink{}
	transport := NewTransport(base, sink, Options{})

	req := httptestRequest(t, http.MethodPost, "https://api.example.com/v1/chat", "ping")
	req.Header.Set("X-Request", "present")
	req = req.WithContext(proxyplan.ContextWithPlan(req.Context(), &proxyplan.Plan{
		Capture: proxyplan.CapturePolicy{
			Configured:      true,
			RequestHeaders:  false,
			ResponseHeaders: false,
			RequestBody:     true,
			ResponseBody:    true,
		},
	}))

	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("round trip failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "pong" {
		t.Fatalf("unexpected response body: %q", string(body))
	}
	upstreamBody, _ := io.ReadAll(base.request.Body)
	if string(upstreamBody) != "ping" {
		t.Fatalf("unexpected upstream body: %q", string(upstreamBody))
	}
	if len(sink.events) != 4 {
		t.Fatalf("expected request/request_body/response/response_body events, got %d", len(sink.events))
	}
	requestEvent := requestDataFromEvent(t, sink.events[0])
	if got := requestEvent.Headers.Get("X-Request"); got != "present" {
		t.Fatalf("expected request headers to be recorded when request body is enabled, got %q", got)
	}
	requestBodyEvent := requestBodyDataFromEvent(t, sink.events[1])
	if requestBodyEvent.Body == nil || requestBodyEvent.Body.Content != "ping" || requestBodyEvent.Body.Encoding != BodyEncodingText {
		t.Fatalf("unexpected request body: %#v", requestBodyEvent.Body)
	}
	responseEvent := responseDataFromEvent(t, sink.events[2])
	if got := responseEvent.Headers.Get("X-Upstream"); got != "ok" {
		t.Fatalf("expected response headers to be recorded when response body is enabled, got %q", got)
	}
	responseBodyEvent := responseBodyDataFromEvent(t, sink.events[3])
	if !responseBodyEvent.Complete {
		t.Fatalf("expected complete response body event: %#v", responseBodyEvent)
	}
	if responseBodyEvent.Body == nil || responseBodyEvent.Body.Content != "pong" || responseBodyEvent.Body.Encoding != BodyEncodingText {
		t.Fatalf("unexpected response body: %#v", responseBodyEvent.Body)
	}
}

func TestTransportPublishesCaptureEventsAfterRequestContextCanceled(t *testing.T) {
	base := &responseTransport{
		response: &http.Response{
			StatusCode:    http.StatusOK,
			Body:          io.NopCloser(strings.NewReader("pong")),
			Header:        http.Header{"Content-Type": {"text/plain"}},
			ContentLength: 4,
			Request:       httptestRequest(t, http.MethodPost, "https://api.example.com/v1/chat", ""),
		},
	}
	sink := &contextRecordingSink{}
	transport := NewTransport(base, sink, Options{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptestRequest(t, http.MethodPost, "https://api.example.com/v1/chat", "ping").WithContext(ctx)
	req = req.WithContext(proxyplan.ContextWithPlan(req.Context(), &proxyplan.Plan{
		Capture: proxyplan.CapturePolicy{
			Configured:     true,
			RequestHeaders: true,
			ResponseBody:   true,
		},
	}))

	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("round trip failed: %v", err)
	}
	_, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	if len(sink.events) != 3 {
		t.Fatalf("expected request/response/response_body events, got %d", len(sink.events))
	}
	for i, canceled := range sink.ctxCanceled {
		if canceled {
			t.Fatalf("event %d was published with canceled context", i)
		}
	}
}

func TestTransportPublishesResponseBodyAfterClientReadsBody(t *testing.T) {
	base := newBlockingBodyTransport()
	sink := &recordingSink{}
	transport := NewTransport(base, sink, Options{})

	req := httptestRequest(t, http.MethodGet, "https://api.example.com/v1/chat", "")
	req = req.WithContext(proxyplan.ContextWithPlan(req.Context(), &proxyplan.Plan{
		Capture: proxyplan.CapturePolicy{
			Configured:     true,
			ResponseBody:   true,
			RequestBody:    false,
			RequestHeaders: false,
		},
	}))

	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("round trip failed: %v", err)
	}
	if len(sink.events) != 1 {
		t.Fatalf("expected response header event before body is read, got %d", len(sink.events))
	}
	if sink.events[0].Type != EventTypeResponse {
		t.Fatalf("expected response event first, got %s", sink.events[0].Type)
	}

	buf := make([]byte, 5)
	n, err := io.ReadFull(resp.Body, buf)
	if err != nil {
		t.Fatalf("read first chunk failed after %d bytes: %v", n, err)
	}
	if string(buf) != "first" {
		t.Fatalf("unexpected first chunk: %q", string(buf))
	}
	if len(sink.events) != 1 {
		t.Fatalf("did not expect response body event before EOF, got %d events", len(sink.events))
	}

	close(base.release)
	rest, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read rest failed: %v", err)
	}
	if string(rest) != "-second" {
		t.Fatalf("unexpected rest body: %q", string(rest))
	}
	if len(sink.events) != 2 {
		t.Fatalf("expected response body event after EOF, got %d events", len(sink.events))
	}
	bodyEvent := responseBodyDataFromEvent(t, sink.events[1])
	if !bodyEvent.Complete {
		t.Fatalf("expected complete body event: %#v", bodyEvent)
	}
	if bodyEvent.Body == nil || bodyEvent.Body.Content != "first-second" {
		t.Fatalf("unexpected body event: %#v", bodyEvent)
	}
}

func TestTransportPublishesIncompleteResponseBodyOnEarlyClose(t *testing.T) {
	base := newBlockingBodyTransport()
	sink := &recordingSink{}
	transport := NewTransport(base, sink, Options{})

	req := httptestRequest(t, http.MethodGet, "https://api.example.com/v1/chat", "")
	req = req.WithContext(proxyplan.ContextWithPlan(req.Context(), &proxyplan.Plan{
		Capture: proxyplan.CapturePolicy{
			Configured:   true,
			ResponseBody: true,
		},
	}))

	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("round trip failed: %v", err)
	}
	buf := make([]byte, 5)
	if _, err := io.ReadFull(resp.Body, buf); err != nil {
		t.Fatalf("read first chunk failed: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("close body failed: %v", err)
	}
	close(base.release)

	if len(sink.events) != 2 {
		t.Fatalf("expected response and incomplete response body events, got %d", len(sink.events))
	}
	bodyEvent := responseBodyDataFromEvent(t, sink.events[1])
	if bodyEvent.Complete {
		t.Fatalf("expected incomplete body event: %#v", bodyEvent)
	}
	if bodyEvent.Body == nil || bodyEvent.Body.Content != "first" {
		t.Fatalf("unexpected incomplete body event: %#v", bodyEvent)
	}
}

func TestTransportSkipsAllEventsWhenCapturePolicyAbsent(t *testing.T) {
	base := &responseTransport{
		response: &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("pong")),
			Header:     http.Header{"X-Upstream": {"ok"}},
			Request:    httptestRequest(t, http.MethodPost, "https://api.example.com/v1/chat", ""),
		},
	}
	sink := &recordingSink{}
	transport := NewTransport(base, sink, Options{})

	req := httptestRequest(t, http.MethodPost, "https://api.example.com/v1/chat", "ping")
	req = req.WithContext(proxyplan.ContextWithPlan(req.Context(), &proxyplan.Plan{
		Labels: map[string]any{
			"trace_id": "trace-123",
		},
	}))

	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("round trip failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "pong" {
		t.Fatalf("unexpected response body: %q", string(body))
	}
	if len(sink.events) != 0 {
		t.Fatalf("expected no capture events, got %#v", sink.events)
	}
}

func TestTransportCaptureDefaultsPublishNoEvents(t *testing.T) {
	base := &responseTransport{
		response: &http.Response{
			StatusCode:    http.StatusOK,
			Body:          io.NopCloser(strings.NewReader("pong")),
			Header:        http.Header{"X-Upstream": {"ok"}},
			ContentLength: 4,
			Request:       httptestRequest(t, http.MethodPost, "https://api.example.com/v1/chat", ""),
		},
	}
	sink := &recordingSink{}
	transport := NewTransport(base, sink, Options{})

	req := httptestRequest(t, http.MethodPost, "https://api.example.com/v1/chat", "ping")
	req.Header.Set("X-Request", "present")
	req = req.WithContext(proxyplan.ContextWithPlan(req.Context(), &proxyplan.Plan{
		Capture: proxyplan.DefaultCapturePolicy(),
	}))

	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("round trip failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "pong" {
		t.Fatalf("unexpected response body: %q", string(body))
	}
	if len(sink.events) != 0 {
		t.Fatalf("expected no capture events, got %d", len(sink.events))
	}
}

func TestTransportAbnormalCapturePublishesEventsForErrorStatusWithoutDirectiveCapture(t *testing.T) {
	base := &responseTransport{
		response: &http.Response{
			StatusCode:    http.StatusInternalServerError,
			Body:          io.NopCloser(strings.NewReader(`{"error":"failed"}`)),
			Header:        http.Header{"Content-Type": {"application/json"}, "X-Upstream": {"failed"}},
			ContentLength: int64(len(`{"error":"failed"}`)),
			Request:       httptestRequest(t, http.MethodPost, "https://api.example.com/v1/chat", ""),
		},
	}
	sink := &recordingSink{}
	transport := NewTransport(base, sink, Options{AbnormalCapture: true})

	req := httptestRequest(t, http.MethodPost, "https://api.example.com/v1/chat", `{"prompt":"hi"}`)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer raw-token")
	req = req.WithContext(proxyplan.ContextWithPlan(req.Context(), &proxyplan.Plan{}))

	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("round trip failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != `{"error":"failed"}` {
		t.Fatalf("unexpected response body: %q", string(body))
	}
	upstreamBody, _ := io.ReadAll(base.request.Body)
	if string(upstreamBody) != `{"prompt":"hi"}` {
		t.Fatalf("unexpected upstream body: %q", string(upstreamBody))
	}
	if len(sink.events) != 4 {
		t.Fatalf("expected abnormal request/request_body/response/response_body events, got %d", len(sink.events))
	}

	requestEvent := requestDataFromEvent(t, sink.events[0])
	if requestEvent.CaptureReason != captureReasonAbnormal || requestEvent.AbnormalType != abnormalTypeUpstreamStatus {
		t.Fatalf("unexpected request metadata: %#v", requestEvent)
	}
	if got := requestEvent.Headers.Get("Authorization"); got != "Bearer raw-token" {
		t.Fatalf("expected raw authorization header, got %q", got)
	}
	requestBodyEvent := requestBodyDataFromEvent(t, sink.events[1])
	if requestBodyEvent.Body == nil || requestBodyEvent.Body.Encoding != BodyEncodingJSON {
		t.Fatalf("unexpected request body event: %#v", requestBodyEvent)
	}
	responseEvent := responseDataFromEvent(t, sink.events[2])
	if responseEvent.StatusCode != http.StatusInternalServerError ||
		responseEvent.CaptureReason != captureReasonAbnormal ||
		responseEvent.AbnormalType != abnormalTypeUpstreamStatus ||
		responseEvent.Headers.Get("X-Upstream") != "failed" {
		t.Fatalf("unexpected response event: %#v", responseEvent)
	}
	responseBodyEvent := responseBodyDataFromEvent(t, sink.events[3])
	if !responseBodyEvent.Complete || responseBodyEvent.CaptureReason != captureReasonAbnormal {
		t.Fatalf("unexpected response body event: %#v", responseBodyEvent)
	}
}

func TestTransportAbnormalCapturePublishesEventsForRoundTripError(t *testing.T) {
	base := &errorTransport{err: errors.New("dial upstream failed")}
	sink := &recordingSink{}
	transport := NewTransport(base, sink, Options{AbnormalCapture: true})

	req := httptestRequest(t, http.MethodPost, "https://api.example.com/v1/chat", "ping")
	req.Header.Set("Content-Type", "text/plain")
	req = req.WithContext(proxyplan.ContextWithPlan(req.Context(), &proxyplan.Plan{}))

	_, err := transport.RoundTrip(req)
	if !errors.Is(err, base.err) {
		t.Fatalf("expected upstream error, got %v", err)
	}
	upstreamBody, _ := io.ReadAll(base.request.Body)
	if string(upstreamBody) != "ping" {
		t.Fatalf("unexpected upstream body: %q", string(upstreamBody))
	}
	if len(sink.events) != 3 {
		t.Fatalf("expected abnormal request/request_body/response events, got %d", len(sink.events))
	}
	responseEvent := responseDataFromEvent(t, sink.events[2])
	if responseEvent.StatusCode != 0 ||
		responseEvent.CaptureReason != captureReasonAbnormal ||
		responseEvent.AbnormalType != abnormalTypeUpstreamError ||
		responseEvent.Error != "dial upstream failed" {
		t.Fatalf("unexpected response event: %#v", responseEvent)
	}
}

func TestTransportAbnormalCaptureSkipsSuccessfulResponseWithoutDirectiveCapture(t *testing.T) {
	base := &responseTransport{
		response: &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("pong")),
			Header:     http.Header{"Content-Type": {"text/plain"}},
			Request:    httptestRequest(t, http.MethodPost, "https://api.example.com/v1/chat", ""),
		},
	}
	sink := &recordingSink{}
	transport := NewTransport(base, sink, Options{AbnormalCapture: true})

	req := httptestRequest(t, http.MethodPost, "https://api.example.com/v1/chat", "ping")
	req = req.WithContext(proxyplan.ContextWithPlan(req.Context(), &proxyplan.Plan{}))

	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("round trip failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "pong" {
		t.Fatalf("unexpected response body: %q", string(body))
	}
	if len(sink.events) != 0 {
		t.Fatalf("expected no events for successful response, got %#v", sink.events)
	}
}

func TestTransportCapturesFilteredSSEEventsFromPolicy(t *testing.T) {
	base := &responseTransport{
		response: &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(strings.NewReader(
				"event: ignored\ndata: a\n\n" +
					"event: wanted\ndata: b\n\n",
			)),
			Header:  http.Header{"Content-Type": {"text/event-stream"}},
			Request: httptestRequest(t, http.MethodGet, "https://api.example.com/v1/stream", ""),
		},
	}
	sink := &recordingSink{}
	transport := NewTransport(base, sink, Options{})

	req := httptestRequest(t, http.MethodGet, "https://api.example.com/v1/stream", "")
	req = req.WithContext(proxyplan.ContextWithPlan(req.Context(), &proxyplan.Plan{
		Capture: proxyplan.CapturePolicy{
			Configured:       true,
			StreamEvents:     true,
			StreamEventTypes: []string{"wanted"},
		},
	}))

	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("round trip failed: %v", err)
	}
	_, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	var streamEvents []StreamEventData
	var endEvent *StreamEndData
	for _, event := range sink.events {
		switch event.Type {
		case EventTypeStreamEvent:
			streamEvents = append(streamEvents, streamEventDataFromEvent(t, event))
		case EventTypeStreamEnd:
			data := streamEndDataFromEvent(t, event)
			endEvent = &data
		}
	}
	if len(streamEvents) != 1 {
		t.Fatalf("expected one filtered stream event, got %d", len(streamEvents))
	}
	if streamEvents[0].EventName != "wanted" {
		t.Fatalf("unexpected stream event: %#v", streamEvents[0])
	}
	if endEvent == nil || endEvent.EventRecords != 1 {
		t.Fatalf("unexpected stream end event: %#v", endEvent)
	}
}

func TestTransportCapturesAllSSEEventsWhenEventTypesEmpty(t *testing.T) {
	tests := []struct {
		name       string
		eventTypes []string
	}{
		{name: "nil"},
		{name: "empty", eventTypes: []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := &responseTransport{
				response: &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(strings.NewReader(
						"event: one\ndata: {\"value\":1}\n\n" +
							"event: two\ndata: b\n\n",
					)),
					Header:  http.Header{"Content-Type": {"text/event-stream"}},
					Request: httptestRequest(t, http.MethodGet, "https://api.example.com/v1/stream", ""),
				},
			}
			sink := &recordingSink{}
			transport := NewTransport(base, sink, Options{})

			req := httptestRequest(t, http.MethodGet, "https://api.example.com/v1/stream", "")
			req = req.WithContext(proxyplan.ContextWithPlan(req.Context(), &proxyplan.Plan{
				Capture: proxyplan.CapturePolicy{
					Configured:       true,
					StreamEvents:     true,
					StreamEventTypes: tt.eventTypes,
				},
			}))

			resp, err := transport.RoundTrip(req)
			if err != nil {
				t.Fatalf("round trip failed: %v", err)
			}
			_, _ = io.ReadAll(resp.Body)
			_ = resp.Body.Close()

			var streamEvents []StreamEventData
			for _, event := range sink.events {
				if event.Type == EventTypeStreamEvent {
					streamEvents = append(streamEvents, streamEventDataFromEvent(t, event))
				}
			}
			if len(streamEvents) != 2 {
				t.Fatalf("expected all stream events, got %d", len(streamEvents))
			}
			if data, ok := streamEvents[0].Payload.(map[string]any); !ok || data["value"] != float64(1) {
				t.Fatalf("expected JSON object stream data, got %#v", streamEvents[0].Payload)
			}
			if streamEvents[1].Payload != "b" {
				t.Fatalf("expected text stream data, got %#v", streamEvents[1].Payload)
			}
		})
	}
}

func TestTransportDoesNotCaptureStreamEventsForNonSSEStream(t *testing.T) {
	base := &responseTransport{
		response: &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("{\"a\":1}\n")),
			Header:     http.Header{"Content-Type": {"application/stream+json"}},
			Request:    httptestRequest(t, http.MethodGet, "https://api.example.com/v1/stream", ""),
		},
	}
	sink := &recordingSink{}
	transport := NewTransport(base, sink, Options{})

	req := httptestRequest(t, http.MethodGet, "https://api.example.com/v1/stream", "")
	req = req.WithContext(proxyplan.ContextWithPlan(req.Context(), &proxyplan.Plan{
		Capture: proxyplan.CapturePolicy{
			Configured:   true,
			StreamEvents: true,
		},
	}))

	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("round trip failed: %v", err)
	}
	_, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	var streamEvent *Event
	var endEvent *Event
	for _, event := range sink.events {
		switch event.Type {
		case EventTypeStreamEvent:
			streamEvent = &event
		case EventTypeStreamEnd:
			endEvent = &event
		}
	}
	if streamEvent != nil || endEvent != nil {
		t.Fatalf("did not expect stream detail events for non-SSE stream: event=%#v end=%#v", streamEvent, endEvent)
	}
}

func requestDataFromEvent(t *testing.T, event Event) RequestData {
	t.Helper()
	data, ok := event.Data.(RequestData)
	if !ok {
		t.Fatalf("expected request data, got %T", event.Data)
	}
	return data
}

func responseDataFromEvent(t *testing.T, event Event) ResponseData {
	t.Helper()
	data, ok := event.Data.(ResponseData)
	if !ok {
		t.Fatalf("expected response data, got %T", event.Data)
	}
	return data
}

func responseBodyDataFromEvent(t *testing.T, event Event) ResponseBodyData {
	t.Helper()
	data, ok := event.Data.(ResponseBodyData)
	if !ok {
		t.Fatalf("expected response body data, got %T", event.Data)
	}
	return data
}

func requestBodyDataFromEvent(t *testing.T, event Event) RequestBodyData {
	t.Helper()
	data, ok := event.Data.(RequestBodyData)
	if !ok {
		t.Fatalf("expected request body data, got %T", event.Data)
	}
	return data
}

func streamEventDataFromEvent(t *testing.T, event Event) StreamEventData {
	t.Helper()
	data, ok := event.Data.(StreamEventData)
	if !ok {
		t.Fatalf("expected stream event data, got %T", event.Data)
	}
	return data
}

func streamEndDataFromEvent(t *testing.T, event Event) StreamEndData {
	t.Helper()
	data, ok := event.Data.(StreamEndData)
	if !ok {
		t.Fatalf("expected stream end data, got %T", event.Data)
	}
	return data
}

func httptestRequest(t *testing.T, method, target, body string) *http.Request {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, target, reader)
	if err != nil {
		t.Fatalf("new request failed: %v", err)
	}
	return req
}
