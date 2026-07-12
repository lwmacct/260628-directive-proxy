package redisstore

import (
	"context"
	"errors"

	"github.com/redis/go-redis/v9"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/core/directive"
)

type Store struct {
	client *redis.Client
	prefix string
}

func New(rawURL, prefix string) (*Store, error) {
	opts, err := redis.ParseURL(rawURL)
	if err != nil {
		return nil, err
	}
	return &Store{
		client: redis.NewClient(opts),
		prefix: prefix,
	}, nil
}

func (s *Store) Get(ctx context.Context, key string) ([]byte, error) {
	if s == nil || s.client == nil {
		return nil, errors.New("redis directive store is closed")
	}
	value, err := s.client.Get(ctx, s.prefix+key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, directive.ErrStoreKeyNotFound
	}
	return value, err
}

func (s *Store) Close() error {
	if s == nil || s.client == nil {
		return nil
	}
	err := s.client.Close()
	s.client = nil
	return err
}
