package program

import (
	"encoding/json"

	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
)

type Spec struct {
	Scope  module.ScopeKind `json:"scope"`
	ID     string           `json:"id"`
	Module string           `json:"module"`
	Config json.RawMessage  `json:"config,omitempty"`
}

type Program []Spec

type Compiler interface {
	Compile(Program) (*Executable, error)
}

type Executable struct {
	exchange []compiled
	attempt  []compiled
}
