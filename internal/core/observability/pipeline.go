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
	sink         *sinkRunner
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

type sinkRunner struct {
	sink         Sink
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

func NewPipeline(ctx context.Context, plugins []Plugin, config SinkConfig) (*Pipeline, error) {
	pipeline := &Pipeline{plugins: append([]Plugin(nil), plugins...), pluginHealth: make(map[string]*pluginHealthState)}
	for _, plugin := range pipeline.plugins {
		if plugin != nil && strings.TrimSpace(plugin.Name()) != "" {
			pipeline.pluginHealth[plugin.Name()] = &pluginHealthState{}
		}
	}
	if config.Sink != nil {
		runner, err := newSinkRunner(ctx, config)
		if err != nil {
			return nil, err
		}
		pipeline.sink = runner
	}
	return pipeline, nil
}

func (p *Pipeline) StartTrace(ctx TraceContext) *Trace {
	if p == nil || p.closed.Load() {
		return nil
	}
	trace := &Trace{pipeline: p, context: ctx, pluginDown: make(map[string]bool)}
	return trace.withPlugins(p.plugins)
}

func (p *Pipeline) StartRequestTrace(ctx TraceContext) *Trace {
	if p == nil || p.closed.Load() {
		return nil
	}
	return &Trace{pipeline: p, context: ctx, pluginDown: make(map[string]bool)}
}

func (t *Trace) withPlugins(plugins []Plugin) *Trace {
	for _, plugin := range plugins {
		if plugin == nil || strings.TrimSpace(plugin.Name()) == "" {
			continue
		}
		observer := plugin.NewTrace(t.context)
		if observer != nil {
			t.plugins = append(t.plugins, tracePlugin{name: plugin.Name(), observer: observer})
		}
	}
	return t
}

func (t *Trace) ReplacePlugins(specs map[string][]byte) error {
	if t == nil || t.pipeline == nil {
		return nil
	}
	plugins, err := PluginsForSpecs(t.pipeline.plugins, specs)
	if err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, plugin := range t.plugins {
		if !t.pluginDown[plugin.name] {
			t.invokeClose(plugin)
		}
	}
	t.plugins = nil
	t.pluginDown = make(map[string]bool)
	for _, plugin := range plugins {
		if observer := plugin.NewTrace(t.context); observer != nil {
			t.plugins = append(t.plugins, tracePlugin{name: plugin.Name(), observer: observer})
		}
	}
	return nil
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
	if signal.ObservedAt.IsZero() {
		signal.ObservedAt = time.Now()
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
			}, nil)
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
			}, nil)
		}
	}()
	plugin.observer.Close(traceEmitter{trace: t, plugin: plugin.name})
}

func (e traceEmitter) Emit(topic string, attempt int, data map[string]any) {
	if e.trace == nil {
		return
	}
	e.trace.emit(e.plugin, topic, attempt, data, nil)
}

func (e traceEmitter) EmitOwned(topic string, attempt int, data map[string]any, release func()) {
	if e.trace == nil {
		if release != nil {
			release()
		}
		return
	}
	e.trace.emit(e.plugin, topic, attempt, data, release)
}

func (t *Trace) emit(plugin, topic string, attempt int, data map[string]any, release func()) {
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
		resource:      newRecordResource(release),
	}
	if t.pipeline.sink != nil {
		t.pipeline.sink.enqueue(record)
	}
	record.release()
}

func (p *Pipeline) ObservabilityHealth() HealthSnapshot {
	if p == nil {
		return HealthSnapshot{Status: "unavailable", Plugins: map[string]HealthStatus{}}
	}
	result := HealthSnapshot{Status: "ok", Plugins: make(map[string]HealthStatus, len(p.pluginHealth))}
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
	if p.sink != nil {
		result.Sink = p.sink.health()
		if result.Sink.Status != "ok" {
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
	if p.sink != nil {
		if err := p.sink.close(ctx); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func newSinkRunner(ctx context.Context, binding SinkConfig) (*sinkRunner, error) {
	if binding.Sink == nil {
		return nil, fmt.Errorf("observability sink is nil")
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
	if err := binding.Sink.Start(ctx); err != nil {
		return nil, fmt.Errorf("start observability sink: %w", err)
	}
	runner := &sinkRunner{
		sink:     binding.Sink,
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

func (r *sinkRunner) enqueue(record Record) {
	if r == nil || r.closed.Load() {
		return
	}
	size := estimateRecordBytes(record)
	if !r.reserve(size) {
		r.dropped.Add(1)
		return
	}
	record.retain()
	index := shard(record.TraceID, len(r.queues))
	select {
	case r.queues[index] <- queuedRecord{record: record, size: size}:
		r.queuedCount.Add(1)
	default:
		r.queuedBytes.Add(-size)
		r.dropped.Add(1)
		record.release()
	}
}

func (r *sinkRunner) reserve(size int64) bool {
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

func (r *sinkRunner) run(queue <-chan queuedRecord) {
	defer r.wg.Done()
	for item := range queue {
		err := r.sink.Write(context.Background(), item.record)
		item.record.release()
		r.queuedBytes.Add(-item.size)
		r.queuedCount.Add(-1)
		if err != nil {
			r.failed.Store(true)
			r.lastFailNano.Store(time.Now().UTC().UnixNano())
			slog.Error("observability sink failed", "error", err)
		} else {
			r.failed.Store(false)
		}
	}
}

func (r *sinkRunner) health() HealthStatus {
	status := r.sink.Health()
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

func (r *sinkRunner) close(ctx context.Context) error {
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
		_ = r.sink.Close(ctx)
		return ctx.Err()
	}
	return r.sink.Close(ctx)
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
