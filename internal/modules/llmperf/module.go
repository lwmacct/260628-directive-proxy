package llmperf

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"strings"
	"time"

	"github.com/lwmacct/260714-go-pkg-llmperf/pkg/llmperf"

	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
)

const Name = "builtin.llmperf"

type Spec struct {
	Protocol            llmperf.Protocol  `json:"protocol"`
	Labels              map[string]string `json:"labels,omitempty"`
	MaxSSEMetadataBytes int               `json:"max-sse-metadata-bytes,omitempty"`
	MaxRetainedBytes    int               `json:"max-retained-bytes,omitempty"`
	MaxNestingDepth     int               `json:"max-nesting-depth,omitempty"`
}

type Module struct{}

type binding struct{ spec Spec }

type instance struct {
	requestAt time.Time
	lastAt    time.Time
	decoder   *llmperf.Decoder
	spec      Spec
	started   bool
	finished  bool
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

func (binding binding) Lifetime() module.Lifetime { return module.LifetimeAttempt }

func (binding binding) Open(module.OpenContext) (module.Instance, error) {
	return &instance{spec: binding.spec}, nil
}

func (perf *instance) Mount(binder *module.Binder) {
	async := module.AsyncPolicy(module.OverflowBlock)
	binder.OnUpstreamStarted(async, perf.onUpstreamStarted)
	binder.OnUpstreamResponseStarted(async, perf.onResponseStarted)
	binder.OnUpstreamBodyChunk(async, perf.onBodyChunk)
	binder.OnUpstreamBodyEnded(async, perf.onBodyEnded)
}

func (perf *instance) Finish(ctx module.FinishContext) error {
	if perf.decoder != nil && !perf.finished {
		perf.finish(perf.lastAt, io.ErrUnexpectedEOF, ctx.Output)
	}
	return nil
}

func (perf *instance) onUpstreamStarted(ctx module.EventContext, _ module.UpstreamStarted) error {
	perf.lastAt = ctx.ObservedAt
	perf.requestAt = ctx.ObservedAt
	return nil
}

func (perf *instance) onResponseStarted(ctx module.EventContext, response module.ResponseStarted) error {
	perf.lastAt = ctx.ObservedAt
	perf.start(ctx.ObservedAt, response, ctx.Output)
	return nil
}

func (perf *instance) onBodyChunk(ctx module.EventContext, chunk module.BodyChunk) error {
	perf.lastAt = ctx.ObservedAt
	perf.feed(ctx.ObservedAt, chunk.Data, ctx.Output)
	return nil
}

func (perf *instance) onBodyEnded(ctx module.EventContext, ended module.BodyEnded) error {
	perf.lastAt = ctx.ObservedAt
	perf.finish(ctx.ObservedAt, ended.Cause, ctx.Output)
	return nil
}

func (perf *instance) start(at time.Time, response module.ResponseStarted, output module.Output) {
	if perf.started || perf.finished || response.StatusCode < 200 || response.StatusCode >= 300 {
		return
	}
	format, ok := responseFormat(response.Header.Get("Content-Type"))
	if !ok {
		perf.emitFailure("content_type", fmt.Errorf("unsupported content type"), output)
		return
	}
	protocol := perf.spec.Protocol
	if protocol == llmperf.ProtocolAuto && format != llmperf.FormatSSE {
		perf.emitFailure("options", errors.New("auto protocol requires SSE"), output)
		return
	}
	decoder, err := llmperf.NewDecoder(llmperf.Options{
		Protocol: protocol, Format: format, RequestStartedAt: perf.requestAt,
		MaxSSEMetadataBytes: perf.spec.MaxSSEMetadataBytes,
		MaxRetainedBytes:    perf.spec.MaxRetainedBytes,
		MaxNestingDepth:     perf.spec.MaxNestingDepth,
	})
	if err != nil {
		perf.emitFailure("decoder", err, output)
		return
	}
	if err := decoder.ResponseHeadersAt(at); err != nil {
		perf.emitFailure("headers", err, output)
		return
	}
	perf.decoder, perf.started = decoder, true
}

func (perf *instance) feed(at time.Time, data []byte, output module.Output) {
	if perf.decoder == nil || perf.finished {
		return
	}
	updates, err := perf.decoder.FeedAt(at, data)
	if err != nil {
		perf.emitFailure("feed", err, output)
		return
	}
	if output == nil {
		return
	}
	for _, update := range updates {
		output.Emit("llm.perf."+string(update.Kind), map[string]any{
			"at": update.At.Format(time.RFC3339Nano), "sequence": update.Sequence,
			"output_kind": string(update.OutputKind), "precision": string(update.Precision),
		})
	}
}

func (perf *instance) finish(at time.Time, cause error, output module.Output) {
	if perf.decoder == nil || perf.finished {
		return
	}
	perf.finished = true
	outcome := llmperf.OutcomeCompleted
	if cause != nil && !errors.Is(cause, io.EOF) {
		outcome = llmperf.OutcomeInterrupted
	}
	result, err := perf.decoder.FinishAt(llmperf.Completion{At: at, Outcome: outcome})
	if err != nil {
		perf.emitFailure("finish", err, output)
		return
	}
	if output == nil {
		return
	}
	data := map[string]any{
		"protocol": string(result.Protocol), "format": string(result.Format), "outcome": string(result.Outcome),
		"terminal": string(result.Terminal), "milestones": result.Milestones, "metrics": result.Metrics,
	}
	addLabels(data, perf.spec.Labels)
	output.Emit("llm.perf.observed", data)
}

func (perf *instance) emitFailure(stage string, err error, output module.Output) {
	if output != nil {
		data := map[string]any{"stage": stage, "error": err.Error()}
		addLabels(data, perf.spec.Labels)
		output.Emit("llm.perf.failed", data)
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
		return Spec{}, errors.New("multiple JSON values")
	}
	if spec.Protocol == "" {
		return Spec{}, errors.New("protocol is required")
	}
	switch spec.Protocol {
	case llmperf.ProtocolAuto, llmperf.ProtocolOpenAIResponses, llmperf.ProtocolOpenAIChatCompletions, llmperf.ProtocolAnthropicMessages, llmperf.ProtocolGoogleGenerateContent:
	default:
		return Spec{}, fmt.Errorf("unsupported protocol %q", spec.Protocol)
	}
	if len(spec.Labels) > 16 {
		return Spec{}, errors.New("too many labels")
	}
	for name, value := range spec.Labels {
		if name == "" || name != strings.TrimSpace(name) || len(name) > 64 || value == "" || value != strings.TrimSpace(value) || len(value) > 256 || strings.ContainsAny(name+value, "\r\n\x00") {
			return Spec{}, errors.New("invalid label")
		}
	}
	if spec.MaxSSEMetadataBytes < 0 || spec.MaxSSEMetadataBytes > 1<<20 {
		return Spec{}, fmt.Errorf("max-sse-metadata-bytes must be between 0 and %d", 1<<20)
	}
	if spec.MaxRetainedBytes < 0 || spec.MaxRetainedBytes > 16<<20 {
		return Spec{}, fmt.Errorf("max-retained-bytes must be between 0 and %d", 16<<20)
	}
	if spec.MaxNestingDepth < 0 || spec.MaxNestingDepth > 256 {
		return Spec{}, errors.New("max-nesting-depth must be between 0 and 256")
	}
	return spec, nil
}

func responseFormat(raw string) (llmperf.Format, bool) {
	mediaType, _, err := mime.ParseMediaType(raw)
	if err != nil {
		return "", false
	}
	if strings.EqualFold(mediaType, "text/event-stream") {
		return llmperf.FormatSSE, true
	}
	if strings.EqualFold(mediaType, "application/json") || strings.HasSuffix(strings.ToLower(mediaType), "+json") {
		return llmperf.FormatJSON, true
	}
	return "", false
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
