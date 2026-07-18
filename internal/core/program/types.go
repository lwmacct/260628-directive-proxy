package program

import (
	"encoding/json"
)

type Spec struct {
	Module string          `json:"module"`
	Config json.RawMessage `json:"config,omitempty"`
}

type Program []Spec

type Compiler interface {
	Compile(Program) (*Executable, error)
}

type Executable struct {
	exchange  []compiled
	roundTrip []compiled
}
