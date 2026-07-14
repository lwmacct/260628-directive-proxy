package observability

import (
	"context"
	"fmt"
	"hash/fnv"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Pipeline struct {
	plugins      []Plugin
	pluginHealth map[string]*pluginHealthState
	outputs      []*outputRunner
	closed       atomic.Bool
}

type pluginHealthState struct {
	failed       atomic.Bool
	lastFailNano atomic.Int64
}

type tracePlugin struct {
	name     string
	observer TraceObserver
}

type Trace struct {
	pipeline   *Pipeline
	context    TraceContext
	mu         sync.Mutex
	sequence   uint64
	plugins    []tracePlugin
	closed     bool
	pluginDown map[string]bool
}

type traceEmitter struct {
	trace  *Trace
	plugin string
}

type queuedRecord struct {
	record Record
	size   int64
}

type outputRunner struct {
	output       Output
	routes       []string
	queues       []chan queuedRecord
	maxBytes     int64
	queuedBytes  atomic.Int64
	queuedCount  atomic.Int64
	dropped      atomic.Uint64
	lastFailNano atomic.Int64
	failed       atomic.Bool
	closed       atomic.Bool
	wg           sync.WaitGroup
}

func NewPipeline(ctx context.Context, plugins []Plugin, bindings []OutputBinding) (*Pipeline, error) {
	pipeline := &Pipeline{plugins: append([]Plugin(nil), plugins...), pluginHealth: make(map[string]*pluginHealthState)}
	for _, plugin := range pipeline.plugins {
		if plugin != nil && strings.TrimSpace(plugin.Name()) != "" {
			pipeline.pluginHealth[plugin.Name()] = &pluginHealthState{}
		}
	}
	for _, binding := range bindings {
		runner, err := newOutputRunner(ctx, binding)
		if err != nil {
			_ = pipeline.Close(context.Background())
			return nil, err
		}
		pipeline.outputs = append(pipeline.outputs, runner)
	}
	return pipeline, nil
}

func (p *Pipeline) StartTrace(ctx TraceContext) *Trace {
	if p == nil || p.closed.Load() {
		return nil
	}
	trace := &Trace{pipeline: p, context: ctx, pluginDown: make(map[string]bool)}
	for _, plugin := range p.plugins {
		if plugin == nil || strings.TrimSpace(plugin.Name()) == "" {
			continue
		}
		observer := plugin.NewTrace(ctx)
		if observer != nil {
			trace.plugins = append(trace.plugins, tracePlugin{name: plugin.Name(), observer: observer})
		}
	}
	return trace
}

func (p *Pipeline) ValidatePluginSpecs(specs map[string][]byte) error {
	if p == nil {
		if len(specs) == 0 {
			return nil
		}
		return fmt.Errorf("observability pipeline is unavailable")
	}
	return ValidatePluginSpecs(p.plugins, specs)
}

func (t *Trace) Observe(signal Signal) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return
	}
	for _, plugin := range t.plugins {
		if t.pluginDown[plugin.name] {
			continue
		}
		t.invoke(plugin, signal)
	}
}

func (t *Trace) Close() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return
	}
	for _, plugin := range t.plugins {
		if t.pluginDown[plugin.name] {
			continue
		}
		t.invokeClose(plugin)
	}
	t.closed = true
}

func (t *Trace) invoke(plugin tracePlugin, signal Signal) {
	defer func() {
		if recovered := recover(); recovered != nil {
			t.pluginDown[plugin.name] = true
			t.pipeline.markPluginFailure(plugin.name)
			t.emit("observability", "observability.plugin.panic", signal.Attempt, map[string]any{
				"plugin": plugin.name,
				"error":  fmt.Sprint(recovered),
			})
		}
	}()
	plugin.observer.Observe(signal, traceEmitter{trace: t, plugin: plugin.name})
}

func (t *Trace) invokeClose(plugin tracePlugin) {
	defer func() {
		if recovered := recover(); recovered != nil {
			t.pluginDown[plugin.name] = true
			t.pipeline.markPluginFailure(plugin.name)
			t.emit("observability", "observability.plugin.panic", 0, map[string]any{
				"plugin": plugin.name,
				"error":  fmt.Sprint(recovered),
			})
		}
	}()
	plugin.observer.Close(traceEmitter{trace: t, plugin: plugin.name})
}

func (e traceEmitter) Emit(topic string, attempt int, data map[string]any) {
	if e.trace == nil {
		return
	}
	e.trace.emit(e.plugin, topic, attempt, data)
}

func (t *Trace) emit(plugin, topic string, attempt int, data map[string]any) {
	t.sequence++
	now := time.Now().UTC()
	record := Record{
		SchemaVersion: SchemaVersion,
		Plugin:        plugin,
		Topic:         topic,
		RecordID:      fmt.Sprintf("%s:%08d", t.context.TraceID, t.sequence),
		TraceID:       t.context.TraceID,
		Attempt:       attempt,
		InstanceID:    t.context.InstanceID,
		Sequence:      t.sequence,
		OccurredAt:    now.Format(time.RFC3339Nano),
		Data:          data,
		Time:          now,
	}
	for _, output := range t.pipeline.outputs {
		output.enqueue(record)
	}
}

func (p *Pipeline) ObservabilityHealth() HealthSnapshot {
	if p == nil {
		return HealthSnapshot{Status: "unavailable", Plugins: map[string]HealthStatus{}, Outputs: map[string]HealthStatus{}}
	}
	result := HealthSnapshot{Status: "ok", Plugins: make(map[string]HealthStatus, len(p.pluginHealth)), Outputs: make(map[string]HealthStatus, len(p.outputs))}
	for name, state := range p.pluginHealth {
		status := HealthStatus{Status: "ok"}
		if state.failed.Load() {
			status.Status = "degraded"
			result.Status = "degraded"
		}
		if value := state.lastFailNano.Load(); value > 0 {
			status.LastFailureAt = time.Unix(0, value).UTC()
		}
		result.Plugins[name] = status
	}
	for _, output := range p.outputs {
		status := output.health()
		result.Outputs[output.output.Name()] = status
		if status.Status != "ok" {
			result.Status = "degraded"
		}
	}
	return result
}

func (p *Pipeline) markPluginFailure(name string) {
	if p == nil {
		return
	}
	state := p.pluginHealth[name]
	if state == nil {
		return
	}
	state.failed.Store(true)
	state.lastFailNano.Store(time.Now().UTC().UnixNano())
}

func (p *Pipeline) Close(ctx context.Context) error {
	if p == nil || !p.closed.CompareAndSwap(false, true) {
		return nil
	}
	var first error
	for _, output := range p.outputs {
		if err := output.close(ctx); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func newOutputRunner(ctx context.Context, binding OutputBinding) (*outputRunner, error) {
	if binding.Output == nil {
		return nil, fmt.Errorf("observability output is nil")
	}
	if binding.Workers <= 0 {
		binding.Workers = 1
	}
	if binding.QueueCapacity <= 0 {
		binding.QueueCapacity = 1024
	}
	if binding.QueueMaxBytes <= 0 {
		binding.QueueMaxBytes = 64 << 20
	}
	if len(binding.Routes) == 0 {
		binding.Routes = []string{"**"}
	}
	if err := binding.Output.Start(ctx); err != nil {
		return nil, fmt.Errorf("start observability output %q: %w", binding.Output.Name(), err)
	}
	runner := &outputRunner{
		output:   binding.Output,
		routes:   append([]string(nil), binding.Routes...),
		maxBytes: binding.QueueMaxBytes,
		queues:   make([]chan queuedRecord, binding.Workers),
	}
	for index := range runner.queues {
		runner.queues[index] = make(chan queuedRecord, binding.QueueCapacity)
		runner.wg.Add(1)
		go runner.run(runner.queues[index])
	}
	return runner, nil
}

func (r *outputRunner) enqueue(record Record) {
	if r == nil || r.closed.Load() || !matchesAny(record.Topic, r.routes) {
		return
	}
	size := estimateRecordBytes(record)
	if !r.reserve(size) {
		r.dropped.Add(1)
		return
	}
	index := shard(record.TraceID, len(r.queues))
	select {
	case r.queues[index] <- queuedRecord{record: record, size: size}:
		r.queuedCount.Add(1)
	default:
		r.queuedBytes.Add(-size)
		r.dropped.Add(1)
	}
}

func (r *outputRunner) reserve(size int64) bool {
	for {
		current := r.queuedBytes.Load()
		if current+size > r.maxBytes {
			return false
		}
		if r.queuedBytes.CompareAndSwap(current, current+size) {
			return true
		}
	}
}

func (r *outputRunner) run(queue <-chan queuedRecord) {
	defer r.wg.Done()
	for item := range queue {
		err := r.output.Write(context.Background(), item.record)
		r.queuedBytes.Add(-item.size)
		r.queuedCount.Add(-1)
		if err != nil {
			r.failed.Store(true)
			r.lastFailNano.Store(time.Now().UTC().UnixNano())
			slog.Error("observability output failed", "output", r.output.Name(), "error", err)
		} else {
			r.failed.Store(false)
		}
	}
}

func (r *outputRunner) health() HealthStatus {
	status := r.output.Health()
	if status.Status == "" {
		status.Status = "ok"
	}
	status.QueuedRecords = r.queuedCount.Load()
	status.QueuedBytes = r.queuedBytes.Load()
	status.DroppedRecords = r.dropped.Load()
	if r.failed.Load() || status.DroppedRecords > 0 {
		status.Status = "degraded"
	}
	if value := r.lastFailNano.Load(); value > 0 {
		last := time.Unix(0, value).UTC()
		if status.LastFailureAt.Before(last) {
			status.LastFailureAt = last
		}
	}
	return status
}

func (r *outputRunner) close(ctx context.Context) error {
	if r == nil || !r.closed.CompareAndSwap(false, true) {
		return nil
	}
	for _, queue := range r.queues {
		close(queue)
	}
	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		_ = r.output.Close(ctx)
		return ctx.Err()
	}
	return r.output.Close(ctx)
}

func matchesAny(topic string, routes []string) bool {
	for _, route := range routes {
		route = strings.TrimSpace(route)
		switch {
		case route == "*" || route == "**":
			return true
		case strings.HasSuffix(route, "**") && strings.HasPrefix(topic, strings.TrimSuffix(route, "**")):
			return true
		case topic == route:
			return true
		}
	}
	return false
}

func shard(traceID string, count int) int {
	if count <= 1 {
		return 0
	}
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(traceID))
	return int(hasher.Sum32() % uint32(count))
}

func estimateRecordBytes(record Record) int64 {
	size := int64(len(record.SchemaVersion) + len(record.Plugin) + len(record.Topic) + len(record.RecordID) + len(record.TraceID) + len(record.InstanceID) + len(record.OccurredAt) + 64)
	return size + estimateValueBytes(record.Data)
}

func estimateValueBytes(value any) int64 {
	switch typed := value.(type) {
	case nil:
		return 0
	case string:
		return int64(len(typed))
	case []byte:
		return int64(len(typed))
	case []string:
		var total int64
		for _, item := range typed {
			total += int64(len(item))
		}
		return total
	case map[string]any:
		var total int64
		for key, item := range typed {
			total += int64(len(key)) + estimateValueBytes(item)
		}
		return total
	case map[string][]string:
		var total int64
		for key, items := range typed {
			total += int64(len(key)) + estimateValueBytes(items)
		}
		return total
	case map[string]string:
		var total int64
		for key, item := range typed {
			total += int64(len(key) + len(item))
		}
		return total
	case []any:
		var total int64
		for _, item := range typed {
			total += estimateValueBytes(item)
		}
		return total
	default:
		return 32
	}
}
