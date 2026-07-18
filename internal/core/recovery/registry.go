package recovery

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type Registry struct {
	definitions map[string]ControllerDefinition
}

func NewRegistry(definitions ...ControllerDefinition) (*Registry, error) {
	registry := &Registry{definitions: make(map[string]ControllerDefinition, len(definitions))}
	for _, definition := range definitions {
		if definition == nil {
			return nil, errors.New("recovery controller definition is nil")
		}
		name := strings.TrimSpace(definition.Name())
		if name == "" || name != definition.Name() {
			return nil, errors.New("recovery controller definition name is invalid")
		}
		if _, exists := registry.definitions[name]; exists {
			return nil, fmt.Errorf("recovery controller module %q is repeated", name)
		}
		registry.definitions[name] = definition
	}
	return registry, nil
}

func (registry *Registry) Compile(spec ControllerSpec) (ControllerBinding, error) {
	if registry == nil {
		return nil, errors.New("recovery controller registry is unavailable")
	}
	name := strings.TrimSpace(spec.Module)
	if name == "" || name != spec.Module {
		return nil, errors.New("recovery controller module is invalid")
	}
	definition := registry.definitions[name]
	if definition == nil {
		return nil, fmt.Errorf("recovery controller module %q is not registered", name)
	}
	config := spec.Config
	if len(config) == 0 {
		config = json.RawMessage(`{}`)
	}
	binding, err := definition.Compile(config)
	if err != nil {
		return nil, fmt.Errorf("compile recovery controller module %q: %w", name, err)
	}
	if binding == nil {
		return nil, fmt.Errorf("compile recovery controller module %q: nil binding", name)
	}
	return binding, nil
}
