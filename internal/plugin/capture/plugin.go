package captureplugin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"hash"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"

	"github.com/lwmacct/260628-directive-proxy/internal/core/observability"
	"github.com/lwmacct/260628-directive-proxy/internal/core/requestmeta"
	"github.com/lwmacct/260628-directive-proxy/internal/core/sse"
)

const Name = "builtin.capture"
const DirectiveName = "capture"

type Config struct {
	Name                     string
	BodyChunkBytes           int
	MaxSSEEventBytes         int
	MaxRetainedResponseBytes int64
	ResponseOverflow         string
	RedactHeaders            []string
	RedactQuery              []string
}

type Plugin struct {
	name           string
	config         Config
	responseBudget *retainedBudget
}

type retainedBudget struct {
	mu   sync.Mutex
	cond *sync.Cond
	max  int64
	used int64
}

type traceObserver struct {
	config Config

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
	responseBudget       *retainedBudget
	responseOverflow     string
	responseDroppedBytes int64
	responseGapEmitted   bool
}

func New(config Config) *Plugin {
	name := strings.TrimSpace(config.Name)
	if name == "" {
		name = Name
	}
	if config.BodyChunkBytes <= 0 {
		config.BodyChunkBytes = 32 << 10
	}
	if config.MaxSSEEventBytes <= 0 {
		config.MaxSSEEventBytes = 1 << 20
	}
	if config.MaxRetainedResponseBytes <= 0 {
		config.MaxRetainedResponseBytes = 256 << 20
	}
	if config.ResponseOverflow != "backpressure" {
		config.ResponseOverflow = "drop"
	}
	if len(config.RedactHeaders) == 0 {
		config.RedactHeaders = []string{"authorization", "proxy-authorization", "cookie", "set-cookie", "x-api-key", "api-key"}
	}
	if len(config.RedactQuery) == 0 {
		config.RedactQuery = []string{"access_token", "api_key", "apikey", "key", "token"}
	}
	config.RedactHeaders = normalizePatterns(config.RedactHeaders)
	config.RedactQuery = normalizePatterns(config.RedactQuery)
	budget := &retainedBudget{max: config.MaxRetainedResponseBytes}
	budget.cond = sync.NewCond(&budget.mu)
	return &Plugin{name: name, config: config, responseBudget: budget}
}

func (p *Plugin) Name() string {
	if p == nil || p.name == "" {
		return Name
	}
	return p.name
}

func (*Plugin) DirectiveName() string { return DirectiveName }

func (*Plugin) ValidateSpec(raw []byte) error {
	if len(strings.TrimSpace(string(raw))) == 0 || strings.TrimSpace(string(raw)) == "null" {
		return nil
	}
	var spec map[string]any
	if err := json.Unmarshal(raw, &spec); err != nil {
		return err
	}
	return nil
}

func (p *Plugin) NewTrace(observability.TraceContext) observability.TraceObserver {
	if p == nil {
		return nil
	}
	return &traceObserver{
		config: p.config, responseHash: sha256.New(), responseBudget: p.responseBudget,
		responseOverflow: p.config.ResponseOverflow,
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
			"mode": value.Mode, "backend": value.Backend, "endpoint": redactURL(value.Endpoint, t.config.RedactQuery), "key": value.Key,
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
			"metadata": redactMetadata(value.Metadata, t.config.RedactHeaders),
		})
	case observability.MetadataChanged:
		emitter.Emit("capture.request.metadata.changed", signal.Attempt, map[string]any{
			"bound_metadata": redactMetadata(value.Bound, t.config.RedactHeaders), "observed_metadata": redactMetadata(value.Observed, t.config.RedactHeaders),
		})
	case observability.UpstreamStarted:
		emitter.Emit("capture.attempt.upstream.started", signal.Attempt, map[string]any{
			"target_url": redactURL(value.TargetURL, t.config.RedactQuery), "headers": redactHTTPHeaders(value.Header, t.config.RedactHeaders),
		})
	case observability.AttemptFinished:
		emitter.Emit("capture.attempt.finished", signal.Attempt, map[string]any{"attempt": signal.Attempt, "outcome": value.Outcome})
	case observability.RetryRequested:
		data := map[string]any{
			"trigger": value.Trigger, "attempt": signal.Attempt, "next_attempt": value.NextAttempt,
		}
		if len(value.SelectorMetadata) > 0 {
			data["selector_metadata"] = redactMetadata(value.SelectorMetadata, t.config.RedactHeaders)
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
		"method": value.Method, "url": redactURL(value.URL, t.config.RedactQuery), "host": value.Host,
	})
	emitter.Emit("capture.request.headers", 0, map[string]any{
		"headers": redactHTTPHeaders(value.Header, t.config.RedactHeaders),
	})
}

func (t *traceObserver) emitRequestBodyAvailable(value observability.RequestBodyAvailable, emitter observability.Emitter) {
	if value.Body == nil {
		return
	}
	for offset := int64(0); offset < value.Body.Size(); offset += int64(t.config.BodyChunkBytes) {
		lease := value.Body.Acquire()
		if !lease.Valid() {
			return
		}
		length := min(int64(t.config.BodyChunkBytes), value.Body.Size()-offset)
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
		target = redactURL(value.Target.String(), t.config.RedactQuery)
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
		t.sseParser = sse.NewParser(t.config.MaxSSEEventBytes, t.onSSEEvent, t.onSSEComment)
	}
	emitter.Emit("capture.response.headers", attempt, map[string]any{
		"status_code": value.StatusCode, "headers": redactHTTPHeaders(value.Header, t.config.RedactHeaders), "sse": t.responseIsSSE,
	})
}

func (t *traceObserver) emitResponseBody(data []byte, emitter observability.Emitter) {
	for len(data) > 0 {
		length := min(len(data), t.config.BodyChunkBytes)
		chunk := data[:length]
		offset := t.responseOffset
		t.responseOffset += int64(length)
		t.responseChunks++
		_, _ = t.responseHash.Write(chunk)
		release, retained := t.responseBudget.acquire(int64(length), t.responseOverflow == "backpressure")
		if retained {
			owned := append([]byte(nil), chunk...)
			emitter.EmitOwned("capture.response.body.chunk", t.responseAttempt, map[string]any{
				"chunk_index": t.responseChunks, "offset": offset, "length": length,
				"encoding": "binary", "data": owned,
			}, release)
		} else {
			t.responseDroppedBytes += int64(length)
			if !t.responseGapEmitted {
				t.responseGapEmitted = true
				emitter.Emit("capture.response.body.gap", t.responseAttempt, map[string]any{
					"offset": offset, "reason": "retained_memory_limit",
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

func (b *retainedBudget) acquire(size int64, block bool) (func(), bool) {
	if b == nil || size <= 0 {
		return func() {}, true
	}
	b.mu.Lock()
	if size > b.max {
		b.mu.Unlock()
		return nil, false
	}
	for b.used+size > b.max {
		if !block {
			b.mu.Unlock()
			return nil, false
		}
		b.cond.Wait()
	}
	b.used += size
	b.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			b.mu.Lock()
			b.used -= size
			if b.used < 0 {
				b.used = 0
			}
			b.cond.Broadcast()
			b.mu.Unlock()
		})
	}, true
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
