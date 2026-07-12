package remote

import (
	"crypto/sha256"
	"errors"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/core/directive"
)

type redisClientCache struct {
	mu          sync.Mutex
	entries     map[[sha256.Size]byte]*redisClientEntry
	capacity    int
	idleTimeout time.Duration
	poolSize    int
	timeout     time.Duration
	closed      bool
}

type redisClientEntry struct {
	client   *redis.Client
	refs     int
	lastUsed time.Time
}

func newRedisClientCache(capacity int, idleTimeout time.Duration, poolSize int, timeout time.Duration) *redisClientCache {
	return &redisClientCache{
		entries:     make(map[[sha256.Size]byte]*redisClientEntry),
		capacity:    capacity,
		idleTimeout: idleTimeout,
		poolSize:    poolSize,
		timeout:     timeout,
	}
}

func (c *redisClientCache) acquire(rawURL string) (*redis.Client, func(), error) {
	opts, err := redis.ParseURL(rawURL)
	if err != nil {
		return nil, nil, err
	}
	opts.PoolSize = c.poolSize
	opts.MaxRetries = -1
	if c.timeout > 0 {
		opts.DialTimeout = c.timeout
		opts.ReadTimeout = c.timeout
		opts.WriteTimeout = c.timeout
	}

	now := time.Now()
	fingerprint := sha256.Sum256([]byte(rawURL))
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, nil, directive.ErrRemoteUnavailable
	}
	c.evictIdle(now)
	if entry := c.entries[fingerprint]; entry != nil {
		entry.refs++
		entry.lastUsed = now
		return entry.client, c.releaseFunc(fingerprint), nil
	}
	if len(c.entries) >= c.capacity && !c.evictOldestIdle() {
		return nil, nil, directive.ErrRemoteUnavailable
	}
	client := redis.NewClient(opts)
	c.entries[fingerprint] = &redisClientEntry{client: client, refs: 1, lastUsed: now}
	return client, c.releaseFunc(fingerprint), nil
}

func (c *redisClientCache) releaseFunc(fingerprint [sha256.Size]byte) func() {
	return func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if entry := c.entries[fingerprint]; entry != nil {
			entry.refs--
			entry.lastUsed = time.Now()
		}
	}
}

func (c *redisClientCache) evictIdle(now time.Time) {
	if c.idleTimeout <= 0 {
		return
	}
	for fingerprint, entry := range c.entries {
		if entry.refs == 0 && now.Sub(entry.lastUsed) >= c.idleTimeout {
			_ = entry.client.Close()
			delete(c.entries, fingerprint)
		}
	}
}

func (c *redisClientCache) evictOldestIdle() bool {
	var oldestKey [sha256.Size]byte
	var oldest *redisClientEntry
	for fingerprint, entry := range c.entries {
		if entry.refs == 0 && (oldest == nil || entry.lastUsed.Before(oldest.lastUsed)) {
			oldestKey = fingerprint
			oldest = entry
		}
	}
	if oldest == nil {
		return false
	}
	_ = oldest.client.Close()
	delete(c.entries, oldestKey)
	return true
}

func (c *redisClientCache) close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	var errs []error
	for fingerprint, entry := range c.entries {
		errs = append(errs, entry.client.Close())
		delete(c.entries, fingerprint)
	}
	return errors.Join(errs...)
}
