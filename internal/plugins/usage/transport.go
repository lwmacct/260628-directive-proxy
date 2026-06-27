package usage

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/eventbus"
	"github.com/lwmacct/260628-llm-relay-dproxy/internal/proxyplan"
	"github.com/lwmacct/260628-llm-relay-dproxy/pkg/jsoncapture"
)

type Mode = jsoncapture.Mode

const (
	ModeInclude = jsoncapture.ModeInclude
	ModeExclude = jsoncapture.ModeExclude
)

type Options struct {
	IDGenerator eventbus.IDGenerator
	Mode        Mode
	Fields      []string
}

type Transport struct {
	base    http.RoundTripper
	sink    Sink
	idGen   eventbus.IDGenerator
	capture jsoncapture.Options
}

func NewTransport(base http.RoundTripper, sink Sink, opts Options) *Transport {
	if base == nil {
		base = http.DefaultTransport
	}
	if sink == nil {
		sink = NopSink{}
	}
	return &Transport{
		base:    base,
		sink:    sink,
		idGen:   defaultIDGenerator(opts.IDGenerator),
		capture: newCaptureOptions(opts.Mode, opts.Fields),
	}
}

func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil || resp == nil || resp.Body == nil {
		return resp, err
	}
	if !isSSEResponse(req, resp) {
		return resp, nil
	}
	resp.Body = newUsageBody(resp.Body, withoutCancel(req.Context()), t.sink, t.idGen, t.requestID(req), t.capture)
	return resp, nil
}

func withoutCancel(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return context.WithoutCancel(ctx)
}

func (t *Transport) requestID(req *http.Request) string {
	if req != nil {
		if id, ok := eventbus.RequestIDFromContext(req.Context()); ok && strings.TrimSpace(id) != "" {
			return strings.TrimSpace(id)
		}
	}
	if t.idGen != nil {
		return t.idGen.Generate()
	}
	return ""
}

func defaultIDGenerator(idGen eventbus.IDGenerator) eventbus.IDGenerator {
	if idGen != nil {
		return idGen
	}
	return eventbus.NewIDGenerator()
}

func isSSEResponse(req *http.Request, resp *http.Response) bool {
	if resp != nil && strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		return true
	}
	if req != nil && strings.Contains(strings.ToLower(req.Header.Get("Accept")), "text/event-stream") {
		return true
	}
	return false
}

type usageBody struct {
	inner  io.ReadCloser
	parser *sseParser
	once   sync.Once
}

func newUsageBody(inner io.ReadCloser, ctx context.Context, sink Sink, idGen eventbus.IDGenerator, requestID string, capture jsoncapture.Options) *usageBody {
	if sink == nil {
		sink = NopSink{}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if idGen == nil {
		idGen = eventbus.NewIDGenerator()
	}
	return &usageBody{
		inner: inner,
		parser: &sseParser{
			ctx:       ctx,
			sink:      sink,
			idGen:     idGen,
			requestID: requestID,
			labels:    labelsFromContext(ctx),
			runtime:   runtimeFromContext(ctx),
			capture:   capture,
		},
	}
}

func (b *usageBody) Read(p []byte) (int, error) {
	n, err := b.inner.Read(p)
	if n > 0 {
		b.parser.write(p[:n])
	}
	if err != nil {
		b.finish()
	}
	return n, err
}

func (b *usageBody) Close() error {
	b.finish()
	return b.inner.Close()
}

func (b *usageBody) finish() {
	b.once.Do(func() {
		b.parser.endEvent()
	})
}

const (
	lineField = iota
	lineSkipSpace
	lineValue
	lineData
	lineIgnore
)

type sseParser struct {
	ctx       context.Context
	sink      Sink
	idGen     eventbus.IDGenerator
	requestID string
	labels    map[string]any
	runtime   eventbus.Runtime
	capture   jsoncapture.Options

	state  int
	field  []byte
	value  []byte
	key    string
	skipLF bool

	eventName      string
	dataLineActive bool
	dataBuf        []byte
	parser         *usageJSONParser
}

func (p *sseParser) write(data []byte) {
	for _, b := range data {
		p.writeByte(b)
	}
}

func (p *sseParser) writeByte(b byte) {
	if p.skipLF {
		p.skipLF = false
		if b == '\n' {
			return
		}
	}
	if b == '\r' {
		p.endLine()
		p.skipLF = true
		return
	}
	if b == '\n' {
		p.endLine()
		return
	}

	switch p.state {
	case lineField:
		if b == ':' {
			p.startValue()
			return
		}
		p.field = append(p.field, b)
	case lineSkipSpace:
		if b == ' ' {
			p.state = p.nextValueState()
			return
		}
		p.state = p.nextValueState()
		p.writeByte(b)
	case lineValue:
		p.value = append(p.value, b)
	case lineData:
		p.writeDataByte(b)
	case lineIgnore:
	}
}

func (p *sseParser) startValue() {
	p.key = string(p.field)
	p.field = p.field[:0]
	switch p.key {
	case "event", "id", "retry", "data":
		p.state = lineSkipSpace
	default:
		p.state = lineIgnore
	}
}

func (p *sseParser) nextValueState() int {
	if p.key == "data" {
		if p.eventName == "response.completed" {
			if p.parser == nil {
				p.parser = newUsageJSONParser(p.capture)
			}
			if p.dataLineActive {
				p.parser.write([]byte{'\n'})
			}
			p.dataLineActive = true
			return lineData
		}
		return lineIgnore
	}
	return lineValue
}

func (p *sseParser) writeDataByte(b byte) {
	if p.parser == nil {
		return
	}
	p.dataBuf = append(p.dataBuf, b)
	if len(p.dataBuf) >= 8192 {
		p.flushData()
	}
}

func (p *sseParser) flushData() {
	if p.parser == nil || len(p.dataBuf) == 0 {
		p.dataBuf = p.dataBuf[:0]
		return
	}
	if _, err := p.parser.write(p.dataBuf); err != nil {
		slog.Debug("usage plugin failed to stream data bytes", "error", err)
	}
	p.dataBuf = p.dataBuf[:0]
}

func (p *sseParser) endLine() {
	switch p.state {
	case lineField:
		if len(p.field) == 0 {
			p.endEvent()
		}
	case lineValue:
		switch p.key {
		case "event":
			p.eventName = string(p.value)
		}
	case lineData:
		p.flushData()
	}
	p.field = p.field[:0]
	p.value = p.value[:0]
	p.key = ""
	p.state = lineField
}

func (p *sseParser) endEvent() {
	if p.parser != nil {
		p.flushData()
		result, ok, err := p.parser.close()
		if err != nil {
			slog.Debug("usage plugin failed to extract response usage", "error", err)
		} else if ok {
			_ = p.sink.Publish(p.ctx, eventbus.Event{
				EventID:   p.idGen.Generate(),
				RequestID: p.requestID,
				Type:      EventTypeUsage,
				Timestamp: time.Now(),
				Labels:    cloneLabelsMap(p.labels),
				Runtime:   eventbus.CloneRuntime(p.runtime),
				Data:      Data(result.Fields),
			})
		}
	}
	p.eventName = ""
	p.dataLineActive = false
	p.parser = nil
}

func runtimeFromContext(ctx context.Context) eventbus.Runtime {
	plan, ok := proxyplan.PlanFromContext(ctx)
	if !ok {
		return eventbus.Runtime{}
	}
	return eventbus.CloneRuntime(plan.Runtime)
}

func labelsFromContext(ctx context.Context) map[string]any {
	plan, ok := proxyplan.PlanFromContext(ctx)
	if !ok || len(plan.Labels) == 0 {
		return nil
	}
	return cloneLabelsMap(plan.Labels)
}

func cloneLabelsMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

type usageJSONParser struct {
	scanner *jsoncapture.Scanner
}

func newUsageJSONParser(capture jsoncapture.Options) *usageJSONParser {
	return &usageJSONParser{scanner: jsoncapture.NewScanner(capture)}
}

func (p *usageJSONParser) write(data []byte) (int, error) {
	if p == nil || p.scanner == nil {
		return len(data), nil
	}
	if err := p.scanner.Write(data); err != nil {
		return 0, err
	}
	return len(data), nil
}

func (p *usageJSONParser) close() (jsoncapture.Result, bool, error) {
	if p == nil || p.scanner == nil {
		return jsoncapture.Result{}, false, nil
	}
	result, err := p.scanner.Finish()
	if err != nil {
		return jsoncapture.Result{}, false, err
	}
	_, ok := result.Fields["usage"]
	return result, ok, nil
}
