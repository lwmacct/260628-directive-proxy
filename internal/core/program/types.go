package program

import "encoding/json"

type Spec struct {
	ID     string          `json:"id"`
	Module string          `json:"module"`
	Config json.RawMessage `json:"config,omitempty"`
}

type Program struct {
	Request []Spec `json:"request,omitempty"`
	Attempt []Spec `json:"attempt,omitempty"`
}

type Compiler interface {
	Compile(Program) (*Executable, error)
}

type Executable struct {
	request []compiled
	attempt []compiled
}
