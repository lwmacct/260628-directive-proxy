package bodymemory

import (
	"context"
	"errors"
	"sync"
	"time"
)

var (
	ErrBodyTooLarge = errors.New("request body exceeds memory limit")
	ErrQueueFull    = errors.New("request body memory queue is full")
	ErrWaitTimeout  = errors.New("request body memory wait timed out")
)

type Config struct {
	MaxActiveBytes int64
	MaxBodyBytes   int64
	QueueMax       int
	QueueWait      time.Duration
}

type Controller struct {
	config Config

	mu    sync.Mutex
	used  int64
	queue []*waiter
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
	UsedBytes      int64
	AvailableBytes int64
	QueuedRequests int
}

func New(config Config) *Controller {
	return &Controller{config: config}
}

func (c *Controller) Reserve(ctx context.Context, size int64) (*Reservation, error) {
	if c == nil || size < 0 || size > c.config.MaxBodyBytes || size > c.config.MaxActiveBytes {
		return nil, ErrBodyTooLarge
	}
	if size == 0 {
		return &Reservation{}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	c.mu.Lock()
	if len(c.queue) == 0 && c.used+size <= c.config.MaxActiveBytes {
		reservation := c.grantLocked(size)
		c.mu.Unlock()
		return reservation, nil
	}
	if c.config.QueueMax <= 0 || len(c.queue) >= c.config.QueueMax {
		c.mu.Unlock()
		return nil, ErrQueueFull
	}
	wait := &waiter{size: size, ready: make(chan *Reservation, 1)}
	c.queue = append(c.queue, wait)
	c.mu.Unlock()

	waitCtx := ctx
	cancel := func() {}
	if c.config.QueueWait > 0 {
		waitCtx, cancel = context.WithTimeout(ctx, c.config.QueueWait)
	}
	defer cancel()

	select {
	case reservation := <-wait.ready:
		if err := waitCtx.Err(); err != nil {
			reservation.Close()
			return nil, reserveContextError(err, ctx.Err())
		}
		return reservation, nil
	case <-waitCtx.Done():
		c.mu.Lock()
		if !wait.granted {
			for index, queued := range c.queue {
				if queued == wait {
					c.queue = append(c.queue[:index], c.queue[index+1:]...)
					break
				}
			}
			c.dispatchLocked()
			c.mu.Unlock()
			return nil, reserveContextError(waitCtx.Err(), ctx.Err())
		}
		c.mu.Unlock()
		reservation := <-wait.ready
		reservation.Close()
		return nil, reserveContextError(waitCtx.Err(), ctx.Err())
	}
}

func (c *Controller) Snapshot() Snapshot {
	if c == nil {
		return Snapshot{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	available := c.config.MaxActiveBytes - c.used
	if available < 0 {
		available = 0
	}
	return Snapshot{UsedBytes: c.used, AvailableBytes: available, QueuedRequests: len(c.queue)}
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
		if c.used+next.size > c.config.MaxActiveBytes {
			return
		}
		c.queue = c.queue[1:]
		next.granted = true
		next.ready <- c.grantLocked(next.size)
	}
}

func reserveContextError(waitErr, parentErr error) error {
	if parentErr != nil {
		return parentErr
	}
	if errors.Is(waitErr, context.DeadlineExceeded) {
		return ErrWaitTimeout
	}
	return waitErr
}
