package recordoutput

import (
	"context"
	"sync"

	"github.com/lwmacct/260628-directive-proxy/internal/core/observability"
)

type Output struct {
	name    string
	mu      sync.Mutex
	records []observability.Record
}

func New(name string) *Output {
	if name == "" {
		name = "memory"
	}
	return &Output{name: name}
}

func (o *Output) Name() string { return o.name }

func (*Output) Start(context.Context) error { return nil }

func (o *Output) Write(_ context.Context, record observability.Record) error {
	o.mu.Lock()
	o.records = append(o.records, record)
	o.mu.Unlock()
	return nil
}

func (*Output) Health() observability.HealthStatus {
	return observability.HealthStatus{Status: "ok"}
}

func (*Output) Close(context.Context) error { return nil }

func (o *Output) Records() []observability.Record {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]observability.Record(nil), o.records...)
}
