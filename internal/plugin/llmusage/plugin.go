package llmusageplugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"strings"

	"github.com/lwmacct/260714-go-pkg-llmusage/pkg/llmusage"

	"github.com/lwmacct/260628-directive-proxy/internal/core/observability"
)

const (
	Name          = "builtin.llmusage"
	DirectiveName = "llmusage"
)

type Config struct {
	Name                string
	MaxSSEMetadataBytes int
	MaxResultBytes      int
	MaxNestingDepth     int
}

type Spec struct {
	Protocol llmusage.Protocol `json:"protocol"`
	Labels   map[string]string `json:"labels,omitempty"`
}

type Plugin struct {
	name   string
	config Config
}

type traceObserver struct {
	config   Config
	decoder  *llmusage.Decoder
	spec     Spec
	format   llmusage.Format
	attempt  int
	results  int
	failed   bool
	finished bool
}

func New(config Config) *Plugin {
	name := strings.TrimSpace(config.Name)
	if name == "" {
		name = Name
	}
	return &Plugin{name: name, config: config}
}

func (p *Plugin) Name() string {
	if p == nil || p.name == "" {
		return Name
	}
	return p.name
}
func (*Plugin) DirectiveName() string { return DirectiveName }

func (*Plugin) ValidateSpec(raw []byte) error {
	_, err := decodeSpec(raw)
	return err
}

func (p *Plugin) NewTrace(observability.TraceContext) observability.TraceObserver {
	if p == nil {
		return nil
	}
	return &traceObserver{config: p.config}
}

func (t *traceObserver) Observe(signal observability.Signal, emitter observability.Emitter) {
	switch value := signal.Value.(type) {
	case observability.UpstreamResponseStarted:
		t.start(signal.Attempt, value, emitter)
	case observability.UpstreamBodyChunk:
		t.feed(value.Data, emitter)
	case observability.UpstreamBodyEnded:
		t.finish(value.Cause, emitter)
	}
}

func (t *traceObserver) Close(emitter observability.Emitter) {
	t.finish(io.ErrUnexpectedEOF, emitter)
}

func (t *traceObserver) start(attempt int, response observability.UpstreamResponseStarted, emitter observability.Emitter) {
	if t.decoder != nil || t.finished || response.StatusCode < 200 || response.StatusCode >= 300 {
		return
	}
	raw, enabled := response.PluginSpecs[DirectiveName]
	if !enabled {
		return
	}
	spec, err := decodeSpec(raw)
	if err != nil {
		t.emitFailure(attempt, "spec", err, emitter)
		return
	}
	t.spec = spec
	t.attempt = attempt
	contentEncoding := strings.TrimSpace(response.Header.Get("Content-Encoding"))
	if contentEncoding != "" && !strings.EqualFold(contentEncoding, "identity") {
		t.emitFailure(attempt, "content_encoding", fmt.Errorf("unsupported content encoding %q", contentEncoding), emitter)
		return
	}
	format, ok := responseFormat(response.Header.Get("Content-Type"))
	if !ok {
		t.emitFailure(attempt, "content_type", fmt.Errorf("unsupported content type %q", response.Header.Get("Content-Type")), emitter)
		return
	}
	decoder, err := llmusage.NewDecoder(llmusage.Options{
		Protocol:            spec.Protocol,
		Format:              format,
		MaxSSEMetadataBytes: t.config.MaxSSEMetadataBytes,
		MaxResultBytes:      t.config.MaxResultBytes,
		MaxNestingDepth:     t.config.MaxNestingDepth,
	})
	if err != nil {
		t.emitFailure(attempt, "decoder", err, emitter)
		return
	}
	t.decoder = decoder
	t.format = format
}

func (t *traceObserver) feed(data []byte, emitter observability.Emitter) {
	if t.decoder == nil || t.failed || t.finished || len(data) == 0 {
		return
	}
	results, err := t.decoder.Feed(data)
	if err != nil {
		t.emitFailure(t.attempt, "feed", err, emitter)
		return
	}
	t.emitResults(results, emitter)
}

func (t *traceObserver) finish(cause error, emitter observability.Emitter) {
	if t.decoder == nil || t.failed || t.finished {
		return
	}
	t.finished = true
	results, err := t.decoder.Finish()
	if err != nil {
		t.emitFailure(t.attempt, "finish", err, emitter)
		return
	}
	t.emitResults(results, emitter)
	if t.results == 0 {
		data := map[string]any{
			"protocol": string(t.spec.Protocol), "format": string(t.format), "reason": "usage_not_reported",
		}
		if cause != nil && !errors.Is(cause, io.EOF) {
			data["stream_error"] = cause.Error()
		}
		addLabels(data, t.spec.Labels)
		emitter.Emit("llm.usage.not_observed", t.attempt, data)
	}
}

func (t *traceObserver) emitResults(results []llmusage.Result, emitter observability.Emitter) {
	for _, result := range results {
		t.results++
		data := map[string]any{
			"protocol":       string(result.Protocol),
			"format":         string(t.format),
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
		addLabels(data, t.spec.Labels)
		emitter.Emit("llm.usage.observed", t.attempt, data)
	}
}

func (t *traceObserver) emitFailure(attempt int, stage string, err error, emitter observability.Emitter) {
	if t.failed {
		return
	}
	t.failed = true
	data := map[string]any{"stage": stage, "error": err.Error()}
	if t.spec.Protocol != "" {
		data["protocol"] = string(t.spec.Protocol)
	}
	if t.format != "" {
		data["format"] = string(t.format)
	}
	addLabels(data, t.spec.Labels)
	emitter.Emit("llm.usage.failed", attempt, data)
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
