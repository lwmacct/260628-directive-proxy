package program

import (
	"fmt"
	"strings"

	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
)

type registry struct {
	catalog *module.Catalog
}

type compiled struct {
	order      int
	moduleName string
	binding    module.Binding
}

func newRegistry(catalog *module.Catalog) (*registry, error) {
	if catalog == nil {
		return nil, fmt.Errorf("module catalog is unavailable")
	}
	return &registry{catalog: catalog}, nil
}

func (r *registry) compile(specs module.Specs) ([]compiled, []compiled, error) {
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
		registered := r.catalog.Lookup(name)
		if registered == nil {
			return nil, nil, fmt.Errorf("module %q at index %d is not registered", name, index)
		}
		definition, ok := registered.(module.ProgramDefinition)
		if !ok {
			return nil, nil, fmt.Errorf("module %q does not provide program capability", name)
		}
		lifetime := definition.Lifetime()
		if lifetime != module.LifetimeExchange && lifetime != module.LifetimeRoundTrip {
			return nil, nil, fmt.Errorf("module %q has invalid lifetime %q", name, lifetime)
		}
		binding, err := definition.CompileProgram(spec.Config)
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
	names := make([]string, 0)
	for _, definition := range r.catalog.Definitions() {
		if _, ok := definition.(module.ProgramDefinition); ok {
			names = append(names, definition.Name())
		}
	}
	return names
}
