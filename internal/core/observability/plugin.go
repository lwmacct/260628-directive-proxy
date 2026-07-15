package observability

import (
	"fmt"
	"sort"
)

type TraceContext struct {
	TraceID string
}

type Emitter interface {
	Emit(topic string, attempt int, data map[string]any) bool
	EmitOwned(topic string, attempt int, data map[string]any, release func()) bool
	// EmitBorrowed copies byte slices only after the sink queue accepts the record.
	// Borrowed values remain owned by the caller and are valid only until return.
	EmitBorrowed(topic string, attempt int, data map[string]any) bool
}

type Plugin interface {
	Name() string
	NewTrace(TraceContext) TraceObserver
}

// DirectivePlugin creates an attempt-scoped configured plugin from a directive spec.
type DirectivePlugin interface {
	Plugin
	DirectiveName() string
	ConfigureSpec([]byte) (Plugin, error)
}

func PluginsForSpecs(plugins []Plugin, specs map[string][]byte) ([]Plugin, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	available := make(map[string]DirectivePlugin)
	for _, plugin := range plugins {
		if configurable, ok := plugin.(DirectivePlugin); ok {
			available[configurable.DirectiveName()] = configurable
		}
	}
	selected := make([]Plugin, 0, len(specs))
	names := make([]string, 0, len(specs))
	for name := range specs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		raw := specs[name]
		plugin, ok := available[name]
		if !ok {
			return nil, fmt.Errorf("observability plugin %q is not registered", name)
		}
		configured, err := plugin.ConfigureSpec(raw)
		if err != nil {
			return nil, fmt.Errorf("validate observability plugin %q: %w", name, err)
		}
		if configured == nil {
			return nil, fmt.Errorf("configure observability plugin %q: nil plugin", name)
		}
		selected = append(selected, configured)
	}
	return selected, nil
}

type TraceObserver interface {
	Observe(Signal, Emitter)
	Close(Emitter)
}

type NopTraceObserver struct{}

func (NopTraceObserver) Observe(Signal, Emitter) {}
func (NopTraceObserver) Close(Emitter)           {}
