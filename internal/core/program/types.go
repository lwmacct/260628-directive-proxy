package program

import "github.com/lwmacct/260628-directive-proxy/internal/core/module"

type Program []module.Spec

type Compiler interface {
	Compile(Program) (*Executable, error)
}

type Executable struct {
	exchange  []compiled
	roundTrip []compiled
}
