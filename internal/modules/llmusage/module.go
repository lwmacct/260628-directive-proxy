package llmusage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"strings"

	"github.com/lwmacct/260714-go-pkg-llmusage/pkg/llmusage"

	"github.com/lwmacct/260628-directive-proxy/internal/core/event"
	"github.com/lwmacct/260628-directive-proxy/internal/core/lifecycle"
	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
)

const Name = "builtin.llmusage"

type Spec struct {
	Protocol            llmusage.Protocol `json:"protocol"`
	Labels              map[string]string `json:"labels,omitempty"`
	MaxSSEMetadataBytes int               `json:"max-sse-metadata-bytes,omitempty"`
	MaxResultBytes      int               `json:"max-result-bytes,omitempty"`
	MaxNestingDepth     int               `json:"max-nesting-depth,omitempty"`
}

type Module struct{}

type binding struct{ spec Spec }

type instance struct {
	decoder  *llmusage.Decoder
	spec     Spec
	format   llmusage.Format
	results  int
	failed   bool
	finished bool
}

func New() *Module { return &Module{} }

func (*Module) Name() string { return Name }

func (*Module) Lifetime() module.Lifetime { return module.LifetimeRoundTrip }

func (*Module) Compile(raw json.RawMessage) (module.Binding, error) {
	spec, err := decodeSpec(raw)
	if err != nil {
		return nil, err
	}
	return binding{spec: spec}, nil
}

func (binding binding) Open(module.OpenContext) (module.Instance, error) {
	return &instance{spec: binding.spec}, nil
}

func (usage *instance) Bind(binder module.Registrar) {
	async := module.AsyncPolicy(module.OverflowBlock)
	binder.OnUpstreamResponseStarted(async, usage.onResponseStarted)
	binder.OnUpstreamSSEData(async, usage.onSSEData)
	binder.OnUpstreamJSONChunk(async, usage.onJSONChunk)
	binder.OnUpstreamBodyEnded(async, usage.onBodyEnded)
}

func (usage *instance) Finish(ctx module.FinishContext) error {
	usage.finish(io.ErrUnexpectedEOF, ctx.Emitter)
	return nil
}

func (usage *instance) onResponseStarted(ctx module.Context, response lifecycle.ResponseStarted) error {
	usage.start(response, ctx.Emitter)
	return nil
}

func (usage *instance) onSSEData(ctx module.Context, event lifecycle.SSEData) error {
	usage.feed(encodeSSEData(event), ctx.Emitter)
	return nil
}

func (usage *instance) onJSONChunk(ctx module.Context, chunk lifecycle.BodyChunk) error {
	usage.feed(chunk.Data, ctx.Emitter)
	return nil
}

func (usage *instance) onBodyEnded(ctx module.Context, ended lifecycle.BodyEnded) error {
	usage.finish(ended.Cause, ctx.Emitter)
	return nil
}

func (usage *instance) start(response lifecycle.ResponseStarted, output event.Emitter) {
	if usage.decoder != nil || usage.finished || response.StatusCode < 200 || response.StatusCode >= 300 {
		return
	}
	contentEncoding := strings.TrimSpace(response.Header.Get("Content-Encoding"))
	if contentEncoding != "" && !strings.EqualFold(contentEncoding, "identity") {
		usage.emitFailure("content_encoding", fmt.Errorf("unsupported content encoding %q", contentEncoding), output)
		return
	}
	format, ok := responseFormat(response.Header.Get("Content-Type"))
	if !ok {
		usage.emitFailure("content_type", fmt.Errorf("unsupported content type %q", response.Header.Get("Content-Type")), output)
		return
	}
	decoder, err := llmusage.NewDecoder(llmusage.Options{
		Protocol:            usage.spec.Protocol,
		Format:              format,
		MaxSSEMetadataBytes: usage.spec.MaxSSEMetadataBytes,
		MaxResultBytes:      usage.spec.MaxResultBytes,
		MaxNestingDepth:     usage.spec.MaxNestingDepth,
	})
	if err != nil {
		usage.emitFailure("decoder", err, output)
		return
	}
	usage.decoder = decoder
	usage.format = format
}

func (usage *instance) feed(data []byte, output event.Emitter) {
	if usage.decoder == nil || usage.failed || usage.finished || len(data) == 0 {
		return
	}
	results, err := usage.decoder.Feed(data)
	if err != nil {
		usage.emitFailure("feed", err, output)
		return
	}
	usage.emitResults(results, output)
}

func (usage *instance) finish(cause error, output event.Emitter) {
	if usage.decoder == nil || usage.failed || usage.finished {
		return
	}
	usage.finished = true
	results, err := usage.decoder.Finish()
	if err != nil {
		usage.emitFailure("finish", err, output)
		return
	}
	usage.emitResults(results, output)
	if usage.results == 0 && output != nil {
		data := map[string]any{
			"protocol": string(usage.spec.Protocol), "format": string(usage.format), "reason": "usage_not_reported",
		}
		if cause != nil && !errors.Is(cause, io.EOF) {
			data["stream_error"] = cause.Error()
		}
		addLabels(data, usage.spec.Labels)
		output.Emit("llm.usage.not_observed", data)
	}
}

func (usage *instance) emitResults(results []llmusage.Result, output event.Emitter) {
	if output == nil {
		return
	}
	for _, result := range results {
		usage.results++
		data := map[string]any{
			"protocol":       string(result.Protocol),
			"format":         string(usage.format),
			"response_id":    result.ResponseID,
			"model":          result.Model,
			"usage_sequence": result.Sequence,
			"usage": map[string]any{
				"input_tokens":        result.Usage.InputTokens,
				"output_tokens":       result.Usage.OutputTokens,
				"total_tokens":        result.Usage.TotalTokens,
				"cached_input_tokens": result.Usage.CachedInputTokens,
				"cache_write_tokens":  result.Usage.CacheWriteTokens,
				"reasoning_tokens":    result.Usage.ReasoningTokens,
			},
			"total_source":   string(result.TotalSource),
			"raw_usage_json": string(result.RawUsage),
		}
		addLabels(data, usage.spec.Labels)
		output.Emit("llm.usage.observed", data)
	}
}

func (usage *instance) emitFailure(stage string, err error, output event.Emitter) {
	if usage.failed {
		return
	}
	usage.failed = true
	if output == nil {
		return
	}
	data := map[string]any{"stage": stage, "error": err.Error()}
	if usage.spec.Protocol != "" {
		data["protocol"] = string(usage.spec.Protocol)
	}
	if usage.format != "" {
		data["format"] = string(usage.format)
	}
	addLabels(data, usage.spec.Labels)
	output.Emit("llm.usage.failed", data)
}

func encodeSSEData(event lifecycle.SSEData) []byte {
	var encoded bytes.Buffer
	if event.Event != "" {
		fmt.Fprintf(&encoded, "event: %s\n", event.Event)
	}
	if event.ID != "" {
		fmt.Fprintf(&encoded, "id: %s\n", event.ID)
	}
	if event.RetryMillis != nil {
		fmt.Fprintf(&encoded, "retry: %d\n", *event.RetryMillis)
	}
	for _, line := range strings.Split(string(event.Data), "\n") {
		fmt.Fprintf(&encoded, "data: %s\n", line)
	}
	encoded.WriteByte('\n')
	return encoded.Bytes()
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
	switch spec.Protocol {
	case llmusage.ProtocolAuto, llmusage.ProtocolOpenAIResponses, llmusage.ProtocolOpenAIChatCompletions, llmusage.ProtocolAnthropicMessages, llmusage.ProtocolGoogleGenerateContent:
	default:
		return Spec{}, fmt.Errorf("unsupported protocol %q", spec.Protocol)
	}
	if len(spec.Labels) > 16 {
		return Spec{}, fmt.Errorf("too many labels")
	}
	for name, value := range spec.Labels {
		if name == "" || name != strings.TrimSpace(name) || len(name) > 64 || value == "" || value != strings.TrimSpace(value) || len(value) > 256 || strings.ContainsAny(name+value, "\r\n\x00") {
			return Spec{}, fmt.Errorf("invalid label")
		}
	}
	if spec.MaxSSEMetadataBytes < 0 || spec.MaxSSEMetadataBytes > 1<<20 {
		return Spec{}, fmt.Errorf("max-sse-metadata-bytes must be between 0 and %d", 1<<20)
	}
	if spec.MaxResultBytes < 0 || spec.MaxResultBytes > 16<<20 {
		return Spec{}, fmt.Errorf("max-result-bytes must be between 0 and %d", 16<<20)
	}
	if spec.MaxNestingDepth < 0 || spec.MaxNestingDepth > 256 {
		return Spec{}, fmt.Errorf("max-nesting-depth must be between 0 and 256")
	}
	return spec, nil
}

func responseFormat(rawContentType string) (llmusage.Format, bool) {
	mediaType, _, err := mime.ParseMediaType(rawContentType)
	if err != nil {
		return "", false
	}
	mediaType = strings.ToLower(mediaType)
	switch {
	case mediaType == "text/event-stream":
		return llmusage.FormatSSE, true
	case mediaType == "application/json", strings.HasPrefix(mediaType, "application/") && strings.HasSuffix(mediaType, "+json"):
		return llmusage.FormatJSON, true
	default:
		return "", false
	}
}

func addLabels(data map[string]any, labels map[string]string) {
	if len(labels) == 0 {
		return
	}
	copyLabels := make(map[string]string, len(labels))
	for name, value := range labels {
		copyLabels[name] = value
	}
	data["labels"] = copyLabels
}
