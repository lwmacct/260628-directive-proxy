package program

import (
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/event"
	"github.com/lwmacct/260628-directive-proxy/internal/core/metadata"
	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
)

var (
	ErrRuntimeClosed = errors.New("program runtime is closed")
	ErrRunClosed     = errors.New("program run is closed")
)

type Runtime struct {
	registry *registry
	emission event.Provider
	health   map[string]*healthState
	closed   atomic.Bool
}

type Run struct {
	runtime       *Runtime
	executable    *Executable
	traceID       string
	metadata      metadata.Set
	emission      event.Session
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

type discardSession struct{}
type discardEmitter struct{}

func NewRuntime(catalog *module.Catalog, emission event.Provider) (*Runtime, error) {
	registry, err := newRegistry(catalog)
	if err != nil {
		return nil, err
	}
	runtime := &Runtime{registry: registry, emission: emission, health: make(map[string]*healthState)}
	for _, name := range registry.names() {
		runtime.health[name] = &healthState{}
	}
	return runtime, nil
}

func (runtime *Runtime) Compile(source module.Specs) (*Executable, error) {
	if runtime == nil {
		return nil, errors.New("program runtime is unavailable")
	}
	if runtime.closed.Load() {
		return nil, ErrRuntimeClosed
	}
	exchange, roundTrip, err := runtime.registry.compile(source)
	if err != nil {
		return nil, fmt.Errorf("compile program: %w", err)
	}
	return &Executable{exchange: exchange, roundTrip: roundTrip}, nil
}

func (runtime *Runtime) StartRun(traceID string, executable *Executable, fields metadata.Set) (*Run, error) {
	if runtime == nil {
		return nil, errors.New("program runtime is unavailable")
	}
	if runtime.closed.Load() {
		return nil, ErrRuntimeClosed
	}
	if strings.TrimSpace(traceID) == "" {
		return nil, errors.New("program trace id is empty")
	}
	if executable == nil {
		return nil, errors.New("compiled program is unavailable")
	}
	if fields.TraceID() != traceID {
		return nil, errors.New("program metadata is incomplete")
	}
	emission := event.Session(discardSession{})
	if runtime.emission != nil {
		if opened := runtime.emission.Open(traceID, fields); opened != nil {
			emission = opened
		}
	}
	return &Run{runtime: runtime, executable: executable, traceID: traceID, metadata: fields, emission: emission}, nil
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

func (run *Run) OpenExchange(ctx module.OpenContext) (*Scope, error) {
	if run == nil || run.closed.Load() {
		return nil, ErrRunClosed
	}
	ctx.TraceID = run.traceID
	ctx.Metadata = run.metadata
	ctx.Lifetime = module.LifetimeExchange
	return openScope(ctx, run.executable.exchange, run)
}

func (run *Run) OpenRoundTrip(ctx module.OpenContext) (*Scope, error) {
	if run == nil || run.closed.Load() {
		return nil, ErrRunClosed
	}
	ctx.TraceID = run.traceID
	ctx.Metadata = run.metadata
	ctx.Lifetime = module.LifetimeRoundTrip
	return openScope(ctx, run.executable.roundTrip, run)
}

func (run *Run) emitter(producer string, roundTrip int) event.Emitter {
	if run == nil || run.closed.Load() || run.emission == nil {
		return discardEmitter{}
	}
	return run.emission.Emitter(producer, roundTrip)
}

func (run *Run) nextEventSequence() uint64 {
	if run == nil || run.closed.Load() {
		return 0
	}
	return run.eventSequence.Add(1)
}

func (run *Run) moduleFailed(name string) {
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

func (discardSession) Emitter(string, int) event.Emitter { return discardEmitter{} }
func (discardSession) Close()                            {}

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
