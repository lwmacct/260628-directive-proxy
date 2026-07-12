package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/lwmacct/260614-go-pkg-tlsreload/pkg/tlsreload"
	"github.com/lwmacct/260711-go-pkg-oidcauth/pkg/oidcauth"
	"github.com/lwmacct/260711-go-pkg-oidcauth/pkg/oidcauth/dexgithub"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/adapter/directive/remote"
	"github.com/lwmacct/260628-llm-relay-dproxy/internal/adapter/exchange/capture"
	"github.com/lwmacct/260628-llm-relay-dproxy/internal/config"
	"github.com/lwmacct/260628-llm-relay-dproxy/internal/core/directive"
	"github.com/lwmacct/260628-llm-relay-dproxy/internal/core/exchange"
	"github.com/lwmacct/260628-llm-relay-dproxy/internal/core/proxy"
	"github.com/lwmacct/260628-llm-relay-dproxy/internal/service"
)

const httpTLSMinVersion = tls.VersionTLS12

type runtime struct {
	exchanges       *service.ExchangeService
	observer        proxy.Observer
	oidcAuth        *oidcauth.Auth
	tls             *tlsRuntime
	directiveReader *remote.Reader
}

func newRuntime(ctx context.Context, cfg *config.Config) (*runtime, error) {
	tlsRuntime, err := newTLSRuntime(ctx, cfg.Server.HTTP.TLS)
	if err != nil {
		return nil, fmt.Errorf("configure tls: %w", err)
	}
	oidcAuth, err := dexgithub.New(ctx, cfg.Server.HTTP.OIDCAuth, dexgithub.Options{})
	if err != nil {
		tlsRuntime.Close()
		return nil, fmt.Errorf("configure authentication: %w", err)
	}
	exchanges := service.NewExchangeService(exchange.DefaultCapacity, exchange.DefaultMaxBodyBytes)
	remoteConfig := cfg.Proxy.Directive.Remote
	directiveReader := remote.New(remote.Options{
		Timeout:                  remoteConfig.Timeout,
		MaxRequestBytes:          remoteConfig.MaxRequestBytes,
		MaxResponseBytes:         remoteConfig.MaxResponseBytes,
		RedisClientCacheCapacity: remoteConfig.RedisClientCacheCapacity,
		RedisClientIdleTimeout:   remoteConfig.RedisClientIdleTimeout,
		RedisPoolSize:            remoteConfig.RedisPoolSize,
	})
	return &runtime{
		exchanges:       exchanges,
		observer:        capture.NewObserver(exchanges),
		oidcAuth:        oidcAuth,
		tls:             tlsRuntime,
		directiveReader: directiveReader,
	}, nil
}

func newProxyHandler(cfg *config.Config, reader directive.RemoteReader, observer proxy.Observer, next http.Handler) http.Handler {
	transport := proxy.NewProxyAwareTransportWithOptions(http.DefaultTransport.(*http.Transport), proxy.ProxyTransportOptions{
		MaxIdleConns:        cfg.Proxy.Transport.MaxIdleConns,
		MaxIdleConnsPerHost: cfg.Proxy.Transport.MaxIdleConnsPerHost,
		MaxConnsPerHost:     cfg.Proxy.Transport.MaxConnsPerHost,
		IdleConnTimeout:     cfg.Proxy.Transport.IdleConnTimeout,
		DisableKeepAlives:   cfg.Proxy.Transport.DisableKeepAlives,
	})

	remoteConfig := cfg.Proxy.Directive.Remote
	return proxy.NewHandler(directive.NewResolver(directive.ResolverOptions{
		RemoteReader:  reader,
		LookupTimeout: remoteConfig.Timeout,
		MaxValueBytes: remoteConfig.MaxResponseBytes,
	}), transport, proxy.HandlerOptions{
		Observer: observer,
		Next:     next,
	})
}

func (rt *runtime) Close(_ context.Context) error {
	if rt == nil {
		return nil
	}
	if rt.tls != nil {
		rt.tls.Close()
		rt.tls = nil
	}
	if rt.directiveReader != nil {
		if err := rt.directiveReader.Close(); err != nil {
			return err
		}
		rt.directiveReader = nil
	}
	return nil
}

type tlsRuntime struct {
	config  *tls.Config
	manager *tlsreload.Manager
}

func newTLSRuntime(ctx context.Context, cfg tlsreload.Config) (*tlsRuntime, error) {
	if !cfg.Enabled {
		return &tlsRuntime{}, nil
	}

	manager, err := tlsreload.New(ctx, cfg, tlsreload.Options{
		MinVersion: httpTLSMinVersion,
		Logger:     slog.Default(),
	})
	if err != nil {
		return nil, err
	}

	return &tlsRuntime{
		config:  manager.TLSConfig(),
		manager: manager,
	}, nil
}

func (rt *tlsRuntime) Close() {
	if rt == nil || rt.manager == nil {
		return
	}
	rt.manager.Close()
	rt.manager = nil
}
