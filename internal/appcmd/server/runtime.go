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
	"github.com/lwmacct/260713-go-pkg-sourceaccess/pkg/sourceaccess"
	"github.com/lwmacct/260713-go-pkg-sourceaccess/pkg/sourcehttp"

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
	sourceAccess    *sourcehttp.Guard
	sourceEngine    *sourceaccess.Engine
	tls             *tlsRuntime
	directiveReader *directiveRemoteReader
}

func newRuntime(ctx context.Context, cfg *config.Config) (*runtime, error) {
	tlsRuntime, err := newTLSRuntime(ctx, cfg.Server.HTTP.TLS)
	if err != nil {
		return nil, fmt.Errorf("configure tls: %w", err)
	}
	sourceAccess, sourceEngine, err := newDirectiveSourceAccess(cfg.Proxy.Directive.SourceAccess)
	if err != nil {
		tlsRuntime.Close()
		return nil, fmt.Errorf("configure source access: %w", err)
	}
	oidcAuth, err := dexgithub.New(ctx, cfg.Server.HTTP.OIDCAuth, dexgithub.Options{})
	if err != nil {
		sourceEngine.Close()
		tlsRuntime.Close()
		return nil, fmt.Errorf("configure authentication: %w", err)
	}
	exchanges := service.NewExchangeService(exchange.DefaultCapacity, exchange.DefaultMaxBodyBytes)
	remoteConfig := cfg.Proxy.Directive.Remote
	directiveReader := newDirectiveRemoteReader(remoteConfig)
	return &runtime{
		exchanges:       exchanges,
		observer:        capture.NewObserver(exchanges),
		oidcAuth:        oidcAuth,
		sourceAccess:    sourceAccess,
		sourceEngine:    sourceEngine,
		tls:             tlsRuntime,
		directiveReader: directiveReader,
	}, nil
}

func newDirectiveSourceAccess(cfg config.DirectiveSourceAccess) (*sourcehttp.Guard, *sourceaccess.Engine, error) {
	policy, err := sourceaccess.CompileSources(cfg.AllowedSources)
	if err != nil || policy.Len() == 0 {
		return nil, nil, config.ErrInvalidAccess
	}
	engine, err := sourceaccess.NewEngine(sourceaccess.EngineConfig{DNS: cfg.DNS}, sourceaccess.EngineOptions{})
	if err != nil {
		return nil, nil, err
	}
	extractor, err := sourcehttp.NewExtractor(sourcehttp.Config{
		TrustedProxies: cfg.TrustedProxies,
		Headers:        sourcehttp.DefaultHeaders(),
	})
	if err != nil {
		engine.Close()
		return nil, nil, err
	}
	guard, err := sourcehttp.NewGuard(extractor, engine.Bind(policy), sourcehttp.GuardOptions{
		DeniedHandler: func(w http.ResponseWriter, _ *http.Request, result sourceaccess.Result) {
			code := result.Decision.Reason
			if code == "" {
				code = sourceaccess.ReasonSourceNotAllowed
			}
			proxy.WriteProxyErrorJSON(w, http.StatusForbidden, string(code), "directive: source access denied")
		},
	})
	if err != nil {
		engine.Close()
		return nil, nil, err
	}
	return guard, engine, nil
}

func newProxyHandler(cfg *config.Config, reader directive.RemoteReader, observer proxy.Observer) http.Handler {
	transport := proxy.NewProxyAwareTransportWithOptions(http.DefaultTransport.(*http.Transport), proxy.ProxyTransportOptions{
		MaxIdleConns:        cfg.Proxy.Transport.MaxIdleConns,
		MaxIdleConnsPerHost: cfg.Proxy.Transport.MaxIdleConnsPerHost,
		MaxConnsPerHost:     cfg.Proxy.Transport.MaxConnsPerHost,
		IdleConnTimeout:     cfg.Proxy.Transport.IdleConnTimeout,
		DisableKeepAlives:   cfg.Proxy.Transport.DisableKeepAlives,
	})

	remoteConfig := cfg.Proxy.Directive.Remote
	return proxy.NewHandler(directive.NewResolver(directive.ResolverOptions{
		RemoteReader:   reader,
		LookupTimeout:  remoteConfig.Timeout,
		MaxTokenBytes:  cfg.Proxy.Directive.MaxTokenBytes,
		MaxInlineBytes: cfg.Proxy.Directive.MaxInlineBytes,
	}), transport, proxy.HandlerOptions{
		Observer: observer,
	})
}

func (rt *runtime) Close(_ context.Context) error {
	if rt == nil {
		return nil
	}
	if rt.sourceEngine != nil {
		rt.sourceEngine.Close()
		rt.sourceEngine = nil
		rt.sourceAccess = nil
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
