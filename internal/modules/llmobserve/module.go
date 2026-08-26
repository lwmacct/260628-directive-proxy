package llmobserve

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"mime"
	"strings"
	"time"

	"github.com/lwmacct/260826-go-pkg-llmobserve/pkg/llmobserve"

	"github.com/lwmacct/260628-directive-proxy/internal/core/event"
	"github.com/lwmacct/260628-directive-proxy/internal/core/lifecycle"
	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
)

const Name = "llmobserve"

const (
	TopicMilestone = "llm.response.milestone"
	TopicUsage     = "llm.response.usage"
	TopicObserved  = "llm.response.observed"
	TopicFailed    = "llm.response.failed"
)

type Observation string

const (
	ObserveUsage       Observation = "usage"
	ObservePerformance Observation = "performance"
)

type Limits struct {
	MaxSSEMetadataBytes int `json:"max-sse-metadata-bytes,omitempty"`
	MaxRetainedBytes    int `json:"max-retained-bytes,omitempty"`
	MaxNestingDepth     int `json:"max-nesting-depth,omitempty"`
	MaxObjectMembers    int `json:"max-object-members,omitempty"`
	MaxKeyBytes         int `json:"max-key-bytes,omitempty"`
}

type Spec struct {
	Protocol llmobserve.Protocol `json:"protocol"`
	Observe  []Observation       `json:"observe"`
	Limits   Limits              `json:"limits,omitempty"`
}

type Module struct{}

type binding struct{ spec Spec }

type instance struct {
	spec         Spec
	requestAt    time.Time
	lastAt       time.Time
	format       llmobserve.Format
	decoder      *llmobserve.Decoder
	started      bool
	finished     bool
	failed       bool
	usageEmitted int
}

func New() *Module { return &Module{} }

func (*Module) Name() string { return Name }

func (*Module) Lifetime() module.Lifetime { return module.LifetimeRoundTrip }

func (*Module) CompileProgram(raw jsontext.Value) (module.Binding, error) {
	spec, err := decodeSpec(raw)
	if err != nil {
		return nil, err
	}
	return binding{spec: spec}, nil
}

func (binding binding) Open(module.OpenContext) (module.Instance, error) {
	return &instance{spec: binding.spec}, nil
}

func (observer *instance) Bind(registrar module.Registrar) {
	async := module.AsyncPolicy(module.OverflowBlock)
	registrar.OnUpstreamStarted(async, observer.onUpstreamStarted)
	registrar.OnUpstreamResponseStarted(async, observer.onResponseStarted)
	registrar.OnUpstreamBodyChunk(async, observer.onBodyChunk)
	registrar.OnUpstreamBodyEnded(async, observer.onBodyEnded)
}

func (observer *instance) Finish(ctx module.FinishContext) error {
	if observer.decoder != nil && !observer.finished {
		at := observer.lastAt
		if at.IsZero() {
			at = ctx.ObservedAt
		}
		observer.finish(at, io.ErrUnexpectedEOF, ctx.Emitter)
	}
	return nil
}

func (observer *instance) onUpstreamStarted(ctx module.Context, _ lifecycle.UpstreamStarted) error {
	observer.requestAt = ctx.ObservedAt
	observer.lastAt = ctx.ObservedAt
	return nil
}

func (observer *instance) onResponseStarted(ctx module.Context, response lifecycle.ResponseStarted) error {
	observer.lastAt = ctx.ObservedAt
	observer.start(ctx.ObservedAt, response, ctx.Emitter)
	return nil
}

func (observer *instance) onBodyChunk(ctx module.Context, chunk lifecycle.BodyChunk) error {
	observer.lastAt = ctx.ObservedAt
	observer.feed(ctx.ObservedAt, chunk.Data, ctx.Emitter)
	return nil
}

func (observer *instance) onBodyEnded(ctx module.Context, ended lifecycle.BodyEnded) error {
	observer.lastAt = ctx.ObservedAt
	observer.finish(ctx.ObservedAt, ended.Cause, ctx.Emitter)
	return nil
}

func (observer *instance) start(at time.Time, response lifecycle.ResponseStarted, output event.Emitter) {
	if observer.started || observer.finished || observer.failed || response.StatusCode < 200 || response.StatusCode >= 300 {
		return
	}
	contentEncoding := strings.TrimSpace(response.Header.Get("Content-Encoding"))
	if contentEncoding != "" && !strings.EqualFold(contentEncoding, "identity") {
		observer.emitFailure("content_encoding", fmt.Errorf("unsupported content encoding %q", contentEncoding), output)
		return
	}
	format, ok := responseFormat(response.Header.Get("Content-Type"))
	if !ok {
		observer.emitFailure("content_type", fmt.Errorf("unsupported content type %q", response.Header.Get("Content-Type")), output)
		return
	}
	observeUsage, observePerformance := observationFlags(observer.spec.Observe)
	decoder, err := llmobserve.NewDecoder(llmobserve.Options{
		Protocol: observer.spec.Protocol, Format: format,
		ObserveUsage: observeUsage, ObservePerformance: observePerformance,
		RequestStartedAt: observer.requestAt,
		Limits: llmobserve.Limits{
			MaxSSEMetadataBytes: observer.spec.Limits.MaxSSEMetadataBytes,
			MaxRetainedBytes:    observer.spec.Limits.MaxRetainedBytes,
			MaxNestingDepth:     observer.spec.Limits.MaxNestingDepth,
			MaxObjectMembers:    observer.spec.Limits.MaxObjectMembers,
			MaxKeyBytes:         observer.spec.Limits.MaxKeyBytes,
		},
	})
	if err != nil {
		observer.emitFailure("decoder", err, output)
		return
	}
	if observePerformance {
		if err := decoder.ResponseHeadersAt(at); err != nil {
			observer.emitFailure("headers", err, output)
			return
		}
	}
	observer.decoder = decoder
	observer.format = format
	observer.started = true
}

func (observer *instance) feed(at time.Time, data []byte, output event.Emitter) {
	if observer.decoder == nil || observer.finished || observer.failed || len(data) == 0 {
		return
	}
	updates, err := observer.decoder.FeedAt(at, data)
	observer.emitUpdates(updates, output)
	if err != nil {
		observer.emitFailure("feed", err, output)
	}
}

func (observer *instance) finish(at time.Time, cause error, output event.Emitter) {
	if observer.decoder == nil || observer.finished || observer.failed {
		return
	}
	observer.finished = true
	outcome := llmobserve.OutcomeCompleted
	if cause != nil && !errors.Is(cause, io.EOF) {
		outcome = llmobserve.OutcomeInterrupted
		if errors.Is(cause, context.Canceled) {
			outcome = llmobserve.OutcomeCanceled
		}
	}
	result, err := observer.decoder.FinishAt(llmobserve.Completion{At: at, Outcome: outcome})
	if err != nil {
		observer.emitFailure("finish", err, output)
		return
	}
	if output == nil {
		return
	}
	for index := observer.usageEmitted; index < len(result.Usage); index++ {
		output.Emit(TopicUsage, usageData(result.Usage[index]))
		observer.usageEmitted++
	}
	usage := make([]map[string]any, len(result.Usage))
	for index, snapshot := range result.Usage {
		usage[index] = usageData(snapshot)
	}
	output.Emit(TopicObserved, map[string]any{
		"protocol": string(result.Protocol), "format": string(result.Format),
		"outcome": string(result.Outcome), "terminal": string(result.Terminal),
		"usage": usage, "milestones": result.Milestones, "metrics": result.Metrics,
	})
}

func (observer *instance) emitUpdates(updates []llmobserve.Update, output event.Emitter) {
	if output == nil {
		return
	}
	for _, update := range updates {
		if update.Kind == llmobserve.UpdateUsage && update.Usage != nil {
			output.Emit(TopicUsage, usageData(*update.Usage))
			observer.usageEmitted++
			continue
		}
		output.Emit(TopicMilestone, map[string]any{
			"kind": string(update.Kind), "at": update.At.Format(time.RFC3339Nano),
			"sequence": update.Sequence, "output_kind": string(update.OutputKind),
			"precision": string(update.Precision),
		})
	}
}

func (observer *instance) emitFailure(stage string, err error, output event.Emitter) {
	if observer.failed {
		return
	}
	observer.failed = true
	if output == nil {
		return
	}
	data := map[string]any{"stage": stage, "error": err.Error(), "protocol": string(observer.spec.Protocol)}
	if observer.format != "" {
		data["format"] = string(observer.format)
	}
	output.Emit(TopicFailed, data)
}

func usageData(snapshot llmobserve.UsageSnapshot) map[string]any {
	return map[string]any{
		"protocol": string(snapshot.Protocol), "response_id": snapshot.ResponseID, "model": snapshot.Model,
		"sequence": snapshot.Sequence,
		"usage": map[string]any{
			"input_tokens": snapshot.Usage.InputTokens, "output_tokens": snapshot.Usage.OutputTokens,
			"total_tokens": snapshot.Usage.TotalTokens, "cached_input_tokens": snapshot.Usage.CachedInputTokens,
			"cache_write_tokens": snapshot.Usage.CacheWriteTokens, "reasoning_tokens": snapshot.Usage.ReasoningTokens,
		},
		"total_source": string(snapshot.TotalSource), "raw_usage_json": string(snapshot.RawUsage),
	}
}

func decodeSpec(raw jsontext.Value) (Spec, error) {
	var spec Spec
	if err := json.Unmarshal(raw, &spec, json.RejectUnknownMembers(true)); err != nil {
		return Spec{}, err
	}
	switch spec.Protocol {
	case llmobserve.ProtocolAuto, llmobserve.ProtocolOpenAIResponses, llmobserve.ProtocolOpenAIChatCompletions,
		llmobserve.ProtocolAnthropicMessages, llmobserve.ProtocolGoogleGenerateContent:
	default:
		return Spec{}, fmt.Errorf("unsupported protocol %q", spec.Protocol)
	}
	if len(spec.Observe) == 0 {
		return Spec{}, errors.New("observe must contain usage and/or performance")
	}
	seen := make(map[Observation]struct{}, len(spec.Observe))
	for _, observation := range spec.Observe {
		if observation != ObserveUsage && observation != ObservePerformance {
			return Spec{}, fmt.Errorf("unsupported observation %q", observation)
		}
		if _, duplicate := seen[observation]; duplicate {
			return Spec{}, fmt.Errorf("duplicate observation %q", observation)
		}
		seen[observation] = struct{}{}
	}
	limits := spec.Limits
	if limits.MaxSSEMetadataBytes < 0 || limits.MaxSSEMetadataBytes > 1<<20 {
		return Spec{}, fmt.Errorf("max-sse-metadata-bytes must be between 0 and %d", 1<<20)
	}
	if limits.MaxRetainedBytes < 0 || limits.MaxRetainedBytes > 16<<20 {
		return Spec{}, fmt.Errorf("max-retained-bytes must be between 0 and %d", 16<<20)
	}
	if limits.MaxNestingDepth < 0 || limits.MaxNestingDepth > 256 {
		return Spec{}, errors.New("max-nesting-depth must be between 0 and 256")
	}
	if limits.MaxObjectMembers < 0 || limits.MaxObjectMembers > 1<<16 {
		return Spec{}, fmt.Errorf("max-object-members must be between 0 and %d", 1<<16)
	}
	if limits.MaxKeyBytes < 0 || limits.MaxKeyBytes > 1<<20 {
		return Spec{}, fmt.Errorf("max-key-bytes must be between 0 and %d", 1<<20)
	}
	return spec, nil
}

func observationFlags(observations []Observation) (usage, performance bool) {
	for _, observation := range observations {
		usage = usage || observation == ObserveUsage
		performance = performance || observation == ObservePerformance
	}
	return usage, performance
}

func responseFormat(raw string) (llmobserve.Format, bool) {
	mediaType, _, err := mime.ParseMediaType(raw)
	if err != nil {
		return "", false
	}
	mediaType = strings.ToLower(mediaType)
	if mediaType == "text/event-stream" {
		return llmobserve.FormatSSE, true
	}
	if mediaType == "application/json" || strings.HasPrefix(mediaType, "application/") && strings.HasSuffix(mediaType, "+json") {
		return llmobserve.FormatJSON, true
	}
	return "", false
}
