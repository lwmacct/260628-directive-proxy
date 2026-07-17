package server

import (
	"errors"

	"github.com/lwmacct/260628-directive-proxy/internal/adapter/directivefile"
	"github.com/lwmacct/260628-directive-proxy/internal/adapter/directivehttp"
	"github.com/lwmacct/260628-directive-proxy/internal/adapter/directiveredis"
	"github.com/lwmacct/260628-directive-proxy/internal/config"
)

type directiveRemotes struct {
	http  *directivehttp.Source
	redis *directiveredis.Source
	file  *directivefile.Source
}

func newDirectiveRemotes(cfg config.RemoteDirective) *directiveRemotes {
	return &directiveRemotes{
		http: directivehttp.New(directivehttp.Options{
			Timeout:          cfg.Timeout,
			MaxRequestBytes:  cfg.HTTP.MaxRequestBytes,
			MaxResponseBytes: cfg.MaxResponseBytes,
		}),
		redis: directiveredis.New(directiveredis.Options{
			Timeout:             cfg.Timeout,
			MaxResponseBytes:    cfg.MaxResponseBytes,
			ClientCacheCapacity: cfg.Redis.ClientCacheCapacity,
			ClientIdleTimeout:   cfg.Redis.ClientIdleTimeout,
			PoolSize:            cfg.Redis.PoolSize,
		}),
		file: directivefile.New(directivefile.Options{
			Root:             cfg.File.Root,
			MaxResponseBytes: cfg.MaxResponseBytes,
		}),
	}
}

func (r *directiveRemotes) Close() error {
	if r == nil {
		return nil
	}
	return errors.Join(r.http.Close(), r.redis.Close())
}
