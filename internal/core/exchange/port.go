package exchange

import "context"

type Collector interface {
	Begin() (Capture, bool)
	Complete(context.Context, Record) error
}

type Writer interface {
	Write(context.Context, Record) error
}
