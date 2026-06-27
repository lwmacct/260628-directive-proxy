package usage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/lwmacct/260628-llm-relay-dproxy/internal/eventbus"
)

const (
	defaultDeliveryMaxBacklog     = 1000
	defaultDeliveryBatchSize      = 10
	defaultDeliveryFlushInterval  = 5 * time.Second
	defaultDeliveryRequestTimeout = 10 * time.Second
)

var ErrDeliveryBacklogFull = errors.New("usage delivery backlog is full")

type DeliveryOptions struct {
	URL           string
	Token         string
	MaxBacklog    int
	BatchSize     int
	FlushInterval time.Duration
	Timeout       time.Duration
	RESTClient    *resty.Client
}

type DeliveryPublisher struct {
	url           string
	token         string
	maxBacklog    int64
	batchSize     int
	flushInterval time.Duration
	client        *resty.Client

	commands     chan deliveryCommand
	backlogCount atomic.Int64
	closed       atomic.Bool
	mu           sync.Mutex
	sendWG       sync.WaitGroup
	runWG        sync.WaitGroup

	closeOnce sync.Once
	closeDone chan struct{}
	closeErr  error
}

type deliveryCommand struct {
	event eventbus.Event
	flush *deliveryFlush
}

type deliveryFlush struct {
	ctx    context.Context
	all    bool
	result chan error
}

func NewDeliveryPublisher(opts DeliveryOptions) (*DeliveryPublisher, error) {
	url := strings.TrimSpace(opts.URL)
	if url == "" {
		return nil, errors.New("usage delivery url is empty")
	}
	maxBacklog := opts.MaxBacklog
	if maxBacklog <= 0 {
		maxBacklog = defaultDeliveryMaxBacklog
	}
	batchSize := opts.BatchSize
	if batchSize <= 0 {
		batchSize = defaultDeliveryBatchSize
	}
	flushInterval := opts.FlushInterval
	if flushInterval <= 0 {
		flushInterval = defaultDeliveryFlushInterval
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultDeliveryRequestTimeout
	}
	client := opts.RESTClient
	if client == nil {
		client = resty.New()
	}
	client.SetTimeout(timeout)

	publisher := &DeliveryPublisher{
		url:           url,
		token:         strings.TrimSpace(opts.Token),
		maxBacklog:    int64(maxBacklog),
		batchSize:     batchSize,
		flushInterval: flushInterval,
		client:        client,
		commands:      make(chan deliveryCommand, maxBacklog+1),
		closeDone:     make(chan struct{}),
	}
	publisher.runWG.Add(1)
	go publisher.run()
	return publisher, nil
}

func (p *DeliveryPublisher) Publish(ctx context.Context, event eventbus.Event) error {
	if p == nil || event.Type == "" {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	p.mu.Lock()
	if p.closed.Load() {
		p.mu.Unlock()
		return errors.New("usage delivery publisher is closed")
	}
	p.sendWG.Add(1)
	p.mu.Unlock()
	defer p.sendWG.Done()

	if !p.reserveBacklog() {
		slog.Warn("usage delivery backlog full; dropping event", "event_type", event.Type, "event_id", event.EventID, "request_id", event.RequestID, "max_backlog", p.maxBacklog)
		return ErrDeliveryBacklogFull
	}

	select {
	case <-ctx.Done():
		p.releaseBacklog(1)
		return ctx.Err()
	case p.commands <- deliveryCommand{event: event}:
		return nil
	}
}

func (p *DeliveryPublisher) Close(ctx context.Context) error {
	if p == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	started := false
	p.closeOnce.Do(func() {
		started = true
		p.closeErr = p.close(ctx)
		close(p.closeDone)
	})
	if started {
		return p.closeErr
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-p.closeDone:
		return p.closeErr
	}
}

func (p *DeliveryPublisher) close(ctx context.Context) error {
	p.mu.Lock()
	p.closed.Store(true)
	p.mu.Unlock()

	sendersDone := make(chan struct{})
	go func() {
		p.sendWG.Wait()
		close(sendersDone)
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-sendersDone:
	}

	flushErr := p.requestFlush(ctx, true)
	close(p.commands)

	runDone := make(chan struct{})
	go func() {
		p.runWG.Wait()
		close(runDone)
	}()
	select {
	case <-ctx.Done():
		return errors.Join(flushErr, ctx.Err())
	case <-runDone:
		return flushErr
	}
}

func (p *DeliveryPublisher) reserveBacklog() bool {
	for {
		current := p.backlogCount.Load()
		if current >= p.maxBacklog {
			return false
		}
		if p.backlogCount.CompareAndSwap(current, current+1) {
			return true
		}
	}
}

func (p *DeliveryPublisher) releaseBacklog(n int) {
	if n > 0 {
		p.backlogCount.Add(-int64(n))
	}
}

func (p *DeliveryPublisher) backlogLen() int {
	if p == nil {
		return 0
	}
	return int(p.backlogCount.Load())
}

func (p *DeliveryPublisher) run() {
	defer p.runWG.Done()
	ticker := time.NewTicker(p.flushInterval)
	defer ticker.Stop()

	pending := make([]eventbus.Event, 0, p.batchSize)
	nextAttempt := time.Time{}
	flushDue := func(all bool) {
		if !nextAttempt.IsZero() && time.Now().Before(nextAttempt) {
			return
		}
		if err := p.flush(context.Background(), &pending, all); err != nil {
			nextAttempt = time.Now().Add(p.flushInterval)
			slog.Warn("usage delivery failed; keeping events in backlog", "url", p.url, "backlog", p.backlogLen(), "error", err)
			return
		}
		nextAttempt = time.Time{}
	}
	for {
		select {
		case command, ok := <-p.commands:
			if !ok {
				return
			}
			if command.flush != nil {
				command.flush.result <- p.flush(command.flush.ctx, &pending, command.flush.all)
				close(command.flush.result)
				continue
			}
			pending = append(pending, command.event)
			if len(pending) >= p.batchSize {
				flushDue(false)
			}
		case <-ticker.C:
			flushDue(true)
		}
	}
}

func (p *DeliveryPublisher) requestFlush(ctx context.Context, all bool) error {
	if ctx == nil {
		ctx = context.Background()
	}
	flush := &deliveryFlush{
		ctx:    ctx,
		all:    all,
		result: make(chan error, 1),
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case p.commands <- deliveryCommand{flush: flush}:
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-flush.result:
		return err
	}
}

func (p *DeliveryPublisher) flush(ctx context.Context, pending *[]eventbus.Event, all bool) error {
	if len(*pending) == 0 {
		return nil
	}
	for len(*pending) > 0 {
		if !all && len(*pending) < p.batchSize {
			return nil
		}
		n := p.batchSize
		if n > len(*pending) {
			n = len(*pending)
		}
		batch := append([]eventbus.Event(nil), (*pending)[:n]...)
		if err := p.deliver(ctx, batch); err != nil {
			return err
		}
		copy(*pending, (*pending)[n:])
		*pending = (*pending)[:len(*pending)-n]
		p.releaseBacklog(n)
	}
	return nil
}

func (p *DeliveryPublisher) deliver(ctx context.Context, batch []eventbus.Event) error {
	data, err := json.Marshal(batch)
	if err != nil {
		return err
	}
	req := p.client.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetBody(data)
	if p.token != "" {
		req.SetAuthToken(p.token)
	}
	resp, err := req.Post(p.url)
	if err != nil {
		return err
	}
	if resp.StatusCode() != http.StatusNoContent {
		return fmt.Errorf("usage delivery returned status %d", resp.StatusCode())
	}
	return nil
}
