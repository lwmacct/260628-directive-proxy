package program

import "github.com/lwmacct/260628-directive-proxy/internal/core/module"

type Compiler interface {
	Compile(module.Specs) (*Executable, error)
}

type Executable struct {
	exchange  []compiled
	roundTrip []compiled
}
