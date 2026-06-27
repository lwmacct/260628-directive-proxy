package capture

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"time"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/eventbus"
	"github.com/lwmacct/260628-llm-relay-dproxy/internal/proxyplan"
)

const (
	AbnormalTypeInvalidDirective = "invalid_directive"
	AbnormalTypeResolverError    = "resolver_error"
)

type LocalAbnormalExchange struct {
	Request             *http.Request
	ResponseStatusCode  int
	ResponseHeaders     http.Header
	ResponseBody        []byte
	ResponseContentType string
	AbnormalType        string
	Error               error
}

func PublishLocalAbnormalExchange(ctx context.Context, sink Sink, idGen eventbus.IDGenerator, exchange LocalAbnormalExchange) {
	if exchange.Request == nil {
		return
	}
	if sink == nil {
		sink = NopSink{}
	}
	if idGen == nil {
		idGen = eventbus.NewIDGenerator()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	req := exchange.Request
	requestCtx, requestID := eventbus.EnsureRequestID(req.Context(), idGen)
	if requestCtx != req.Context() {
		req = req.WithContext(requestCtx)
	}

	var requestBody []byte
	if req.Body != nil {
		requestBody, _ = io.ReadAll(req.Body)
		req.Body = io.NopCloser(bytes.NewReader(requestBody))
	}

	now := time.Now()
	labels := labelsFromRequest(req)
	runtime := runtimeFromRequest(req)
	metadata := abnormalMetadata(exchange.AbnormalType, exchange.Error)
	policy := proxyplan.CapturePolicy{
		Configured:       true,
		RequestHeaders:   true,
		RequestBody:      true,
		ResponseHeaders:  true,
		ResponseBody:     true,
		StreamEvents:     false,
		StreamEventTypes: nil,
	}.WithDefaults()
	transport := &Transport{sink: sink, idGen: idGen}

	_ = sink.Publish(ctx, transport.buildRequestEvent(req, requestID, now, labels, runtime, requestBody, policy, metadata))
	if len(requestBody) > 0 {
		_ = sink.Publish(ctx, transport.buildRequestBodyEvent(req, requestID, now, labels, runtime, requestBody, metadata))
	}

	headers := exchange.ResponseHeaders.Clone()
	if headers == nil {
		headers = make(http.Header)
	}
	contentType := exchange.ResponseContentType
	if contentType == "" {
		contentType = headers.Get("Content-Type")
	}
	resp := &http.Response{
		StatusCode:    exchange.ResponseStatusCode,
		Header:        headers,
		ContentLength: int64(len(exchange.ResponseBody)),
	}
	responseEvent := transport.buildResponseEvent(resp, requestID, now, labels, runtime, policy, metadata)
	_ = sink.Publish(ctx, responseEvent)
	if len(exchange.ResponseBody) > 0 {
		data := ResponseBodyData{
			ContentType: contentType,
			Body:        buildBody(exchange.ResponseBody, contentType),
			Size:        len(exchange.ResponseBody),
			Duration:    time.Since(now),
			Complete:    true,
		}
		applyResponseBodyMetadata(&data, metadata)
		_ = sink.Publish(ctx, eventbus.Event{
			EventID:   idGen.Generate(),
			RequestID: requestID,
			Type:      EventTypeResponseBody,
			Timestamp: time.Now(),
			Labels:    cloneLabelsMap(labels),
			Runtime:   eventbus.CloneRuntime(runtime),
			Data:      data,
		})
	}
}
