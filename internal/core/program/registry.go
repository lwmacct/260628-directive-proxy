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
	roundTrip := make([]compiled, 0, len(specs))
	seen := make(map[string]struct{}, len(specs))
	for index, spec := range specs {
		name := strings.TrimSpace(spec.Module)
		if name == "" {
			return nil, nil, fmt.Errorf("module name at index %d is empty", index)
		}
		if _, exists := seen[name]; exists {
			return nil, nil, fmt.Errorf("module %q is repeated", name)
		}
		seen[name] = struct{}{}
		definition := r.definitions[name]
		if definition == nil {
			return nil, nil, fmt.Errorf("module %q at index %d is not registered", name, index)
		}
		lifetime := definition.Lifetime()
		if lifetime != module.LifetimeExchange && lifetime != module.LifetimeRoundTrip {
			return nil, nil, fmt.Errorf("module %q has invalid lifetime %q", name, lifetime)
		}
		binding, err := definition.Compile(spec.Config)
		if err != nil {
			return nil, nil, fmt.Errorf("compile module %q: %w", name, err)
		}
		if binding == nil {
			return nil, nil, fmt.Errorf("compile module %q: nil binding", name)
		}
		item := compiled{order: index, moduleName: name, binding: binding}
		switch lifetime {
		case module.LifetimeExchange:
			exchange = append(exchange, item)
		case module.LifetimeRoundTrip:
			roundTrip = append(roundTrip, item)
		}
	}
	return exchange, roundTrip, nil
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
