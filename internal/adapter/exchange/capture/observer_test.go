package capture

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/core/exchange"
)

type collectorStub struct {
	capture exchange.Capture
	record  exchange.Record
}

func (c *collectorStub) Begin() (exchange.Capture, bool) {
	return c.capture, true
}

func (c *collectorStub) Complete(_ context.Context, record exchange.Record) error {
	c.record = record
	return nil
}

func TestObserverCapturesAndRedactsHTTPExchange(t *testing.T) {
	collector := &collectorStub{capture: exchange.Capture{ID: 7, MaxBodyBytes: 1024}}
	observer := NewObserver(collector)
	req := httptest.NewRequest(http.MethodPost, "http://proxy.local/v1/chat", strings.NewReader("hello"))
	req.Header.Set("Authorization", "Bearer secret")
	observation := observer.Start(req)
	wrappedRequest := observation.WrapRequest(req)
	if _, err := io.ReadAll(wrappedRequest.Body); err != nil {
		t.Fatalf("read request failed: %v", err)
	}
	response := httptest.NewRecorder()
	wrappedResponse := observation.WrapResponseWriter(response)
	wrappedResponse.Header().Set("Set-Cookie", "secret=value")
	wrappedResponse.WriteHeader(http.StatusCreated)
	_, _ = wrappedResponse.Write([]byte("world"))
	target, _ := url.Parse("https://api.example.test/v1/chat")
	observation.SetTargetURL(target)
	observation.SetDirective("remote", "redis", "redis://redis.example.com/0", "team-a/openai", 7)
	outboundRequest := httptest.NewRequest(http.MethodPost, target.String(), nil)
	outboundRequest.Header.Set("Authorization", "Bearer upstream-secret")
	outboundRequest.Header.Set("X-Outbound", "visible")
	observation.SetOutboundRequest(outboundRequest)
	observation.Finish()

	record := collector.record
	if record.ID != 7 || record.StatusCode != http.StatusCreated || record.TargetURL != target.String() {
		t.Fatalf("unexpected record metadata: %#v", record)
	}
	if record.DirectiveMode != "remote" || record.DirectiveBackend != "redis" ||
		record.DirectiveEndpoint != "redis://redis.example.com/0" || record.DirectiveKey != "team-a/openai" ||
		record.DirectiveResolutionMillis != 7 {
		t.Fatalf("unexpected directive metadata: %#v", record)
	}
	if record.RequestBody.Text != "hello" || record.ResponseBody.Text != "world" {
		t.Fatalf("unexpected captured bodies: %#v %#v", record.RequestBody, record.ResponseBody)
	}
	if record.RequestHeaders["Authorization"][0] != "<redacted>" || record.ResponseHeaders["Set-Cookie"][0] != "<redacted>" {
		t.Fatalf("sensitive headers were not redacted: %#v %#v", record.RequestHeaders, record.ResponseHeaders)
	}
	if record.OutboundRequestHeaders["Authorization"][0] != "<redacted>" || record.OutboundRequestHeaders["X-Outbound"][0] != "visible" {
		t.Fatalf("unexpected outbound headers: %#v", record.OutboundRequestHeaders)
	}
}
