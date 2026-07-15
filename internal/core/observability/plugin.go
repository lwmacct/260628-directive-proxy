package observability

import "fmt"

type TraceContext struct {
	TraceID    string
	InstanceID string
}

type Emitter interface {
	Emit(topic string, attempt int, data map[string]any)
	EmitOwned(topic string, attempt int, data map[string]any, release func())
}

type Plugin interface {
	Name() string
	NewTrace(TraceContext) TraceObserver
}

// DirectivePlugin accepts per-attempt configuration from a directive payload.
// The directive name is intentionally independent from the internal plugin name.
type DirectivePlugin interface {
	Plugin
	DirectiveName() string
	ValidateSpec([]byte) error
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
	for name, raw := range specs {
		plugin, ok := available[name]
		if !ok {
			return nil, fmt.Errorf("observability plugin %q is not registered", name)
		}
		if err := plugin.ValidateSpec(raw); err != nil {
			return nil, fmt.Errorf("validate observability plugin %q: %w", name, err)
		}
		selected = append(selected, plugin)
	}
	return selected, nil
}

func ValidatePluginSpecs(plugins []Plugin, specs map[string][]byte) error {
	if len(specs) == 0 {
		return nil
	}
	available := make(map[string]DirectivePlugin)
	for _, plugin := range plugins {
		configurable, ok := plugin.(DirectivePlugin)
		if !ok {
			continue
		}
		available[configurable.DirectiveName()] = configurable
	}
	for name, raw := range specs {
		plugin, ok := available[name]
		if !ok {
			return fmt.Errorf("observability plugin %q is not enabled", name)
		}
		if err := plugin.ValidateSpec(raw); err != nil {
			return fmt.Errorf("validate observability plugin %q: %w", name, err)
		}
	}
	return nil
}

type TraceObserver interface {
	Observe(Signal, Emitter)
	Close(Emitter)
}

type NopTraceObserver struct{}

func (NopTraceObserver) Observe(Signal, Emitter) {}
func (NopTraceObserver) Close(Emitter)           {}
