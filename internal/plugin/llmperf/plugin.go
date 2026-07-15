package llmperfplugin

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

	"github.com/lwmacct/260628-directive-proxy/internal/core/observability"
)

const (
	Name          = "builtin.llmperf"
	DirectiveName = "llmperf"
)

type Spec struct {
	Protocol            llmperf.Protocol  `json:"protocol"`
	Labels              map[string]string `json:"labels,omitempty"`
	MaxSSEMetadataBytes int               `json:"max-sse-metadata-bytes,omitempty"`
	MaxRetainedBytes    int               `json:"max-retained-bytes,omitempty"`
	MaxNestingDepth     int               `json:"max-nesting-depth,omitempty"`
}

type Plugin struct {
	spec Spec
}

type traceObserver struct {
	requestAt time.Time
	lastAt    time.Time
	decoder   *llmperf.Decoder
	spec      Spec
	started   bool
	finished  bool
	attempt   int
}

func New() *Plugin { return &Plugin{} }

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
	return &traceObserver{spec: p.spec}
}

func (t *traceObserver) Observe(signal observability.Signal, emitter observability.Emitter) {
	t.lastAt = signal.ObservedAt
	switch value := signal.Value.(type) {
	case observability.UpstreamStarted:
		t.requestAt = signal.ObservedAt
	case observability.UpstreamResponseStarted:
		t.start(signal.Attempt, signal.ObservedAt, value, emitter)
	case observability.UpstreamBodyChunk:
		t.feed(signal.ObservedAt, value.Data, emitter)
	case observability.UpstreamBodyEnded:
		t.finish(signal.ObservedAt, value.Cause, emitter)
	}
}

func (t *traceObserver) Close(emitter observability.Emitter) {
	if t.decoder != nil && !t.finished {
		t.finish(t.lastAt, io.ErrUnexpectedEOF, emitter)
	}
}

func (t *traceObserver) start(attempt int, at time.Time, response observability.UpstreamResponseStarted, emitter observability.Emitter) {
	if t.started || t.finished || response.StatusCode < 200 || response.StatusCode >= 300 {
		return
	}
	format, ok := responseFormat(response.Header.Get("Content-Type"))
	if !ok {
		t.emitFailure(attempt, "content_type", fmt.Errorf("unsupported content type"), emitter)
		return
	}
	protocol := t.spec.Protocol
	if protocol == llmperf.ProtocolAuto && format != llmperf.FormatSSE {
		t.emitFailure(attempt, "options", errors.New("auto protocol requires SSE"), emitter)
		return
	}
	decoder, err := llmperf.NewDecoder(llmperf.Options{
		Protocol: protocol, Format: format, RequestStartedAt: t.requestAt,
		MaxSSEMetadataBytes: t.spec.MaxSSEMetadataBytes, MaxRetainedBytes: t.spec.MaxRetainedBytes, MaxNestingDepth: t.spec.MaxNestingDepth,
	})
	if err != nil {
		t.emitFailure(attempt, "decoder", err, emitter)
		return
	}
	if err := decoder.ResponseHeadersAt(at); err != nil {
		t.emitFailure(attempt, "headers", err, emitter)
		return
	}
	t.decoder, t.attempt, t.started = decoder, attempt, true
}

func (t *traceObserver) feed(at time.Time, data []byte, emitter observability.Emitter) {
	if t.decoder == nil || t.finished {
		return
	}
	updates, err := t.decoder.FeedAt(at, data)
	if err != nil {
		t.emitFailure(t.attempt, "feed", err, emitter)
		return
	}
	for _, update := range updates {
		emitter.Emit("llm.perf."+string(update.Kind), t.attempt, map[string]any{"at": update.At.Format(time.RFC3339Nano), "sequence": update.Sequence, "output_kind": string(update.OutputKind), "precision": string(update.Precision)})
	}
}

func (t *traceObserver) finish(at time.Time, cause error, emitter observability.Emitter) {
	if t.decoder == nil || t.finished {
		return
	}
	t.finished = true
	outcome := llmperf.OutcomeCompleted
	if cause != nil && !errors.Is(cause, io.EOF) {
		outcome = llmperf.OutcomeInterrupted
	}
	result, err := t.decoder.FinishAt(llmperf.Completion{At: at, Outcome: outcome})
	if err != nil {
		t.emitFailure(t.attempt, "finish", err, emitter)
		return
	}
	data := map[string]any{"protocol": string(result.Protocol), "format": string(result.Format), "outcome": string(result.Outcome), "terminal": string(result.Terminal), "milestones": result.Milestones, "metrics": result.Metrics}
	addLabels(data, t.spec.Labels)
	emitter.Emit("llm.perf.observed", t.attempt, data)
}

func (t *traceObserver) emitFailure(attempt int, stage string, err error, emitter observability.Emitter) {
	emitter.Emit("llm.perf.failed", attempt, map[string]any{"stage": stage, "error": err.Error()})
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
	data["labels"] = labels
}
