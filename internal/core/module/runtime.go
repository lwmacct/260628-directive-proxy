package module

import (
	"strings"
	"sync/atomic"
	"time"
)

type Runtime struct {
	registry *Registry
	emission EmissionProvider
	health   map[string]*healthState
	closed   atomic.Bool
}

type Run struct {
	runtime       *Runtime
	traceID       string
	emission      EmissionSession
	eventSequence atomic.Uint64
	closed        atomic.Bool
}

type HealthStatus struct {
	Status        string    `json:"status"`
	LastFailureAt time.Time `json:"last_failure_at,omitempty"`
}

type HealthSnapshot struct {
	Status  string                  `json:"status"`
	Modules map[string]HealthStatus `json:"modules"`
}

type HealthProvider interface {
	ModuleHealth() HealthSnapshot
}

type healthState struct {
	failed       atomic.Bool
	lastFailNano atomic.Int64
}

type discardEmissionSession struct{}
type discardEmitter struct{}

func NewRuntime(definitions []Definition, emission EmissionProvider) (*Runtime, error) {
	registry, err := NewRegistry(definitions...)
	if err != nil {
		return nil, err
	}
	runtime := &Runtime{registry: registry, emission: emission, health: make(map[string]*healthState)}
	for _, name := range registry.Names() {
		runtime.health[name] = &healthState{}
	}
	return runtime, nil
}

func (runtime *Runtime) Compile(lifetime Lifetime, specs []Spec) ([]Compiled, error) {
	if runtime == nil || runtime.closed.Load() {
		return nil, nil
	}
	return runtime.registry.Compile(lifetime, specs)
}

func (runtime *Runtime) StartRun(traceID string) *Run {
	if runtime == nil || runtime.closed.Load() || strings.TrimSpace(traceID) == "" {
		return nil
	}
	emission := EmissionSession(discardEmissionSession{})
	if runtime.emission != nil {
		if opened := runtime.emission.Open(traceID); opened != nil {
			emission = opened
		}
	}
	return &Run{runtime: runtime, traceID: traceID, emission: emission}
}

func (runtime *Runtime) ModuleHealth() HealthSnapshot {
	if runtime == nil {
		return HealthSnapshot{Status: "unavailable", Modules: map[string]HealthStatus{}}
	}
	result := HealthSnapshot{Status: "ok", Modules: make(map[string]HealthStatus, len(runtime.health))}
	for name, state := range runtime.health {
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
	return result
}

func (runtime *Runtime) Close() {
	if runtime != nil {
		runtime.closed.Store(true)
	}
}

func (run *Run) OpenScope(ctx OpenContext, compiled []Compiled) (*Scope, error) {
	if run == nil || run.closed.Load() {
		return nil, nil
	}
	ctx.TraceID = run.traceID
	return OpenScope(ctx, compiled, run)
}

func (run *Run) Emitter(producer string, attempt int) Emitter {
	if run == nil || run.closed.Load() || run.emission == nil {
		return discardEmitter{}
	}
	return run.emission.Emitter(producer, attempt)
}

func (run *Run) NextEventSequence() uint64 {
	if run == nil || run.closed.Load() {
		return 0
	}
	return run.eventSequence.Add(1)
}

func (run *Run) ModuleFailed(name string) {
	if run == nil || run.runtime == nil {
		return
	}
	state := run.runtime.health[name]
	if state == nil {
		return
	}
	state.failed.Store(true)
	state.lastFailNano.Store(time.Now().UTC().UnixNano())
}

func (run *Run) Close() {
	if run == nil || !run.closed.CompareAndSwap(false, true) {
		return
	}
	if run.emission != nil {
		run.emission.Close()
	}
}

func (discardEmissionSession) Emitter(string, int) Emitter { return discardEmitter{} }
func (discardEmissionSession) Close()                      {}

func (discardEmitter) Emit(string, map[string]any) bool { return false }
func (discardEmitter) EmitBorrowed(string, map[string]any) bool {
	return false
}
func (discardEmitter) EmitOwned(_ string, _ map[string]any, release func()) bool {
	if release != nil {
		release()
	}
	return false
}
