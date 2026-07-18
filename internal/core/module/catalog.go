package module

import (
	"errors"
	"fmt"
	"strings"
)

type Catalog struct {
	definitions map[string]Definition
}

func NewCatalog(definitions ...Definition) (*Catalog, error) {
	catalog := &Catalog{definitions: make(map[string]Definition, len(definitions))}
	for _, definition := range definitions {
		if definition == nil {
			return nil, errors.New("module definition is nil")
		}
		name := strings.TrimSpace(definition.Name())
		if name == "" || name != definition.Name() {
			return nil, errors.New("module definition name is invalid")
		}
		if _, exists := catalog.definitions[name]; exists {
			return nil, fmt.Errorf("module %q is registered more than once", name)
		}
		catalog.definitions[name] = definition
	}
	return catalog, nil
}

func MustCatalog(definitions ...Definition) *Catalog {
	catalog, err := NewCatalog(definitions...)
	if err != nil {
		panic(err)
	}
	return catalog
}

func (catalog *Catalog) Lookup(name string) Definition {
	if catalog == nil {
		return nil
	}
	return catalog.definitions[name]
}

func (catalog *Catalog) Definitions() []Definition {
	if catalog == nil {
		return nil
	}
	result := make([]Definition, 0, len(catalog.definitions))
	for _, definition := range catalog.definitions {
		result = append(result, definition)
	}
	return result
}
