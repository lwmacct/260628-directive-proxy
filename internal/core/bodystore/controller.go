package bodystore

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrBodyTooLarge  = errors.New("request body exceeds replay store limit")
	ErrStoreCapacity = errors.New("request body replay store capacity is exhausted")
	ErrQueueFull     = errors.New("request body replay store queue is full")
	ErrQueueTimeout  = errors.New("request body replay store queue wait timed out")
	ErrStoreRetired  = errors.New("request body replay store is retired")
	ErrStoreClosed   = errors.New("request body replay store is closed")
)

type Config struct {
	MemoryMaxBytes   int64
	MaxBodyBytes     int64
	ChunkBytes       int
	QueueMaxRequests int
}

type StreamOptions struct {
	MaxBodyBytes int64
	QueueWait    time.Duration
	ChunkBytes   int
}

type Controller struct {
	config Config

	mu                sync.Mutex
	used              int64
	queue             []*waiter
	admittedTotal     atomic.Uint64
	queueFullTotal    atomic.Uint64
	queueTimeoutTotal atomic.Uint64
	canceledTotal     atomic.Uint64
	maxQueueWaitNano  atomic.Int64
}

type waiter struct {
	size    int64
	ready   chan *Reservation
	granted bool
}

type Reservation struct {
	controller *Controller
	size       int64
	once       sync.Once
}

type Snapshot struct {
	MemoryUsedBytes      int64
	MemoryAvailableBytes int64
	QueuedRequests       int
	AdmittedTotal        uint64
	QueueFullTotal       uint64
	QueueTimeoutTotal    uint64
	CanceledTotal        uint64
	MaxQueueWaitNanos    int64
}

func New(config Config) *Controller {
	if config.ChunkBytes <= 0 {
		config.ChunkBytes = 64 << 10
	}
	return &Controller{config: config}
}

func (c *Controller) Snapshot() Snapshot {
	if c == nil {
		return Snapshot{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	available := c.config.MemoryMaxBytes - c.used
	if available < 0 {
		available = 0
	}
	return Snapshot{
		MemoryUsedBytes: c.used, MemoryAvailableBytes: available, QueuedRequests: len(c.queue),
		AdmittedTotal: c.admittedTotal.Load(), QueueFullTotal: c.queueFullTotal.Load(),
		QueueTimeoutTotal: c.queueTimeoutTotal.Load(), CanceledTotal: c.canceledTotal.Load(),
		MaxQueueWaitNanos: c.maxQueueWaitNano.Load(),
	}
}

func (r *Reservation) Size() int64 {
	if r == nil {
		return 0
	}
	return r.size
}

func (r *Reservation) Close() {
	if r == nil || r.controller == nil || r.size == 0 {
		return
	}
	r.once.Do(func() { r.controller.release(r.size) })
}

func (c *Controller) Admit(ctx context.Context, expected, maxBodyBytes int64, queueWait time.Duration, chunkBytes int) (*Reservation, error) {
	if c == nil {
		return nil, ErrStoreCapacity
	}
	if maxBodyBytes <= 0 {
		maxBodyBytes = c.config.MaxBodyBytes
	}
	if maxBodyBytes <= 0 || expected > maxBodyBytes {
		return nil, ErrBodyTooLarge
	}
	chunkBytes = normalizeChunkBytes(chunkBytes, c.config.ChunkBytes)
	reserveSize := maxBodyBytes + int64(chunkBytes)
	if expected >= 0 {
		reserveSize = expected + int64(chunkBytes)
	}
	if reserveSize < 0 {
		return nil, ErrBodyTooLarge
	}
	return c.admit(ctx, reserveSize, queueWait)
}

func normalizeChunkBytes(value, fallback int) int {
	if value > 0 {
		return value
	}
	if fallback > 0 {
		return fallback
	}
	return 64 << 10
}

func (c *Controller) admit(ctx context.Context, size int64, wait time.Duration) (*Reservation, error) {
	if c == nil {
		return nil, ErrStoreCapacity
	}
	if size < 0 {
		return nil, ErrBodyTooLarge
	}
	if size > c.config.MemoryMaxBytes {
		return nil, ErrStoreCapacity
	}
	if size == 0 {
		return &Reservation{}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	c.mu.Lock()
	if len(c.queue) == 0 && c.used+size <= c.config.MemoryMaxBytes {
		reservation := c.grantLocked(size)
		c.mu.Unlock()
		c.admittedTotal.Add(1)
		return reservation, nil
	}
	if c.config.QueueMaxRequests <= 0 || len(c.queue) >= c.config.QueueMaxRequests {
		c.mu.Unlock()
		c.queueFullTotal.Add(1)
		return nil, ErrQueueFull
	}
	item := &waiter{size: size, ready: make(chan *Reservation, 1)}
	queuedAt := time.Now()
	c.queue = append(c.queue, item)
	c.mu.Unlock()

	waitCtx := ctx
	cancel := func() {}
	if wait >= 0 {
		waitCtx, cancel = context.WithTimeout(ctx, wait)
	}
	defer cancel()
	select {
	case reservation := <-item.ready:
		c.recordQueueWait(time.Since(queuedAt))
		if err := waitCtx.Err(); err != nil {
			reservation.Close()
			c.recordAdmissionFailure(err, ctx.Err())
			return nil, admissionContextError(err, ctx.Err())
		}
		c.admittedTotal.Add(1)
		return reservation, nil
	case <-waitCtx.Done():
		c.mu.Lock()
		if !item.granted {
			for index, queued := range c.queue {
				if queued == item {
					c.queue = append(c.queue[:index], c.queue[index+1:]...)
					break
				}
			}
			c.dispatchLocked()
			c.mu.Unlock()
			c.recordQueueWait(time.Since(queuedAt))
			c.recordAdmissionFailure(waitCtx.Err(), ctx.Err())
			return nil, admissionContextError(waitCtx.Err(), ctx.Err())
		}
		c.mu.Unlock()
		reservation := <-item.ready
		reservation.Close()
		c.recordQueueWait(time.Since(queuedAt))
		c.recordAdmissionFailure(waitCtx.Err(), ctx.Err())
		return nil, admissionContextError(waitCtx.Err(), ctx.Err())
	}
}

func (c *Controller) recordQueueWait(wait time.Duration) {
	for {
		current := c.maxQueueWaitNano.Load()
		if wait.Nanoseconds() <= current || c.maxQueueWaitNano.CompareAndSwap(current, wait.Nanoseconds()) {
			return
		}
	}
}

func (c *Controller) recordAdmissionFailure(waitErr, parentErr error) {
	if parentErr != nil || errors.Is(waitErr, context.Canceled) {
		c.canceledTotal.Add(1)
		return
	}
	if errors.Is(waitErr, context.DeadlineExceeded) {
		c.queueTimeoutTotal.Add(1)
	}
}

func admissionContextError(waitErr, parentErr error) error {
	if parentErr != nil {
		return parentErr
	}
	if errors.Is(waitErr, context.DeadlineExceeded) {
		return ErrQueueTimeout
	}
	return waitErr
}

func (c *Controller) grantLocked(size int64) *Reservation {
	c.used += size
	return &Reservation{controller: c, size: size}
}

func (c *Controller) release(size int64) {
	c.mu.Lock()
	c.used -= size
	if c.used < 0 {
		c.used = 0
	}
	c.dispatchLocked()
	c.mu.Unlock()
}

func (c *Controller) dispatchLocked() {
	for len(c.queue) > 0 {
		next := c.queue[0]
		if c.used+next.size > c.config.MemoryMaxBytes {
			return
		}
		c.queue = c.queue[1:]
		next.granted = true
		next.ready <- c.grantLocked(next.size)
	}
}
