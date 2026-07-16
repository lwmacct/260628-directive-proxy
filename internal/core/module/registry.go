package module

import (
	"fmt"
	"strings"
)

type Registry struct {
	definitions map[string]Definition
}

type Compiled struct {
	Spec    Spec
	Binding Binding
}

func NewRegistry(definitions ...Definition) (*Registry, error) {
	registry := &Registry{definitions: make(map[string]Definition, len(definitions))}
	for _, definition := range definitions {
		if definition == nil {
			continue
		}
		name := strings.TrimSpace(definition.Name())
		if name == "" {
			return nil, fmt.Errorf("module has an empty name")
		}
		if _, exists := registry.definitions[name]; exists {
			return nil, fmt.Errorf("module %q is registered more than once", name)
		}
		registry.definitions[name] = definition
	}
	return registry, nil
}

func (r *Registry) Compile(lifetime Lifetime, specs []Spec) ([]Compiled, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	if r == nil {
		return nil, fmt.Errorf("module registry is unavailable")
	}
	compiled := make([]Compiled, 0, len(specs))
	seen := make(map[string]struct{}, len(specs))
	for index, spec := range specs {
		if _, exists := seen[spec.ID]; exists {
			return nil, fmt.Errorf("module id %q is repeated", spec.ID)
		}
		seen[spec.ID] = struct{}{}
		definition := r.definitions[spec.Module]
		if definition == nil {
			return nil, fmt.Errorf("module %q at index %d is not registered", spec.Module, index)
		}
		binding, err := definition.Compile(spec.Config)
		if err != nil {
			return nil, fmt.Errorf("compile module %q (%s): %w", spec.Module, spec.ID, err)
		}
		if binding == nil {
			return nil, fmt.Errorf("compile module %q (%s): nil binding", spec.Module, spec.ID)
		}
		if binding.Lifetime() != lifetime {
			return nil, fmt.Errorf("module %q (%s) has lifetime %q, not %q", spec.Module, spec.ID, binding.Lifetime(), lifetime)
		}
		compiled = append(compiled, Compiled{Spec: spec, Binding: binding})
	}
	return compiled, nil
}

func (r *Registry) Names() []string {
	if r == nil {
		return nil
	}
	names := make([]string, 0, len(r.definitions))
	for name := range r.definitions {
		names = append(names, name)
	}
	return names
}
