package server

import (
	"context"
	"errors"
	"net/http"

	remotehttp "github.com/lwmacct/260628-llm-relay-dproxy/internal/adapter/directive/remote/http"
	remoteredis "github.com/lwmacct/260628-llm-relay-dproxy/internal/adapter/directive/remote/redis"
	"github.com/lwmacct/260628-llm-relay-dproxy/internal/config"
	"github.com/lwmacct/260628-llm-relay-dproxy/internal/core/directive"
)

type directiveRemoteReader struct {
	http  *remotehttp.Source
	redis *remoteredis.Source
}

func newDirectiveRemoteReader(cfg config.RemoteDirective) *directiveRemoteReader {
	return &directiveRemoteReader{
		http: remotehttp.New(remotehttp.Options{
			Timeout:          cfg.Timeout,
			MaxRequestBytes:  cfg.HTTP.MaxRequestBytes,
			MaxResponseBytes: cfg.MaxResponseBytes,
		}),
		redis: remoteredis.New(remoteredis.Options{
			Timeout:             cfg.Timeout,
			MaxResponseBytes:    cfg.MaxResponseBytes,
			ClientCacheCapacity: cfg.Redis.ClientCacheCapacity,
			ClientIdleTimeout:   cfg.Redis.ClientIdleTimeout,
			PoolSize:            cfg.Redis.PoolSize,
		}),
	}
}

func (r *directiveRemoteReader) Read(ctx context.Context, spec directive.RemoteSpec, req *http.Request) ([]byte, error) {
	if r == nil {
		return nil, directive.ErrRemoteUnavailable
	}
	switch spec.Type {
	case directive.RemoteTypeHTTP:
		return r.http.Read(ctx, spec, req)
	case directive.RemoteTypeRedis:
		return r.redis.Read(ctx, spec, req)
	default:
		return nil, directive.ErrRemoteUnavailable
	}
}

func (r *directiveRemoteReader) Close() error {
	if r == nil {
		return nil
	}
	return errors.Join(r.http.Close(), r.redis.Close())
}
