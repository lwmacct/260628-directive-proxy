package recordoutput

import (
	"context"
	"sync"

	"github.com/lwmacct/260628-directive-proxy/internal/core/event"
)

type Output struct {
	mu      sync.Mutex
	records []event.Record
}

func New(name string) *Output {
	return &Output{}
}

func (*Output) Start(context.Context) error { return nil }

func (o *Output) Write(_ context.Context, _ int, record event.Record) error {
	o.mu.Lock()
	o.records = append(o.records, record)
	o.mu.Unlock()
	return nil
}

func (*Output) Health() event.Status {
	return event.Status{Status: "ok"}
}

func (*Output) Close(context.Context) error { return nil }

func (o *Output) Records() []event.Record {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]event.Record(nil), o.records...)
}
