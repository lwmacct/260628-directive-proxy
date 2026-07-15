package captureplugin

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/lwmacct/260628-directive-proxy/internal/core/observability"
	"github.com/lwmacct/260628-directive-proxy/internal/core/requestmeta"
	"github.com/lwmacct/260628-directive-proxy/internal/core/sse"
)

const Name = "builtin.capture"
const DirectiveName = "capture"

const (
	defaultBodyChunkBytes   = 32 << 10
	defaultMaxSSEEventBytes = 1 << 20
	maxBodyChunkBytes       = 1 << 20
	maxSSEEventBytes        = 16 << 20
)

var (
	defaultRedactHeaders = []string{"authorization", "proxy-authorization", "cookie", "set-cookie", "x-api-key", "api-key"}
	defaultRedactQuery   = []string{"access_token", "api_key", "apikey", "key", "token"}
)

type Spec struct {
	BodyChunkBytes   int      `json:"body-chunk-bytes,omitempty"`
	MaxSSEEventBytes int      `json:"max-sse-event-bytes,omitempty"`
	RedactHeaders    []string `json:"redact-headers,omitempty"`
	RedactQuery      []string `json:"redact-query,omitempty"`
}

type Plugin struct {
	spec Spec
}

type traceObserver struct {
	spec Spec

	requestBodyEnded bool
	requestChunks    int64

	responseHash         hash.Hash
	responseOffset       int64
	responseChunks       int64
	responseEnded        bool
	responseIsSSE        bool
	sseParser            *sse.Parser
	sseEvents            uint64
	sseComments          uint64
	responseEmitter      observability.Emitter
	responseAttempt      int
	responseDroppedBytes int64
	responseGapEmitted   bool
}

func New() *Plugin {
	return &Plugin{spec: defaultSpec()}
}

func (p *Plugin) Name() string {
	return Name
}

func (*Plugin) DirectiveName() string { return DirectiveName }

func (*Plugin) ConfigureSpec(raw []byte) (observability.Plugin, error) {
	spec, err := decodeSpec(raw)
	if err != nil {
		return nil, err
	}
	return &Plugin{spec: spec}, nil
}

func (p *Plugin) ValidateSpec(raw []byte) error {
	_, err := p.ConfigureSpec(raw)
	return err
}

func (p *Plugin) NewTrace(observability.TraceContext) observability.TraceObserver {
	if p == nil {
		return nil
	}
	return &traceObserver{
		spec: p.spec, responseHash: sha256.New(),
	}
}

func (t *traceObserver) Observe(signal observability.Signal, emitter observability.Emitter) {
	switch value := signal.Value.(type) {
	case observability.RequestStarted:
		t.emitRequestStarted(value, emitter)
	case observability.RequestBodyAvailable:
		t.emitRequestBodyAvailable(value, emitter)
	case observability.RequestBodyEnded:
		t.emitRequestBodyEnd(value, emitter)
	case observability.AttemptStarted:
		emitter.Emit("capture.attempt.started", signal.Attempt, map[string]any{"attempt": signal.Attempt})
		emitter.Emit("capture.directive.resolve.started", signal.Attempt, map[string]any{
			"mode": value.Mode, "backend": value.Backend, "endpoint": redactURL(value.Endpoint, t.spec.RedactQuery), "key": value.Key,
		})
	case observability.AttemptRejected:
		emitter.Emit("capture.attempt.rejected", signal.Attempt, map[string]any{"reason": value.Reason})
	case observability.DirectiveResolved:
		t.emitDirectiveResolved(signal.Attempt, value, emitter)
	case observability.DirectiveFailed:
		emitter.Emit("capture.directive.resolve.failed", signal.Attempt, map[string]any{
			"duration_millis": value.Duration.Milliseconds(), "error_code": value.Code,
		})
	case observability.MetadataBound:
		emitter.Emit("capture.request.metadata.bound", signal.Attempt, map[string]any{
			"metadata": redactMetadata(value.Metadata, t.spec.RedactHeaders),
		})
	case observability.MetadataChanged:
		emitter.Emit("capture.request.metadata.changed", signal.Attempt, map[string]any{
			"bound_metadata": redactMetadata(value.Bound, t.spec.RedactHeaders), "observed_metadata": redactMetadata(value.Observed, t.spec.RedactHeaders),
		})
	case observability.UpstreamStarted:
		emitter.Emit("capture.attempt.upstream.started", signal.Attempt, map[string]any{
			"target_url": redactURL(value.TargetURL, t.spec.RedactQuery), "headers": redactHTTPHeaders(value.Header, t.spec.RedactHeaders),
		})
	case observability.AttemptFinished:
		emitter.Emit("capture.attempt.finished", signal.Attempt, map[string]any{"attempt": signal.Attempt, "outcome": value.Outcome})
	case observability.RetryRequested:
		data := map[string]any{
			"trigger": value.Trigger, "attempt": signal.Attempt, "next_attempt": value.NextAttempt,
		}
		if len(value.SelectorMetadata) > 0 {
			data["selector_metadata"] = redactMetadata(value.SelectorMetadata, t.spec.RedactHeaders)
		}
		emitter.Emit("capture.retry.requested", signal.Attempt, data)
	case observability.DownstreamResponseStarted:
		t.startResponse(signal.Attempt, value, emitter)
	case observability.DownstreamBodyChunk:
		t.emitResponseBody(value.Data, emitter)
	case observability.DownstreamBodyEnded:
		t.finishResponse(emitter)
	case observability.RequestCompleted:
		t.finishResponse(emitter)
		emitter.Emit("capture.request.completed", signal.Attempt, map[string]any{
			"outcome": value.Outcome, "status_code": value.StatusCode, "duration_millis": value.Duration.Milliseconds(),
		})
	}
}

func (t *traceObserver) Close(emitter observability.Emitter) {
	t.finishResponse(emitter)
}

func (t *traceObserver) emitRequestStarted(value observability.RequestStarted, emitter observability.Emitter) {
	emitter.Emit("capture.request.started", 0, map[string]any{
		"method": value.Method, "url": redactURL(value.URL, t.spec.RedactQuery), "host": value.Host,
	})
	emitter.Emit("capture.request.headers", 0, map[string]any{
		"headers": redactHTTPHeaders(value.Header, t.spec.RedactHeaders),
	})
}

func (t *traceObserver) emitRequestBodyAvailable(value observability.RequestBodyAvailable, emitter observability.Emitter) {
	if value.Body == nil {
		return
	}
	for offset := int64(0); offset < value.Body.Size(); offset += int64(t.spec.BodyChunkBytes) {
		lease := value.Body.Acquire()
		if !lease.Valid() {
			return
		}
		length := min(int64(t.spec.BodyChunkBytes), value.Body.Size()-offset)
		data := lease.Bytes()[offset : offset+length]
		t.requestChunks++
		emitter.EmitOwned("capture.request.body.chunk", 0, map[string]any{
			"chunk_index": t.requestChunks, "offset": offset, "length": length,
			"encoding": "binary", "data": data,
		}, func() { _ = lease.Close() })
	}
}

func (t *traceObserver) emitRequestBodyEnd(value observability.RequestBodyEnded, emitter observability.Emitter) {
	if t.requestBodyEnded {
		return
	}
	t.requestBodyEnded = true
	emitter.Emit("capture.request.body.end", 0, map[string]any{
		"total_bytes": value.Total, "sha256": value.SHA256, "complete": value.Complete, "chunks": t.requestChunks,
	})
}

func (t *traceObserver) emitDirectiveResolved(attempt int, value observability.DirectiveResolved, emitter observability.Emitter) {
	target := ""
	if value.Target != nil {
		target = redactURL(value.Target.String(), t.spec.RedactQuery)
	}
	emitter.Emit("capture.directive.resolve.finished", attempt, map[string]any{
		"duration_millis": value.Duration.Milliseconds(), "payload_sha256": value.PayloadSHA256,
		"target_url": target, "target_changed": value.TargetChanged, "plan_changed": value.PlanChanged,
	})
}

func (t *traceObserver) startResponse(attempt int, value observability.DownstreamResponseStarted, emitter observability.Emitter) {
	mediaType, _, _ := mime.ParseMediaType(value.Header.Get("Content-Type"))
	contentEncoding := strings.TrimSpace(value.Header.Get("Content-Encoding"))
	t.responseIsSSE = strings.EqualFold(mediaType, "text/event-stream")
	t.responseAttempt = attempt
	t.responseEmitter = emitter
	if t.responseIsSSE && (contentEncoding == "" || strings.EqualFold(contentEncoding, "identity")) {
		t.sseParser = sse.NewParser(t.spec.MaxSSEEventBytes, t.onSSEEvent, t.onSSEComment)
	}
	emitter.Emit("capture.response.headers", attempt, map[string]any{
		"status_code": value.StatusCode, "headers": redactHTTPHeaders(value.Header, t.spec.RedactHeaders), "sse": t.responseIsSSE,
	})
}

func (t *traceObserver) emitResponseBody(data []byte, emitter observability.Emitter) {
	for len(data) > 0 {
		length := min(len(data), t.spec.BodyChunkBytes)
		chunk := data[:length]
		offset := t.responseOffset
		t.responseOffset += int64(length)
		t.responseChunks++
		_, _ = t.responseHash.Write(chunk)
		accepted := emitter.EmitBorrowed("capture.response.body.chunk", t.responseAttempt, map[string]any{
			"chunk_index": t.responseChunks, "offset": offset, "length": length,
			"encoding": "binary", "data": chunk,
		})
		if !accepted {
			t.responseDroppedBytes += int64(length)
			if !t.responseGapEmitted {
				t.responseGapEmitted = true
				emitter.Emit("capture.response.body.gap", t.responseAttempt, map[string]any{
					"offset": offset, "reason": "output_queue_full",
				})
			}
		}
		if t.sseParser != nil {
			t.sseParser.Feed(chunk)
		}
		data = data[length:]
	}
}

func (t *traceObserver) finishResponse(emitter observability.Emitter) {
	if t.responseEnded {
		return
	}
	t.responseEnded = true
	if t.sseParser != nil {
		t.sseParser.Close()
	}
	emitter.Emit("capture.response.body.end", t.responseAttempt, map[string]any{
		"total_bytes": t.responseOffset, "chunks": t.responseChunks,
		"sha256": hex.EncodeToString(t.responseHash.Sum(nil)), "sse_events": t.sseEvents, "sse_comments": t.sseComments,
		"dropped_bytes": t.responseDroppedBytes,
	})
}

func (t *traceObserver) onSSEEvent(event sse.Event) {
	t.sseEvents++
	data := map[string]any{
		"sse_sequence": event.Sequence, "upstream_event_id": event.ID,
		"event": event.Type, "data": event.Data, "truncated": event.Truncated,
	}
	if event.RetryMillis != nil {
		data["retry_millis"] = *event.RetryMillis
	}
	if t.responseEmitter != nil {
		t.responseEmitter.Emit("capture.response.sse.event", t.responseAttempt, data)
	}
}

func (t *traceObserver) onSSEComment(comment string) {
	t.sseComments++
	if t.responseEmitter != nil {
		t.responseEmitter.Emit("capture.response.sse.comment", t.responseAttempt, map[string]any{
			"comment_sequence": t.sseComments, "comment": comment,
		})
	}
}

func defaultSpec() Spec {
	return Spec{
		BodyChunkBytes: defaultBodyChunkBytes, MaxSSEEventBytes: defaultMaxSSEEventBytes,
		RedactHeaders: append([]string(nil), defaultRedactHeaders...),
		RedactQuery:   append([]string(nil), defaultRedactQuery...),
	}
}

func decodeSpec(raw []byte) (Spec, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var spec Spec
	if err := decoder.Decode(&spec); err != nil {
		return Spec{}, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return Spec{}, fmt.Errorf("multiple JSON values")
	}
	if spec.BodyChunkBytes < 0 || spec.BodyChunkBytes > maxBodyChunkBytes {
		return Spec{}, fmt.Errorf("body-chunk-bytes must be between 0 and %d", maxBodyChunkBytes)
	}
	if spec.MaxSSEEventBytes < 0 || spec.MaxSSEEventBytes > maxSSEEventBytes {
		return Spec{}, fmt.Errorf("max-sse-event-bytes must be between 0 and %d", maxSSEEventBytes)
	}
	defaults := defaultSpec()
	if spec.BodyChunkBytes == 0 {
		spec.BodyChunkBytes = defaults.BodyChunkBytes
	}
	if spec.MaxSSEEventBytes == 0 {
		spec.MaxSSEEventBytes = defaults.MaxSSEEventBytes
	}
	if spec.RedactHeaders == nil {
		spec.RedactHeaders = defaults.RedactHeaders
	}
	if spec.RedactQuery == nil {
		spec.RedactQuery = defaults.RedactQuery
	}
	var err error
	if spec.RedactHeaders, err = validatePatterns(spec.RedactHeaders, false); err != nil {
		return Spec{}, fmt.Errorf("redact-headers: %w", err)
	}
	if spec.RedactQuery, err = validatePatterns(spec.RedactQuery, true); err != nil {
		return Spec{}, fmt.Errorf("redact-query: %w", err)
	}
	return spec, nil
}

func validatePatterns(values []string, allowEmpty bool) ([]string, error) {
	if len(values) == 0 && !allowEmpty {
		return nil, fmt.Errorf("must not be empty")
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		value := strings.ToLower(strings.TrimSpace(raw))
		if value == "" {
			return nil, fmt.Errorf("contains an empty pattern")
		}
		if _, err := path.Match(value, "capture-test-value"); err != nil {
			return nil, fmt.Errorf("invalid pattern %q", raw)
		}
		if _, exists := seen[value]; exists {
			return nil, fmt.Errorf("duplicate pattern %q", value)
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result, nil
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

func redactMetadata(metadata requestmeta.Metadata, patterns []string) map[string][]string {
	if len(metadata) == 0 {
		return nil
	}
	headers := make(http.Header, len(metadata))
	for name, values := range metadata {
		headers[name] = append([]string(nil), values...)
	}
	return redactHTTPHeaders(headers, patterns)
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
