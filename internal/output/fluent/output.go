package fluentoutput

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lwmacct/260714-go-pkg-fluent/pkg/fluent"

	"github.com/lwmacct/260628-directive-proxy/internal/core/observability"
)

type Output struct {
	config          fluent.Config
	mu              sync.RWMutex
	client          *fluent.Client
	started         bool
	closed          bool
	healthy         atomic.Bool
	lastFailureNano atomic.Int64
}

func New(config fluent.Config) *Output {
	return &Output{config: config}
}

func (o *Output) Start(ctx context.Context) error {
	if o == nil {
		return fmt.Errorf("fluent output is nil")
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.started {
		return nil
	}
	if o.closed {
		return fmt.Errorf("fluent output is closed")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	client, err := fluent.New(o.config)
	if err != nil {
		return fmt.Errorf("create fluent client: %w", err)
	}
	o.client = client
	o.started = true
	o.healthy.Store(true)
	return nil
}

func (o *Output) Write(ctx context.Context, _ int, record observability.Record) error {
	if o == nil {
		return fmt.Errorf("fluent output is nil")
	}
	o.mu.RLock()
	if !o.started || o.closed || o.client == nil {
		o.mu.RUnlock()
		return fmt.Errorf("fluent output is unavailable")
	}
	client := o.client
	o.mu.RUnlock()
	err := client.Send(ctx, record.Topic, fluent.Entry{Time: record.Time, Record: fluent.Record(record.Map())})
	if err != nil {
		o.healthy.Store(false)
		o.lastFailureNano.Store(time.Now().UTC().UnixNano())
	} else {
		o.healthy.Store(true)
	}
	return err
}

func (o *Output) Health() observability.HealthStatus {
	if o == nil {
		return observability.HealthStatus{Status: "unavailable"}
	}
	o.mu.RLock()
	available := o.started && !o.closed && o.client != nil
	o.mu.RUnlock()
	if !available {
		return observability.HealthStatus{Status: "unavailable"}
	}
	status := observability.HealthStatus{Status: "ok"}
	if !o.healthy.Load() {
		status.Status = "degraded"
	}
	if value := o.lastFailureNano.Load(); value > 0 {
		status.LastFailureAt = time.Unix(0, value).UTC()
	}
	return status
}

func (o *Output) Close(ctx context.Context) error {
	if o == nil {
		return nil
	}
	o.mu.Lock()
	if o.closed {
		o.mu.Unlock()
		return nil
	}
	o.closed = true
	client := o.client
	o.client = nil
	o.mu.Unlock()

	if client == nil {
		return nil
	}
	if err := client.Shutdown(ctx); err != nil {
		client.Abort()
		return err
	}
	return nil
}
