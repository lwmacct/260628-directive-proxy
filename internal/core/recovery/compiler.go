package recovery

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
)

type ControllerCompiler struct {
	catalog *module.Catalog
}

func NewControllerCompiler(catalog *module.Catalog) (*ControllerCompiler, error) {
	if catalog == nil {
		return nil, errors.New("module catalog is unavailable")
	}
	return &ControllerCompiler{catalog: catalog}, nil
}

func (compiler *ControllerCompiler) Compile(spec module.Spec) (ControllerBinding, error) {
	if compiler == nil || compiler.catalog == nil {
		return nil, errors.New("recovery controller compiler is unavailable")
	}
	name := strings.TrimSpace(spec.Module)
	if name == "" || name != spec.Module {
		return nil, errors.New("recovery controller module is invalid")
	}
	registered := compiler.catalog.Lookup(name)
	if registered == nil {
		return nil, fmt.Errorf("recovery controller module %q is not registered", name)
	}
	definition, ok := registered.(ControllerDefinition)
	if !ok {
		return nil, fmt.Errorf("module %q does not provide recovery controller capability", name)
	}
	config := spec.Config
	if len(config) == 0 {
		config = json.RawMessage(`{}`)
	}
	binding, err := definition.CompileController(config)
	if err != nil {
		return nil, fmt.Errorf("compile recovery controller module %q: %w", name, err)
	}
	if binding == nil {
		return nil, fmt.Errorf("compile recovery controller module %q: nil binding", name)
	}
	return binding, nil
}
