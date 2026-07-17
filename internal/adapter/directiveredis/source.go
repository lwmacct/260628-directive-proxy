package directiveredis

import (
	"context"
	"fmt"
	"strings"
	"time"

	redislib "github.com/redis/go-redis/v9"

	"github.com/lwmacct/260628-directive-proxy/internal/core/directive"
)

type Options struct {
	Timeout             time.Duration
	MaxResponseBytes    int64
	ClientCacheCapacity int
	ClientIdleTimeout   time.Duration
	PoolSize            int
}

type Source struct {
	clients          *clientCache
	maxResponseBytes int64
}

var _ directive.RedisRemoteReader = (*Source)(nil)

func New(opts Options) *Source {
	return &Source{
		clients:          newClientCache(opts.ClientCacheCapacity, opts.ClientIdleTimeout, opts.PoolSize, opts.Timeout),
		maxResponseBytes: opts.MaxResponseBytes,
	}
}

func (s *Source) Read(ctx context.Context, reference directive.RedisReference) ([]byte, error) {
	client, release, err := s.clients.acquire(reference.Endpoint.String())
	if err != nil {
		return nil, err
	}
	defer release()
	value, err := client.Do(ctx, "JSON.GET", reference.Key).Text()
	if err == redislib.Nil {
		return nil, directive.ErrRemoteNotFound
	}
	if err != nil {
		if strings.Contains(strings.ToUpper(err.Error()), "WRONGTYPE") {
			return nil, fmt.Errorf("%w: Redis key is not a JSON document: %w", directive.ErrRemoteInvalid, err)
		}
		return nil, fmt.Errorf("%w: %w", directive.ErrRemoteUnavailable, err)
	}
	if s.maxResponseBytes > 0 && int64(len(value)) > s.maxResponseBytes {
		return nil, fmt.Errorf("%w: response exceeds limit", directive.ErrRemoteInvalid)
	}
	return []byte(value), nil
}

func (s *Source) Close() error {
	if s == nil || s.clients == nil {
		return nil
	}
	return s.clients.close()
}
