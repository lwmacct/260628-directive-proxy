package capture

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/eventbus"
	"github.com/lwmacct/260628-llm-relay-dproxy/internal/proxyplan"
)

type Transport struct {
	base                  http.RoundTripper
	sink                  Sink
	idGen                 eventbus.IDGenerator
	abnormalCapturePolicy proxyplan.CapturePolicy
}

const (
	captureReasonAbnormal      = "abnormal"
	abnormalTypeUpstreamError  = "upstream_error"
	abnormalTypeUpstreamStatus = "upstream_status"
)

type captureMetadata struct {
	reason       string
	abnormalType string
	err          error
}

func abnormalMetadata(typ string, err error) captureMetadata {
	return captureMetadata{
		reason:       captureReasonAbnormal,
		abnormalType: typ,
		err:          err,
	}
}

func (m captureMetadata) empty() bool {
	return m.reason == "" && m.abnormalType == "" && m.err == nil
}

func NewTransport(base http.RoundTripper, sink Sink, opts Options) *Transport {
	opts = opts.withDefaults()
	if base == nil {
		base = http.DefaultTransport
	}
	if sink == nil {
		sink = NopSink{}
	}
	return &Transport{
		base:                  base,
		sink:                  sink,
		idGen:                 opts.IDGenerator,
		abnormalCapturePolicy: opts.AbnormalCapturePolicy,
	}
}

func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()
	policy := requestCapturePolicy(req)
	hasExplicitCapture := policy.Configured && capturePolicyHasOutputs(policy)
	abnormalPolicy := t.abnormalCapturePolicy
	hasAbnormalCapture := abnormalPolicy.Configured && capturePolicyHasOutputs(abnormalPolicy)
	if !hasExplicitCapture && !hasAbnormalCapture {
		ctx, _ := eventbus.EnsureRequestID(req.Context(), t.idGen)
		if ctx != req.Context() {
			req = req.WithContext(ctx)
		}
		return t.base.RoundTrip(req)
	}

	ctx, requestID := eventbus.EnsureRequestID(req.Context(), t.idGen)
	if ctx != req.Context() {
		req = req.WithContext(ctx)
	}
	labels := labelsFromRequest(req)
	runtime := runtimeFromRequest(req)
	eventCtx := withoutCancel(req.Context())
	captureRequest := captureRequestEvent(policy)
	captureResponse := captureResponseEvent(policy)
	publishedRequest := false
	publishedRequestBody := false
	publishedResponse := false

	var requestBody []byte
	if ((captureRequest && policy.RequestBody) || (hasAbnormalCapture && abnormalPolicy.RequestBody)) && req.Body != nil {
		requestBody, _ = io.ReadAll(req.Body)
		req.Body = io.NopCloser(bytes.NewReader(requestBody))
	}

	if captureRequest {
		_ = t.sink.Publish(eventCtx, t.buildRequestEvent(req, requestID, start, labels, runtime, requestBody, policy, captureMetadata{}))
		publishedRequest = true
		if policy.RequestBody && len(requestBody) > 0 {
			_ = t.sink.Publish(eventCtx, t.buildRequestBodyEvent(req, requestID, start, labels, runtime, requestBody, captureMetadata{}))
			publishedRequestBody = true
		}
	}
	publishAbnormalRequest := func(metadata captureMetadata) {
		if !hasAbnormalCapture {
			return
		}
		if captureRequestEvent(abnormalPolicy) && !publishedRequest {
			_ = t.sink.Publish(eventCtx, t.buildRequestEvent(req, requestID, start, labels, runtime, requestBody, abnormalPolicy, metadata))
			publishedRequest = true
		}
		if abnormalPolicy.RequestBody && len(requestBody) > 0 && !publishedRequestBody {
			_ = t.sink.Publish(eventCtx, t.buildRequestBodyEvent(req, requestID, start, labels, runtime, requestBody, metadata))
			publishedRequestBody = true
		}
	}
	publishResponse := func(policy proxyplan.CapturePolicy, data ResponseData, metadata captureMetadata) {
		applyResponseMetadata(&data, metadata)
		_ = t.sink.Publish(eventCtx, t.responseEvent(requestID, time.Now(), labels, runtime, data))
		publishedResponse = true
	}

	resp, err := t.base.RoundTrip(req)
	if err != nil {
		metadata := abnormalMetadata(abnormalTypeUpstreamError, err)
		if captureResponse {
			publishResponse(policy, ResponseData{
				StatusCode: 0,
				Duration:   time.Since(start),
			}, metadata)
		}
		publishAbnormalRequest(metadata)
		if hasAbnormalCapture && captureResponseEvent(abnormalPolicy) && !publishedResponse {
			publishResponse(abnormalPolicy, ResponseData{
				StatusCode: 0,
				Duration:   time.Since(start),
			}, metadata)
		}
		return nil, err
	}

	metadata := captureMetadata{}
	if isAbnormalStatus(resp.StatusCode) {
		metadata = abnormalMetadata(abnormalTypeUpstreamStatus, nil)
	}
	hasAbnormalResponse := hasAbnormalCapture && !metadata.empty()
	if isUpgradeResponse(resp) {
		if captureResponse {
			data := ResponseData{
				StatusCode: resp.StatusCode,
				Duration:   time.Since(start),
				IsUpgrade:  true,
				Size:       countResponseHeaderBytes(resp, -1),
			}
			if policy.ResponseHeaders {
				data.Headers = resp.Header.Clone()
			}
			publishResponse(policy, data, metadata)
		}
		if hasAbnormalResponse {
			publishAbnormalRequest(metadata)
			if captureResponseEvent(abnormalPolicy) && !publishedResponse {
				data := ResponseData{
					StatusCode: resp.StatusCode,
					Duration:   time.Since(start),
					IsUpgrade:  true,
					Size:       countResponseHeaderBytes(resp, -1),
				}
				if abnormalPolicy.ResponseHeaders {
					data.Headers = resp.Header.Clone()
				}
				publishResponse(abnormalPolicy, data, metadata)
			}
		}
		return resp, nil
	}

	if isStreamingResponse(req, resp) {
		streamContentType := responseStreamContentType(req, resp)
		streamPolicy := resolveStreamPolicy(policy, streamContentType)
		if captureResponse {
			data := ResponseData{
				StatusCode: resp.StatusCode,
				Duration:   time.Since(start),
				IsStream:   true,
				Size:       countResponseHeaderBytes(resp, -1),
			}
			if policy.ResponseHeaders {
				data.Headers = resp.Header.Clone()
			}
			publishResponse(policy, data, metadata)
		}
		if hasAbnormalResponse {
			publishAbnormalRequest(metadata)
			if captureResponseEvent(abnormalPolicy) && !publishedResponse {
				data := ResponseData{
					StatusCode: resp.StatusCode,
					Duration:   time.Since(start),
					IsStream:   true,
					Size:       countResponseHeaderBytes(resp, -1),
				}
				if abnormalPolicy.ResponseHeaders {
					data.Headers = resp.Header.Clone()
				}
				publishResponse(abnormalPolicy, data, metadata)
			}
			if abnormalPolicy.ResponseBody && resp.Body != nil {
				resp.Body = newResponseBodyRecorder(
					resp.Body,
					eventCtx,
					t.sink,
					t.idGen,
					requestID,
					labels,
					runtime,
					resp.Header.Get("Content-Type"),
					start,
					metadata,
				)
				return resp, nil
			}
		}
		if !streamPolicy.parseSSE {
			return resp, nil
		}
		resp.Body = newStreamRecorder(
			resp.Body,
			requestID,
			req.Method,
			req.URL.Path,
			req.URL.Host,
			streamContentType,
			streamPolicy,
			t.sink,
			t.idGen,
			labels,
			runtime,
			eventCtx,
		)
		return resp, nil
	}

	if captureResponse {
		_ = t.sink.Publish(eventCtx, t.buildResponseEvent(resp, requestID, start, labels, runtime, policy, metadata))
		publishedResponse = true
	}
	if hasAbnormalResponse {
		publishAbnormalRequest(metadata)
		if captureResponseEvent(abnormalPolicy) && !publishedResponse {
			_ = t.sink.Publish(eventCtx, t.buildResponseEvent(resp, requestID, start, labels, runtime, abnormalPolicy, metadata))
			publishedResponse = true
		}
	}
	if (policy.ResponseBody || (hasAbnormalResponse && abnormalPolicy.ResponseBody)) && resp.Body != nil {
		resp.Body = newResponseBodyRecorder(
			resp.Body,
			eventCtx,
			t.sink,
			t.idGen,
			requestID,
			labels,
			runtime,
			resp.Header.Get("Content-Type"),
			start,
			metadata,
		)
	}
	return resp, nil
}

func withoutCancel(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return context.WithoutCancel(ctx)
}

func capturePolicyHasOutputs(policy proxyplan.CapturePolicy) bool {
	return captureRequestEvent(policy) || captureResponseEvent(policy) || policy.StreamEvents
}

func captureRequestEvent(policy proxyplan.CapturePolicy) bool {
	return policy.RequestHeaders || policy.RequestBody
}

func captureResponseEvent(policy proxyplan.CapturePolicy) bool {
	return policy.ResponseHeaders || policy.ResponseBody
}

func isAbnormalStatus(statusCode int) bool {
	return statusCode >= http.StatusBadRequest
}

func applyRequestMetadata(data *RequestData, metadata captureMetadata) {
	if data == nil || metadata.empty() {
		return
	}
	data.CaptureReason = metadata.reason
	data.AbnormalType = metadata.abnormalType
}

func applyRequestBodyMetadata(data *RequestBodyData, metadata captureMetadata) {
	if data == nil || metadata.empty() {
		return
	}
	data.CaptureReason = metadata.reason
	data.AbnormalType = metadata.abnormalType
}

func applyResponseMetadata(data *ResponseData, metadata captureMetadata) {
	if data == nil || metadata.empty() {
		return
	}
	data.CaptureReason = metadata.reason
	data.AbnormalType = metadata.abnormalType
	if metadata.err != nil {
		data.Error = metadata.err.Error()
	}
}

func applyResponseBodyMetadata(data *ResponseBodyData, metadata captureMetadata) {
	if data == nil || metadata.empty() {
		return
	}
	data.CaptureReason = metadata.reason
	data.AbnormalType = metadata.abnormalType
	if metadata.err != nil && data.Error == "" {
		data.Error = metadata.err.Error()
	}
}

func (t *Transport) Close(ctx context.Context) error {
	return nil
}

func (t *Transport) buildRequestEvent(req *http.Request, requestID string, timestamp time.Time, labels map[string]any, runtime eventbus.Runtime, bodyBytes []byte, policy proxyplan.CapturePolicy, metadata captureMetadata) eventbus.Event {
	bodyLen := -1
	if len(bodyBytes) > 0 {
		bodyLen = len(bodyBytes)
	} else if req.ContentLength >= 0 {
		bodyLen = int(req.ContentLength)
	}
	data := RequestData{
		Method:      req.Method,
		URL:         req.URL.String(),
		RemoteAddr:  req.RemoteAddr,
		HTTPVersion: req.Proto,
		Size:        countRequestHeaderBytes(req, bodyLen),
	}
	if policy.RequestHeaders {
		data.Headers = req.Header.Clone()
	}
	applyRequestMetadata(&data, metadata)
	return t.newEvent(requestID, EventTypeRequest, timestamp, labels, runtime, data)
}

func (t *Transport) buildRequestBodyEvent(req *http.Request, requestID string, timestamp time.Time, labels map[string]any, runtime eventbus.Runtime, bodyBytes []byte, metadata captureMetadata) eventbus.Event {
	contentType := req.Header.Get("Content-Type")
	data := RequestBodyData{
		ContentType: contentType,
		Size:        len(bodyBytes),
	}
	if len(bodyBytes) > 0 {
		data.Body = buildBody(bodyBytes, contentType)
	}
	applyRequestBodyMetadata(&data, metadata)
	return t.newEvent(requestID, EventTypeRequestBody, timestamp, labels, runtime, data)
}

func (t *Transport) buildResponseEvent(resp *http.Response, requestID string, start time.Time, labels map[string]any, runtime eventbus.Runtime, policy proxyplan.CapturePolicy, metadata captureMetadata) eventbus.Event {
	data := ResponseData{
		StatusCode: resp.StatusCode,
		Duration:   time.Since(start),
		IsStream:   false,
		Size:       countResponseHeaderBytes(resp, -1),
	}
	if policy.ResponseHeaders {
		data.Headers = resp.Header.Clone()
	}
	applyResponseMetadata(&data, metadata)
	return t.responseEvent(requestID, time.Now(), labels, runtime, data)
}

func (t *Transport) responseEvent(requestID string, timestamp time.Time, labels map[string]any, runtime eventbus.Runtime, data ResponseData) eventbus.Event {
	return t.newEvent(requestID, EventTypeResponse, timestamp, labels, runtime, data)
}

func (t *Transport) newEvent(requestID string, typ eventbus.Type, timestamp time.Time, labels map[string]any, runtime eventbus.Runtime, data any) eventbus.Event {
	return eventbus.Event{
		EventID:   t.idGen.Generate(),
		RequestID: requestID,
		Type:      typ,
		Timestamp: timestamp,
		Labels:    cloneLabelsMap(labels),
		Runtime:   eventbus.CloneRuntime(runtime),
		Data:      data,
	}
}

func labelsFromRequest(req *http.Request) map[string]any {
	if req == nil {
		return nil
	}
	d, ok := proxyplan.PlanFromContext(req.Context())
	if !ok || len(d.Labels) == 0 {
		return nil
	}
	return cloneLabelsMap(d.Labels)
}

func runtimeFromRequest(req *http.Request) eventbus.Runtime {
	if req == nil {
		return eventbus.Runtime{}
	}
	d, ok := proxyplan.PlanFromContext(req.Context())
	if !ok {
		return eventbus.Runtime{}
	}
	return eventbus.CloneRuntime(d.Runtime)
}

func requestCapturePolicy(req *http.Request) proxyplan.CapturePolicy {
	if req == nil {
		return proxyplan.CapturePolicy{}
	}
	d, ok := proxyplan.PlanFromContext(req.Context())
	if !ok {
		return proxyplan.CapturePolicy{}
	}
	return d.Capture.WithDefaults()
}

func isStreamingResponse(req *http.Request, resp *http.Response) bool {
	if resp != nil {
		contentType := strings.ToLower(resp.Header.Get("Content-Type"))
		if strings.Contains(contentType, "text/event-stream") ||
			strings.Contains(contentType, "application/stream+json") {
			return true
		}
	}
	if req != nil {
		accept := strings.ToLower(req.Header.Get("Accept"))
		return strings.Contains(accept, "text/event-stream") ||
			strings.Contains(accept, "application/stream+json")
	}
	return false
}

func responseStreamContentType(req *http.Request, resp *http.Response) string {
	if resp != nil {
		if contentType := strings.TrimSpace(resp.Header.Get("Content-Type")); strings.Contains(strings.ToLower(contentType), "text/event-stream") ||
			strings.Contains(strings.ToLower(contentType), "application/stream+json") {
			return contentType
		}
	}
	if req != nil {
		accept := strings.TrimSpace(req.Header.Get("Accept"))
		if strings.Contains(strings.ToLower(accept), "text/event-stream") {
			return "text/event-stream"
		}
		if strings.Contains(strings.ToLower(accept), "application/stream+json") {
			return "application/stream+json"
		}
	}
	return ""
}

func isUpgradeResponse(resp *http.Response) bool {
	if resp == nil || resp.StatusCode != http.StatusSwitchingProtocols {
		return false
	}
	connection := strings.TrimSpace(resp.Header.Get("Connection"))
	upgrade := strings.TrimSpace(resp.Header.Get("Upgrade"))
	return strings.EqualFold(connection, "upgrade") || upgrade != ""
}

func cloneLabelsMap(labels map[string]any) map[string]any {
	cloned := make(map[string]any, len(labels))
	for key, value := range labels {
		cloned[key] = value
	}
	return cloned
}
