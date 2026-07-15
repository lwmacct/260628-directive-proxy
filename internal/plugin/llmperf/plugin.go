package llmperfplugin

import (
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

type Config struct {
	Name                string
	MaxSSEMetadataBytes int
	MaxRetainedBytes    int
	MaxNestingDepth     int
}

type Spec struct {
	Protocol llmperf.Protocol  `json:"protocol"`
	Labels   map[string]string `json:"labels,omitempty"`
}

type Plugin struct {
	name   string
	config Config
}

type traceObserver struct {
	config    Config
	requestAt time.Time
	lastAt    time.Time
	decoder   *llmperf.Decoder
	spec      Spec
	started   bool
	finished  bool
	attempt   int
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
	raw, enabled := response.PluginSpecs[DirectiveName]
	if !enabled {
		return
	}
	spec, err := decodeSpec(raw)
	if err != nil {
		t.emitFailure(attempt, "spec", err, emitter)
		return
	}
	format, ok := responseFormat(response.Header.Get("Content-Type"))
	if !ok {
		t.emitFailure(attempt, "content_type", fmt.Errorf("unsupported content type"), emitter)
		return
	}
	protocol := spec.Protocol
	if protocol == llmperf.ProtocolAuto && format != llmperf.FormatSSE {
		t.emitFailure(attempt, "options", errors.New("auto protocol requires SSE"), emitter)
		return
	}
	decoder, err := llmperf.NewDecoder(llmperf.Options{Protocol: protocol, Format: format, RequestStartedAt: t.requestAt, MaxSSEMetadataBytes: t.config.MaxSSEMetadataBytes, MaxRetainedBytes: t.config.MaxRetainedBytes, MaxNestingDepth: t.config.MaxNestingDepth})
	if err != nil {
		t.emitFailure(attempt, "decoder", err, emitter)
		return
	}
	if err := decoder.ResponseHeadersAt(at); err != nil {
		t.emitFailure(attempt, "headers", err, emitter)
		return
	}
	t.spec, t.decoder, t.attempt, t.started = spec, decoder, attempt, true
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
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	var spec Spec
	if err := decoder.Decode(&spec); err != nil {
		return Spec{}, err
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
