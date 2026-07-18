package program

import (
	"fmt"
	"strings"

	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
)

type registry struct {
	definitions map[string]module.Definition
}

type compiled struct {
	id         string
	moduleName string
	binding    module.Binding
}

func newRegistry(definitions ...module.Definition) (*registry, error) {
	result := &registry{definitions: make(map[string]module.Definition, len(definitions))}
	for _, definition := range definitions {
		if definition == nil {
			continue
		}
		name := strings.TrimSpace(definition.Name())
		if name == "" {
			return nil, fmt.Errorf("module has an empty name")
		}
		if _, exists := result.definitions[name]; exists {
			return nil, fmt.Errorf("module %q is registered more than once", name)
		}
		result.definitions[name] = definition
	}
	return result, nil
}

func (r *registry) compile(kind module.ScopeKind, specs []Spec) ([]compiled, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	if r == nil {
		return nil, fmt.Errorf("module registry is unavailable")
	}
	result := make([]compiled, 0, len(specs))
	seen := make(map[string]struct{}, len(specs))
	for index, spec := range specs {
		if strings.TrimSpace(spec.ID) == "" {
			return nil, fmt.Errorf("module id at index %d is empty", index)
		}
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
		if binding.Scope() != kind {
			return nil, fmt.Errorf("module %q (%s) has scope %q, not %q", spec.Module, spec.ID, binding.Scope(), kind)
		}
		result = append(result, compiled{id: spec.ID, moduleName: spec.Module, binding: binding})
	}
	return result, nil
}

func (r *registry) names() []string {
	if r == nil {
		return nil
	}
	names := make([]string, 0, len(r.definitions))
	for name := range r.definitions {
		names = append(names, name)
	}
	return names
}
