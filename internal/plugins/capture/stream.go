package capture

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/eventbus"
	"github.com/lwmacct/260628-llm-relay-dproxy/internal/proxyplan"
)

type streamPolicy struct {
	contentType string
	parseSSE    bool
	eventTypes  map[string]struct{}
}

func resolveStreamPolicy(capturePolicy proxyplan.CapturePolicy, contentType string) streamPolicy {
	policy := streamPolicy{
		contentType: contentType,
	}
	if !capturePolicy.StreamEvents {
		return policy
	}
	policy.parseSSE = strings.Contains(strings.ToLower(contentType), "text/event-stream")
	if policy.parseSSE && len(capturePolicy.StreamEventTypes) > 0 {
		policy.eventTypes = make(map[string]struct{}, len(capturePolicy.StreamEventTypes))
		for _, eventType := range capturePolicy.StreamEventTypes {
			eventType = strings.TrimSpace(eventType)
			if eventType != "" {
				policy.eventTypes[eventType] = struct{}{}
			}
		}
	}
	return policy
}

type streamRecorder struct {
	inner          io.ReadCloser
	requestID      string
	method         string
	path           string
	targetHost     string
	readChunks     int
	totalBytes     int64
	startTime      time.Time
	ctx            context.Context
	publisher      eventbus.Publisher
	idGen          eventbus.IDGenerator
	labels         map[string]any
	runtime        eventbus.Runtime
	policy         streamPolicy
	detailSequence int
	detailBytes    int64
	detailRecords  int
	streamErr      error
	pendingSSE     []byte
	endOnce        sync.Once
}

func newStreamRecorder(
	inner io.ReadCloser,
	requestID string,
	method string,
	path string,
	targetHost string,
	contentType string,
	policy streamPolicy,
	publisher eventbus.Publisher,
	idGen eventbus.IDGenerator,
	labels map[string]any,
	runtime eventbus.Runtime,
	ctx context.Context,
) *streamRecorder {
	policy.contentType = contentType
	if publisher == nil {
		publisher = eventbus.NopPublisher{}
	}
	if idGen == nil {
		idGen = eventbus.NewIDGenerator()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return &streamRecorder{
		inner:      inner,
		requestID:  requestID,
		method:     method,
		path:       path,
		targetHost: targetHost,
		startTime:  time.Now(),
		ctx:        ctx,
		publisher:  publisher,
		idGen:      idGen,
		labels:     cloneLabelsMap(labels),
		runtime:    eventbus.CloneRuntime(runtime),
		policy:     policy,
	}
}

func (s *streamRecorder) Read(p []byte) (n int, err error) {
	n, err = s.inner.Read(p)
	if n > 0 {
		chunk := make([]byte, n)
		copy(chunk, p[:n])
		s.readChunks++
		s.totalBytes += int64(n)
		s.recordDetail(chunk)
	}
	if err != nil && err != io.EOF {
		s.streamErr = err
	}
	return n, err
}

func (s *streamRecorder) Close() error {
	s.end()
	return s.inner.Close()
}

func (s *streamRecorder) end() {
	s.endOnce.Do(func() {
		if s.policy.parseSSE && len(bytes.TrimSpace(s.pendingSSE)) > 0 {
			s.emitSSEEvent(s.pendingSSE)
			s.pendingSSE = nil
		}
		data := StreamEndData{
			TotalReads:   s.readChunks,
			TotalBytes:   s.totalBytes,
			Duration:     time.Since(s.startTime),
			EventRecords: s.detailRecords,
			EventBytes:   s.detailBytes,
		}
		if s.streamErr != nil {
			data.Error = s.streamErr.Error()
			slog.Warn("upstream stream terminated with read error",
				"request_id", s.requestID,
				"method", s.method,
				"path", s.path,
				"target_host", s.targetHost,
				"content_type", s.policy.contentType,
				"total_reads", s.readChunks,
				"total_bytes", s.totalBytes,
				"duration", time.Since(s.startTime),
				"error", s.streamErr)
		}
		_ = s.publisher.Publish(s.ctx, s.newEvent(EventTypeStreamEnd, data))
	})
}

func (s *streamRecorder) recordDetail(data []byte) {
	if !s.policy.parseSSE || len(data) == 0 {
		return
	}
	s.pendingSSE = append(s.pendingSSE, data...)
	for {
		eventData, rest, ok := nextSSEEvent(s.pendingSSE)
		if !ok {
			s.pendingSSE = rest
			return
		}
		s.pendingSSE = rest
		if len(bytes.TrimSpace(eventData)) == 0 {
			continue
		}
		s.emitSSEEvent(eventData)
	}
}

func (s *streamRecorder) emitSSEEvent(raw []byte) {
	parsed := parseSSEEvent(raw)
	if !s.shouldEmitSSEEvent(parsed.Event) {
		return
	}
	event := s.newEvent(EventTypeStreamEvent, StreamEventData{
		Sequence:  s.detailSequence,
		EventName: parsed.Event,
		SSEID:     parsed.ID,
		Retry:     parsed.Retry,
		Payload:   parsed.DataValue(),
		Size:      len(raw),
	})
	if err := s.publisher.Publish(s.ctx, event); err != nil {
		return
	}
	s.detailSequence++
	s.detailRecords++
	s.detailBytes += int64(len(raw))
}

func (s *streamRecorder) newEvent(typ eventbus.Type, data any) eventbus.Event {
	return eventbus.Event{
		EventID:   s.idGen.Generate(),
		RequestID: s.requestID,
		Type:      typ,
		Timestamp: time.Now(),
		Labels:    cloneLabelsMap(s.labels),
		Runtime:   eventbus.CloneRuntime(s.runtime),
		Data:      data,
	}
}

func (s *streamRecorder) shouldEmitSSEEvent(event string) bool {
	if len(s.policy.eventTypes) == 0 {
		return true
	}
	_, ok := s.policy.eventTypes[event]
	return ok
}

func nextSSEEvent(buf []byte) (event []byte, rest []byte, ok bool) {
	lfIdx := bytes.Index(buf, []byte("\n\n"))
	crlfIdx := bytes.Index(buf, []byte("\r\n\r\n"))
	switch {
	case lfIdx >= 0 && (crlfIdx < 0 || lfIdx < crlfIdx):
		end := lfIdx + len("\n\n")
		return append([]byte(nil), buf[:end]...), buf[end:], true
	case crlfIdx >= 0:
		end := crlfIdx + len("\r\n\r\n")
		return append([]byte(nil), buf[:end]...), buf[end:], true
	default:
		return nil, buf, false
	}
}

type parsedSSEEvent struct {
	Event string
	ID    string
	Retry string
	Data  []string
}

func (e parsedSSEEvent) DataValue() any {
	if len(e.Data) == 0 {
		return nil
	}
	data := strings.Join(e.Data, "\n")
	var value any
	if err := json.Unmarshal([]byte(data), &value); err == nil {
		return value
	}
	return data
}

func parseSSEEvent(raw []byte) parsedSSEEvent {
	normalized := strings.ReplaceAll(string(raw), "\r\n", "\n")
	normalized = strings.TrimRight(normalized, "\n")

	var parsed parsedSSEEvent
	for _, line := range strings.Split(normalized, "\n") {
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		value = strings.TrimPrefix(value, " ")
		switch key {
		case "event":
			parsed.Event = value
		case "id":
			parsed.ID = value
		case "retry":
			parsed.Retry = value
		case "data":
			parsed.Data = append(parsed.Data, value)
		}
	}
	return parsed
}
