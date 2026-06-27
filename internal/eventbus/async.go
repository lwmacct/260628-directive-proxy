package eventbus

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

type AsyncPublisher struct {
	base   Publisher
	queue  chan Event
	wg     sync.WaitGroup
	sendWG sync.WaitGroup
	mu     sync.Mutex
	closed bool
}

type AsyncOptions struct {
	QueueSize int
}

func NewAsyncPublisher(base Publisher, opts AsyncOptions) *AsyncPublisher {
	if opts.QueueSize <= 0 {
		opts.QueueSize = 1_000_000
	}
	if base == nil {
		base = NopPublisher{}
	}
	publisher := &AsyncPublisher{
		base:  base,
		queue: make(chan Event, opts.QueueSize),
	}
	publisher.wg.Add(1)
	go publisher.run(publisher.queue)
	return publisher
}

func (p *AsyncPublisher) Publish(ctx context.Context, event Event) error {
	if event.Type == "" {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if p == nil {
		return fmt.Errorf("async publisher is nil")
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return fmt.Errorf("async publisher is closed")
	}
	p.sendWG.Add(1)
	p.mu.Unlock()
	defer p.sendWG.Done()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case p.queue <- event:
	}
	return nil
}

func (p *AsyncPublisher) Close(ctx context.Context) error {
	if p == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	p.mu.Unlock()
	sendDone := make(chan struct{})
	go func() {
		p.sendWG.Wait()
		close(sendDone)
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-sendDone:
	}
	close(p.queue)
	drainDone := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(drainDone)
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-drainDone:
	}
	return p.base.Close(ctx)
}

func (p *AsyncPublisher) run(queue <-chan Event) {
	defer p.wg.Done()
	for event := range queue {
		if err := p.base.Publish(context.Background(), event); err != nil {
			slog.Error("publish event failed", "event_type", event.Type, "event_id", event.EventID, "request_id", event.RequestID, "error", err)
		}
	}
}
