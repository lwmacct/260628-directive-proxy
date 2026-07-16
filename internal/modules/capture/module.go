package capture

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

	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
	"github.com/lwmacct/260628-directive-proxy/internal/core/requestmeta"
)

const Name = "builtin.capture"

const (
	defaultBodyChunkBytes = 32 << 10
	maxBodyChunkBytes     = 1 << 20
)

var (
	defaultRedactHeaders = []string{"authorization", "proxy-authorization", "cookie", "set-cookie", "x-api-key", "api-key"}
	defaultRedactQuery   = []string{"access_token", "api_key", "apikey", "key", "token"}
)

type Spec struct {
	BodyChunkBytes int      `json:"body-chunk-bytes,omitempty"`
	RedactHeaders  []string `json:"redact-headers,omitempty"`
	RedactQuery    []string `json:"redact-query,omitempty"`
}

type Module struct{}

type binding struct{ spec Spec }

type instance struct {
	spec Spec

	requestBodyEnded bool
	requestChunks    int64
	requestOffset    int64
	requestPending   []byte

	responseHash         hash.Hash
	responseOffset       int64
	responseChunks       int64
	responseStarted      bool
	responseEnded        bool
	responseIsSSE        bool
	sseEvents            uint64
	sseComments          uint64
	responseDroppedBytes int64
	responseGapEmitted   bool
}

func New() *Module { return &Module{} }

func (*Module) Name() string { return Name }

func (*Module) Compile(raw json.RawMessage) (module.Binding, error) {
	spec, err := decodeSpec(raw)
	if err != nil {
		return nil, err
	}
	return binding{spec: spec}, nil
}

func (binding binding) Lifetime() module.Lifetime { return module.LifetimeRequest }

func (binding binding) Open(module.OpenContext) (module.Instance, error) {
	return &instance{spec: binding.spec, responseHash: sha256.New()}, nil
}

func (capture *instance) Mount(binder *module.Binder) {
	async := module.AsyncPolicy(module.OverflowDrop)
	binder.OnRequestStarted(async, capture.onRequestStarted)
	binder.OnRequestBodyChunk(module.AsyncBarrierPolicy(module.OverflowBlock), capture.onRequestBodyChunk)
	binder.OnRequestBodyEnded(async, capture.onRequestBodyEnded)
	binder.OnAttemptStarted(async, capture.onAttemptStarted)
	binder.OnDirectiveResolved(async, capture.onDirectiveResolved)
	binder.OnDirectiveFailed(async, capture.onDirectiveFailed)
	binder.OnMetadataBound(async, capture.onMetadataBound)
	binder.OnMetadataChanged(async, capture.onMetadataChanged)
	binder.OnUpstreamStarted(async, capture.onUpstreamStarted)
	binder.OnAttemptFinished(async, capture.onAttemptFinished)
	binder.OnRetryRequested(async, capture.onRetryRequested)
	binder.OnDownstreamResponseStarted(async, capture.onDownstreamResponseStarted)
	binder.OnDownstreamBodyChunk(async, capture.onDownstreamBodyChunk)
	binder.OnDownstreamSSEData(async, capture.onDownstreamSSEData)
	binder.OnDownstreamSSEComment(async, capture.onDownstreamSSEComment)
	binder.OnDownstreamBodyEnded(async, capture.onDownstreamBodyEnded)
	binder.OnRequestFinished(async, capture.onRequestFinished)
}

func (capture *instance) Finish(ctx module.FinishContext) error {
	capture.finishResponse(ctx.Emitter)
	return nil
}

func (capture *instance) onRequestStarted(ctx module.EventContext, value module.RequestStarted) error {
	if ctx.Emitter == nil {
		return nil
	}
	ctx.Emitter.Emit("capture.request.started", map[string]any{
		"method": value.Method, "url": redactURL(value.URL, capture.spec.RedactQuery), "host": value.Host,
	})
	ctx.Emitter.Emit("capture.request.headers", map[string]any{
		"headers": redactHTTPHeaders(value.Header, capture.spec.RedactHeaders),
	})
	return nil
}

func (capture *instance) onRequestBodyChunk(ctx module.EventContext, value module.BodyChunk) error {
	if len(value.Data) == 0 || ctx.Emitter == nil {
		return nil
	}
	capture.requestPending = append(capture.requestPending, value.Data...)
	for len(capture.requestPending) >= capture.spec.BodyChunkBytes {
		capture.emitRequestChunk(ctx.Emitter, capture.requestPending[:capture.spec.BodyChunkBytes])
		capture.requestPending = capture.requestPending[capture.spec.BodyChunkBytes:]
	}
	return nil
}

func (capture *instance) onRequestBodyEnded(ctx module.EventContext, value module.RequestBodyEnded) error {
	if capture.requestBodyEnded || ctx.Emitter == nil {
		return nil
	}
	if len(capture.requestPending) > 0 {
		capture.emitRequestChunk(ctx.Emitter, capture.requestPending)
		capture.requestPending = nil
	}
	capture.requestBodyEnded = true
	ctx.Emitter.Emit("capture.request.body.end", map[string]any{
		"total_bytes": value.Total, "sha256": value.SHA256, "complete": value.Complete, "chunks": capture.requestChunks,
	})
	return nil
}

func (capture *instance) emitRequestChunk(emitter module.Emitter, data []byte) {
	if emitter == nil || len(data) == 0 {
		return
	}
	capture.requestChunks++
	emitter.EmitBorrowed("capture.request.body.chunk", map[string]any{
		"chunk_index": capture.requestChunks, "offset": capture.requestOffset, "length": int64(len(data)),
		"encoding": "binary", "data": data,
	})
	capture.requestOffset += int64(len(data))
}

func (*instance) onAttemptStarted(ctx module.EventContext, value module.AttemptStarted) error {
	if ctx.Emitter != nil {
		ctx.Emitter.Emit("capture.attempt.started", map[string]any{"attempt": ctx.Attempt})
		ctx.Emitter.Emit("capture.directive.resolve.started", map[string]any{
			"mode": value.Mode, "backend": value.Backend, "endpoint": value.Endpoint, "key": value.Key,
		})
	}
	return nil
}

func (capture *instance) onDirectiveResolved(ctx module.EventContext, value module.DirectiveResolved) error {
	if ctx.Emitter == nil {
		return nil
	}
	target := ""
	if value.Target != nil {
		target = redactURL(value.Target.String(), capture.spec.RedactQuery)
	}
	ctx.Emitter.Emit("capture.directive.resolve.finished", map[string]any{
		"duration_millis": value.Duration.Milliseconds(), "payload_sha256": value.PayloadSHA256,
		"target_url": target, "target_changed": value.TargetChanged, "plan_changed": value.PlanChanged,
	})
	return nil
}

func (*instance) onDirectiveFailed(ctx module.EventContext, value module.DirectiveFailed) error {
	if ctx.Emitter != nil {
		ctx.Emitter.Emit("capture.directive.resolve.failed", map[string]any{
			"duration_millis": value.Duration.Milliseconds(), "error_code": value.Code,
		})
	}
	return nil
}

func (capture *instance) onMetadataBound(ctx module.EventContext, value module.MetadataBound) error {
	if ctx.Emitter != nil {
		ctx.Emitter.Emit("capture.request.metadata.bound", map[string]any{
			"metadata": redactMetadata(value.Metadata, capture.spec.RedactHeaders),
		})
	}
	return nil
}

func (capture *instance) onMetadataChanged(ctx module.EventContext, value module.MetadataChanged) error {
	if ctx.Emitter != nil {
		ctx.Emitter.Emit("capture.request.metadata.changed", map[string]any{
			"bound_metadata":    redactMetadata(value.Bound, capture.spec.RedactHeaders),
			"observed_metadata": redactMetadata(value.Observed, capture.spec.RedactHeaders),
		})
	}
	return nil
}

func (capture *instance) onUpstreamStarted(ctx module.EventContext, value module.UpstreamStarted) error {
	if ctx.Emitter != nil {
		ctx.Emitter.Emit("capture.attempt.upstream.started", map[string]any{
			"target_url": redactURL(value.TargetURL, capture.spec.RedactQuery),
			"headers":    redactHTTPHeaders(value.Header, capture.spec.RedactHeaders),
		})
	}
	return nil
}

func (*instance) onAttemptFinished(ctx module.EventContext, value module.AttemptFinished) error {
	if ctx.Emitter != nil {
		ctx.Emitter.Emit("capture.attempt.finished", map[string]any{"attempt": ctx.Attempt, "outcome": value.Outcome})
	}
	return nil
}

func (capture *instance) onRetryRequested(ctx module.EventContext, value module.RetryRequested) error {
	if ctx.Emitter == nil {
		return nil
	}
	data := map[string]any{"trigger": value.Trigger, "attempt": ctx.Attempt, "next_attempt": value.NextAttempt}
	if len(value.SelectorMetadata) > 0 {
		data["selector_metadata"] = redactMetadata(value.SelectorMetadata, capture.spec.RedactHeaders)
	}
	ctx.Emitter.Emit("capture.retry.requested", data)
	return nil
}

func (capture *instance) onDownstreamResponseStarted(ctx module.EventContext, value module.ResponseStarted) error {
	mediaType, _, _ := mime.ParseMediaType(value.Header.Get("Content-Type"))
	capture.responseIsSSE = strings.EqualFold(mediaType, "text/event-stream")
	capture.responseStarted = true
	if ctx.Emitter != nil {
		ctx.Emitter.Emit("capture.response.headers", map[string]any{
			"status_code": value.StatusCode,
			"headers":     redactHTTPHeaders(value.Header, capture.spec.RedactHeaders),
			"sse":         capture.responseIsSSE,
		})
	}
	return nil
}

func (capture *instance) onDownstreamBodyChunk(ctx module.EventContext, value module.BodyChunk) error {
	if ctx.Emitter == nil {
		return nil
	}
	data := value.Data
	for len(data) > 0 {
		length := min(len(data), capture.spec.BodyChunkBytes)
		chunk := data[:length]
		offset := capture.responseOffset
		capture.responseOffset += int64(length)
		capture.responseChunks++
		_, _ = capture.responseHash.Write(chunk)
		accepted := ctx.Emitter.EmitBorrowed("capture.response.body.chunk", map[string]any{
			"chunk_index": capture.responseChunks, "offset": offset, "length": length,
			"encoding": "binary", "data": chunk,
		})
		if !accepted {
			capture.responseDroppedBytes += int64(length)
			if !capture.responseGapEmitted {
				capture.responseGapEmitted = true
				ctx.Emitter.Emit("capture.response.body.gap", map[string]any{"offset": offset, "reason": "output_queue_full"})
			}
		}
		data = data[length:]
	}
	return nil
}

func (capture *instance) onDownstreamSSEData(ctx module.EventContext, value module.SSEData) error {
	capture.sseEvents++
	if ctx.Emitter == nil {
		return nil
	}
	data := map[string]any{
		"sse_sequence": value.Sequence, "upstream_event_id": value.ID,
		"event": value.Event, "data": string(value.Data), "truncated": value.Truncated,
	}
	if value.RetryMillis != nil {
		data["retry_millis"] = *value.RetryMillis
	}
	ctx.Emitter.Emit("capture.response.sse.event", data)
	return nil
}

func (capture *instance) onDownstreamSSEComment(ctx module.EventContext, value module.SSEComment) error {
	capture.sseComments++
	if ctx.Emitter != nil {
		ctx.Emitter.Emit("capture.response.sse.comment", map[string]any{
			"comment_sequence": value.Sequence, "comment": value.Comment,
		})
	}
	return nil
}

func (capture *instance) onDownstreamBodyEnded(ctx module.EventContext, _ module.BodyEnded) error {
	capture.finishResponse(ctx.Emitter)
	return nil
}

func (capture *instance) onRequestFinished(ctx module.EventContext, value module.RequestFinished) error {
	capture.finishResponse(ctx.Emitter)
	if ctx.Emitter != nil {
		ctx.Emitter.Emit("capture.request.completed", map[string]any{
			"outcome": value.Outcome, "status_code": value.StatusCode, "duration_millis": value.Duration.Milliseconds(),
		})
	}
	return nil
}

func (capture *instance) finishResponse(output module.Emitter) {
	if !capture.responseStarted || capture.responseEnded || output == nil {
		return
	}
	capture.responseEnded = true
	output.Emit("capture.response.body.end", map[string]any{
		"total_bytes": capture.responseOffset, "chunks": capture.responseChunks,
		"sha256": hex.EncodeToString(capture.responseHash.Sum(nil)), "sse_events": capture.sseEvents,
		"sse_comments": capture.sseComments, "dropped_bytes": capture.responseDroppedBytes,
	})
}

func defaultSpec() Spec {
	return Spec{
		BodyChunkBytes: defaultBodyChunkBytes,
		RedactHeaders:  append([]string(nil), defaultRedactHeaders...),
		RedactQuery:    append([]string(nil), defaultRedactQuery...),
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
	defaults := defaultSpec()
	if spec.BodyChunkBytes == 0 {
		spec.BodyChunkBytes = defaults.BodyChunkBytes
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
