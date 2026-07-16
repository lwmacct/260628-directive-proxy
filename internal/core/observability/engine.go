package observability

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
)

type Engine struct {
	enabled      bool
	registry     *module.Registry
	moduleHealth map[string]*moduleHealthState
	sink         *sinkRunner
	closed       atomic.Bool
}

type moduleHealthState struct {
	failed       atomic.Bool
	lastFailNano atomic.Int64
}

type Run struct {
	engine   *Engine
	traceID  string
	mu       sync.Mutex
	sequence uint64
	closed   atomic.Bool
}

type runOutputFactory struct {
	run *Run
}

type runOutput struct {
	run      *Run
	producer string
	attempt  int
}

func NewEngine(ctx context.Context, definitions []module.Definition, config SinkConfig) (*Engine, error) {
	registry, err := module.NewRegistry(definitions...)
	if err != nil {
		return nil, err
	}
	engine := &Engine{enabled: true, registry: registry, moduleHealth: make(map[string]*moduleHealthState)}
	for _, name := range registry.Names() {
		engine.moduleHealth[name] = &moduleHealthState{}
	}
	if config.Sink != nil {
		runner, err := newSinkRunner(ctx, config)
		if err != nil {
			return nil, err
		}
		engine.sink = runner
	}
	return engine, nil
}

func NewDisabledEngine() *Engine {
	return &Engine{moduleHealth: make(map[string]*moduleHealthState)}
}

func (e *Engine) Enabled() bool {
	return e != nil && e.enabled && !e.closed.Load()
}

func (e *Engine) Compile(lifetime module.Lifetime, specs []module.Spec) ([]module.Compiled, error) {
	if !e.Enabled() {
		return nil, nil
	}
	return e.registry.Compile(lifetime, specs)
}

func (e *Engine) StartRun(traceID string) *Run {
	if !e.Enabled() || strings.TrimSpace(traceID) == "" {
		return nil
	}
	return &Run{engine: e, traceID: traceID}
}

func (r *Run) OpenScope(ctx module.OpenContext, compiled []module.Compiled) (*module.Scope, error) {
	if r == nil || r.closed.Load() {
		return nil, nil
	}
	ctx.TraceID = r.traceID
	return module.OpenScope(ctx, compiled, runOutputFactory{run: r})
}

func (r *Run) Close() {
	if r != nil {
		r.closed.Store(true)
	}
}

func (f runOutputFactory) Output(producer string, attempt int) module.Output {
	return runOutput{run: f.run, producer: producer, attempt: attempt}
}

func (f runOutputFactory) ModuleFailed(producer string) {
	if f.run != nil && f.run.engine != nil {
		f.run.engine.markModuleFailure(producer)
	}
}

func (o runOutput) Emit(topic string, data map[string]any) bool {
	return o.run.emit(o.producer, topic, o.attempt, data, nil, false)
}

func (o runOutput) EmitOwned(topic string, data map[string]any, release func()) bool {
	return o.run.emit(o.producer, topic, o.attempt, data, release, false)
}

func (o runOutput) EmitBorrowed(topic string, data map[string]any) bool {
	return o.run.emit(o.producer, topic, o.attempt, data, nil, true)
}

func (r *Run) emit(producer, topic string, attempt int, data map[string]any, release func(), copyBorrowed bool) bool {
	if r == nil || r.engine == nil || r.closed.Load() {
		if release != nil {
			release()
		}
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sequence++
	now := time.Now().UTC()
	record := Record{
		SchemaVersion: SchemaVersion,
		Producer:      producer,
		Topic:         topic,
		RecordID:      fmt.Sprintf("%s:%08d", r.traceID, r.sequence),
		TraceID:       r.traceID,
		Attempt:       attempt,
		Sequence:      r.sequence,
		OccurredAt:    now.Format(time.RFC3339Nano),
		Data:          data,
		Time:          now,
		resource:      newRecordResource(release),
	}
	if r.engine.sink != nil {
		accepted := r.engine.sink.enqueue(record, copyBorrowed)
		record.release()
		return accepted
	}
	record.release()
	return false
}

func (e *Engine) ObservabilityHealth() HealthSnapshot {
	if e == nil {
		return HealthSnapshot{Status: "unavailable", Modules: map[string]HealthStatus{}, Sink: HealthStatus{Status: "unavailable"}}
	}
	if !e.enabled {
		return HealthSnapshot{Status: "disabled", Modules: map[string]HealthStatus{}, Sink: HealthStatus{Status: "disabled"}}
	}
	result := HealthSnapshot{Enabled: true, Status: "ok", Modules: make(map[string]HealthStatus, len(e.moduleHealth))}
	for name, state := range e.moduleHealth {
		status := HealthStatus{Status: "ok"}
		if state.failed.Load() {
			status.Status = "degraded"
			result.Status = "degraded"
		}
		if value := state.lastFailNano.Load(); value > 0 {
			status.LastFailureAt = time.Unix(0, value).UTC()
		}
		result.Modules[name] = status
	}
	if e.sink != nil {
		result.Sink = e.sink.health()
		if result.Sink.Status != "ok" {
			result.Status = "degraded"
		}
	} else {
		result.Sink = HealthStatus{Status: "disabled"}
	}
	return result
}

func (e *Engine) markModuleFailure(name string) {
	if e == nil || e.moduleHealth[name] == nil {
		return
	}
	e.moduleHealth[name].failed.Store(true)
	e.moduleHealth[name].lastFailNano.Store(time.Now().UTC().UnixNano())
}

func (e *Engine) Close(ctx context.Context) error {
	if e == nil || !e.closed.CompareAndSwap(false, true) || e.sink == nil {
		return nil
	}
	return e.sink.close(ctx)
}
