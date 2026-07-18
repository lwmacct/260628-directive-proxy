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
	order      int
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

func (r *registry) compile(specs Program) ([]compiled, []compiled, error) {
	if len(specs) == 0 {
		return nil, nil, nil
	}
	if r == nil {
		return nil, nil, fmt.Errorf("module registry is unavailable")
	}
	exchange := make([]compiled, 0, len(specs))
	attempt := make([]compiled, 0, len(specs))
	seen := make(map[string]struct{}, len(specs))
	for index, spec := range specs {
		if spec.Scope != module.ScopeExchange && spec.Scope != module.ScopeAttempt {
			return nil, nil, fmt.Errorf("module scope at index %d is invalid", index)
		}
		if strings.TrimSpace(spec.ID) == "" {
			return nil, nil, fmt.Errorf("module id at index %d is empty", index)
		}
		if _, exists := seen[spec.ID]; exists {
			return nil, nil, fmt.Errorf("module id %q is repeated", spec.ID)
		}
		seen[spec.ID] = struct{}{}
		definition := r.definitions[spec.Module]
		if definition == nil {
			return nil, nil, fmt.Errorf("module %q at index %d is not registered", spec.Module, index)
		}
		binding, err := definition.Compile(module.CompileContext{Scope: spec.Scope}, spec.Config)
		if err != nil {
			return nil, nil, fmt.Errorf("compile module %q (%s): %w", spec.Module, spec.ID, err)
		}
		if binding == nil {
			return nil, nil, fmt.Errorf("compile module %q (%s): nil binding", spec.Module, spec.ID)
		}
		item := compiled{order: index, id: spec.ID, moduleName: spec.Module, binding: binding}
		switch spec.Scope {
		case module.ScopeExchange:
			exchange = append(exchange, item)
		case module.ScopeAttempt:
			attempt = append(attempt, item)
		}
	}
	return exchange, attempt, nil
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
