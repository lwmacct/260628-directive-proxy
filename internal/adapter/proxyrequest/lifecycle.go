package proxyrequestadapter

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"hash"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/capture"
	"github.com/lwmacct/260628-directive-proxy/internal/core/proxyrequest"
	"github.com/lwmacct/260628-directive-proxy/internal/core/sse"
)

type ProxyRequestOptions struct {
	RetryAfter        time.Duration
	MaxAttempts       int
	MaxActiveRequests int
	InstanceID        string
	BodyChunkBytes    int
	MaxSSEEventBytes  int
	RedactHeaders     []string
	RedactQuery       []string
}

type proxyRequestSession struct {
	service       *ProxyRequestService
	ctx           context.Context
	traceID       string
	startedAt     time.Time
	method        string
	requestURL    string
	targetURL     string
	state         proxyrequest.State
	attempt       int
	attemptAt     time.Time
	cancelAttempt func()

	emitMu   sync.Mutex
	sequence uint64

	bodyMu           sync.Mutex
	requestBodyEnded bool
	requestChunks    int64
	responseBodyHash hash.Hash
	responseOffset   int64
	responseChunks   int64
	responseEnded    bool
	responseStatus   int
	responseIsSSE    bool
	sseParser        *sse.Parser
	sseEvents        uint64
	sseComments      uint64

	completeOnce sync.Once
}

type capturePolicy struct {
	bodyChunkBytes   int
	maxSSEEventBytes int
	redactHeaders    []string
	redactQuery      []string
}

type proxyResponseWriter struct {
	http.ResponseWriter
	session *proxyRequestSession
	wrote   bool
	status  int
}

func (s *proxyRequestSession) TraceID() string { return s.traceID }

func (s *proxyRequestSession) SetTargetURL(target *url.URL) {
	if s == nil || target == nil {
		return
	}
	s.service.mu.Lock()
	s.targetURL = redactURL(target.String(), s.service.policy.redactQuery)
	s.service.mu.Unlock()
}

func (s *proxyRequestSession) SetDirective(mode, backend, endpoint, key string, duration time.Duration) {
	if s == nil {
		return
	}
	s.emit("lifecycle", "directive.resolved", 0, map[string]any{
		"mode":            mode,
		"backend":         backend,
		"endpoint":        redactURL(endpoint, s.service.policy.redactQuery),
		"key":             key,
		"duration_millis": duration.Milliseconds(),
	})
}

func (s *proxyRequestSession) WrapResponseWriter(w http.ResponseWriter) http.ResponseWriter {
	if s == nil || w == nil {
		return w
	}
	w.Header().Set("X-Dproxy-Trace-ID", s.traceID)
	return &proxyResponseWriter{ResponseWriter: w, session: s}
}

func (s *proxyRequestSession) RequestBodyChunk(data []byte, offset int64) {
	if s == nil || len(data) == 0 {
		return
	}
	chunkBytes := s.service.policy.bodyChunkBytes
	for len(data) > 0 {
		length := min(len(data), chunkBytes)
		chunk := data[:length]
		s.bodyMu.Lock()
		s.requestChunks++
		chunkIndex := s.requestChunks
		s.bodyMu.Unlock()
		s.emit("request.body", "request.body.chunk", 0, map[string]any{
			"chunk_index": chunkIndex,
			"offset":      offset,
			"length":      length,
			"encoding":    "base64",
			"data":        base64.StdEncoding.EncodeToString(chunk),
		})
		offset += int64(length)
		data = data[length:]
	}
}

func (s *proxyRequestSession) RequestBodyEnd(total int64, digest string, complete bool) {
	if s == nil {
		return
	}
	s.bodyMu.Lock()
	if s.requestBodyEnded {
		s.bodyMu.Unlock()
		return
	}
	s.requestBodyEnded = true
	chunks := s.requestChunks
	s.bodyMu.Unlock()
	s.emit("request.body", "request.body.end", 0, map[string]any{
		"total_bytes": total,
		"sha256":      digest,
		"complete":    complete,
		"chunks":      chunks,
	})
}

func (s *proxyRequestSession) BeginAttempt(req *http.Request, cancel func()) int {
	if s == nil {
		return 0
	}
	now := time.Now().UTC()
	s.service.mu.Lock()
	if _, exists := s.service.active[s.traceID]; !exists && s.service.maxActive > 0 && len(s.service.active) >= s.service.maxActive {
		s.service.mu.Unlock()
		s.emit("lifecycle", "attempt.rejected", s.attempt+1, map[string]any{"reason": "active_capacity"})
		return 0
	}
	s.attempt++
	s.attemptAt = now
	s.cancelAttempt = cancel
	s.state = proxyrequest.StateAwaitingResponse
	s.service.active[s.traceID] = s
	attempt := s.attempt
	target := s.targetURL
	s.service.mu.Unlock()

	data := map[string]any{"attempt": attempt, "target_url": target}
	if req != nil {
		data["headers"] = redactHTTPHeaders(req.Header, s.service.policy.redactHeaders)
	}
	s.emit("attempt", "attempt.started", attempt, data)
	return attempt
}

func (s *proxyRequestSession) FinishAttempt(attempt int, responseStarted bool, attemptErr error) proxyrequest.AttemptAction {
	if s == nil {
		return proxyrequest.AttemptReturn
	}
	s.service.mu.Lock()
	action := proxyrequest.AttemptReturn
	state := s.state
	if attempt == s.attempt && state == proxyrequest.StateRetryRequested && s.ctx.Err() == nil {
		action = proxyrequest.AttemptRetry
	} else {
		delete(s.service.active, s.traceID)
	}
	s.cancelAttempt = nil
	s.service.mu.Unlock()

	outcome := "response_headers"
	if action == proxyrequest.AttemptRetry {
		outcome = "canceled_for_retry"
	} else if attemptErr != nil {
		outcome = "transport_error"
	} else if !responseStarted {
		outcome = "ended_without_response"
	}
	s.emit("attempt", "attempt.finished", attempt, map[string]any{
		"attempt": attempt,
		"outcome": outcome,
	})
	return action
}

func (s *proxyRequestSession) Complete() {
	if s == nil {
		return
	}
	s.completeOnce.Do(func() {
		s.service.mu.Lock()
		delete(s.service.active, s.traceID)
		s.cancelAttempt = nil
		s.service.mu.Unlock()

		s.RequestBodyEnd(0, "", false)
		s.finishResponseBody()
		outcome := "completed"
		if s.ctx.Err() != nil {
			outcome = "client_canceled"
		}
		s.bodyMu.Lock()
		status := s.responseStatus
		s.bodyMu.Unlock()
		s.emit("lifecycle", "request.completed", s.currentAttempt(), map[string]any{
			"outcome":         outcome,
			"status_code":     status,
			"duration_millis": time.Since(s.startedAt).Milliseconds(),
		})
	})
}

func (s *proxyRequestSession) currentAttempt() int {
	s.service.mu.RLock()
	attempt := s.attempt
	s.service.mu.RUnlock()
	return attempt
}

func (s *proxyRequestSession) responseHeaders(status int, headers http.Header) {
	s.bodyMu.Lock()
	s.responseStatus = status
	mediaType, _, _ := mime.ParseMediaType(headers.Get("Content-Type"))
	isSSE := strings.EqualFold(mediaType, "text/event-stream")
	s.responseIsSSE = isSSE
	if isSSE && strings.TrimSpace(headers.Get("Content-Encoding")) == "" {
		s.sseParser = s.newSSEParser()
	}
	s.bodyMu.Unlock()
	s.emit("response.headers", "response.headers", s.currentAttempt(), map[string]any{
		"status_code": status,
		"headers":     redactHTTPHeaders(headers, s.service.policy.redactHeaders),
		"sse":         isSSE,
	})
}

func (s *proxyRequestSession) responseBodyChunk(data []byte) {
	if len(data) == 0 {
		return
	}
	s.bodyMu.Lock()
	chunkBytes := s.service.policy.bodyChunkBytes
	for len(data) > 0 {
		length := min(len(data), chunkBytes)
		chunk := data[:length]
		offset := s.responseOffset
		s.responseOffset += int64(length)
		s.responseChunks++
		chunkIndex := s.responseChunks
		_, _ = s.responseBodyHash.Write(chunk)
		s.emit("response.body", "response.body.chunk", s.currentAttempt(), map[string]any{
			"chunk_index": chunkIndex,
			"offset":      offset,
			"length":      length,
			"encoding":    "base64",
			"data":        base64.StdEncoding.EncodeToString(chunk),
		})
		if s.sseParser != nil {
			s.sseParser.Feed(chunk)
		}
		data = data[length:]
	}
	s.bodyMu.Unlock()
}

func (s *proxyRequestSession) finishResponseBody() {
	s.bodyMu.Lock()
	if s.responseEnded {
		s.bodyMu.Unlock()
		return
	}
	s.responseEnded = true
	if s.sseParser != nil {
		s.sseParser.Close()
	}
	total := s.responseOffset
	chunks := s.responseChunks
	digest := hex.EncodeToString(s.responseBodyHash.Sum(nil))
	events := s.sseEvents
	comments := s.sseComments
	s.bodyMu.Unlock()
	s.emit("response.body", "response.body.end", s.currentAttempt(), map[string]any{
		"total_bytes":  total,
		"chunks":       chunks,
		"sha256":       digest,
		"sse_events":   events,
		"sse_comments": comments,
	})
}

func (s *proxyRequestSession) newSSEParser() *sse.Parser {
	return sse.NewParser(s.service.policy.maxSSEEventBytes, func(event sse.Event) {
		s.sseEvents++
		data := map[string]any{
			"sse_sequence":      event.Sequence,
			"sse_event_id":      fmt.Sprintf("%s:e%d", s.traceID, event.Sequence),
			"upstream_event_id": event.ID,
			"event":             event.Type,
			"data":              event.Data,
			"truncated":         event.Truncated,
		}
		if event.RetryMillis != nil {
			data["retry_millis"] = *event.RetryMillis
		}
		s.emit("response.sse", "response.sse.event", s.currentAttempt(), data)
	}, func(comment string) {
		s.sseComments++
		s.emit("response.sse", "response.sse.comment", s.currentAttempt(), map[string]any{
			"comment_sequence": s.sseComments,
			"comment":          comment,
		})
	})
}

func (s *proxyRequestSession) emit(tag, kind string, attempt int, data map[string]any) {
	if s == nil || s.service == nil {
		return
	}
	s.emitMu.Lock()
	defer s.emitMu.Unlock()
	s.sequence++
	now := time.Now().UTC()
	attemptID := ""
	if attempt > 0 {
		attemptID = fmt.Sprintf("%s:a%d", s.traceID, attempt)
	}
	event := capture.Event{
		SchemaVersion: capture.SchemaVersion,
		RecordID:      fmt.Sprintf("%s:%08d", s.traceID, s.sequence),
		TraceID:       s.traceID,
		AttemptID:     attemptID,
		InstanceID:    s.service.instanceID,
		Sequence:      s.sequence,
		Kind:          kind,
		OccurredAt:    now.Format(time.RFC3339Nano),
		Data:          data,
		Time:          now,
	}
	if err := s.service.sink.Emit(tag, event); err != nil {
		s.service.logCaptureError(err)
	}
}

func (w *proxyResponseWriter) WriteHeader(status int) {
	if w.wrote {
		return
	}
	if status >= 100 && status < 200 && status != http.StatusSwitchingProtocols {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	w.wrote = true
	w.status = status
	w.ResponseWriter.WriteHeader(status)
	w.session.responseHeaders(status, w.Header().Clone())
}

func (w *proxyResponseWriter) Write(data []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	written, err := w.ResponseWriter.Write(data)
	w.session.bodyMu.Lock()
	isSSE := w.session.responseIsSSE
	w.session.bodyMu.Unlock()
	if isSSE {
		_ = http.NewResponseController(w.ResponseWriter).Flush()
	}
	if written > 0 {
		w.session.responseBodyChunk(data[:written])
	}
	return written, err
}

func (w *proxyResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func newTraceID() string {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err == nil {
		return hex.EncodeToString(data)
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
	return hex.EncodeToString(sum[:16])
}

func requestURL(req *http.Request) string {
	if req.URL == nil {
		return ""
	}
	value := *req.URL
	if value.Scheme == "" {
		value.Scheme = "http"
	}
	if value.Host == "" {
		value.Host = req.Host
	}
	return value.String()
}

func redactURL(value string, patterns []string) string {
	parsed, err := url.Parse(value)
	if err != nil {
		return value
	}
	if parsed.RawQuery != "" {
		query := parsed.Query()
		for name := range query {
			if matchesPattern(name, patterns) {
				query[name] = []string{"<redacted>"}
			}
		}
		parsed.RawQuery = query.Encode()
	}
	return parsed.Redacted()
}

func redactHTTPHeaders(headers http.Header, patterns []string) map[string][]string {
	if len(headers) == 0 {
		return nil
	}
	result := make(map[string][]string, len(headers))
	for name, values := range headers {
		canonical := http.CanonicalHeaderKey(name)
		if matchesPattern(canonical, patterns) {
			result[canonical] = []string{"<redacted>"}
		} else {
			result[canonical] = append([]string(nil), values...)
		}
	}
	return result
}

func normalizePatterns(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

func matchesPattern(value string, patterns []string) bool {
	value = strings.ToLower(value)
	for _, pattern := range patterns {
		matched, err := path.Match(pattern, value)
		if err == nil && matched {
			return true
		}
	}
	return false
}
