package server

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lwmacct/260714-go-pkg-fluent/pkg/fluent"

	"github.com/lwmacct/260628-directive-proxy/internal/core/observability"
)

// fluentSink adapts the configured Fluent client to the observability sink
// port. It is kept in the composition root because no other package consumes
// this application-specific adapter.
type fluentSink struct {
	config          fluent.Config
	mu              sync.RWMutex
	client          *fluent.Client
	started         bool
	closed          bool
	healthy         atomic.Bool
	lastFailureNano atomic.Int64
}

func newFluentSink(config fluent.Config) *fluentSink {
	return &fluentSink{config: config}
}

func (s *fluentSink) Start(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("fluent sink is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return nil
	}
	if s.closed {
		return fmt.Errorf("fluent sink is closed")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	client, err := fluent.New(s.config)
	if err != nil {
		return fmt.Errorf("create fluent client: %w", err)
	}
	s.client = client
	s.started = true
	s.healthy.Store(true)
	return nil
}

func (s *fluentSink) Write(ctx context.Context, _ int, record observability.Record) error {
	if s == nil {
		return fmt.Errorf("fluent sink is nil")
	}
	s.mu.RLock()
	if !s.started || s.closed || s.client == nil {
		s.mu.RUnlock()
		return fmt.Errorf("fluent sink is unavailable")
	}
	client := s.client
	s.mu.RUnlock()
	err := client.Send(ctx, record.Topic, fluent.Entry{Time: record.Time, Record: fluent.Record(record.Map())})
	if err != nil {
		s.healthy.Store(false)
		s.lastFailureNano.Store(time.Now().UTC().UnixNano())
	} else {
		s.healthy.Store(true)
	}
	return err
}

func (s *fluentSink) Health() observability.HealthStatus {
	if s == nil {
		return observability.HealthStatus{Status: "unavailable"}
	}
	s.mu.RLock()
	available := s.started && !s.closed && s.client != nil
	s.mu.RUnlock()
	if !available {
		return observability.HealthStatus{Status: "unavailable"}
	}
	status := observability.HealthStatus{Status: "ok"}
	if !s.healthy.Load() {
		status.Status = "degraded"
	}
	if value := s.lastFailureNano.Load(); value > 0 {
		status.LastFailureAt = time.Unix(0, value).UTC()
	}
	return status
}

func (s *fluentSink) Close(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	client := s.client
	s.client = nil
	s.mu.Unlock()

	if client == nil {
		return nil
	}
	if err := client.Shutdown(ctx); err != nil {
		client.Abort()
		return err
	}
	return nil
}
