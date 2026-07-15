package recordoutput

import (
	"context"
	"sync"

	"github.com/lwmacct/260628-directive-proxy/internal/core/observability"
)

type Output struct {
	mu      sync.Mutex
	records []observability.Record
}

func New(name string) *Output {
	return &Output{}
}

func (*Output) Start(context.Context) error { return nil }

func (o *Output) Write(_ context.Context, _ int, record observability.Record) error {
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
