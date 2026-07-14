package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/lwmacct/260614-go-pkg-tlsreload/pkg/tlsreload"
	"github.com/lwmacct/260711-go-pkg-httpauth/pkg/httpauth"
	"github.com/lwmacct/260711-go-pkg-httpauth/pkg/httpauth/oidc"
	"github.com/lwmacct/260711-go-pkg-httpauth/pkg/httpauth/oidc/dexgithub"
	"github.com/lwmacct/260711-go-pkg-httpauth/pkg/httpauth/statictoken"
	"github.com/lwmacct/260713-go-pkg-sourceaccess/pkg/sourceaccess"
	"github.com/lwmacct/260713-go-pkg-sourceaccess/pkg/sourcehttp"

	"github.com/lwmacct/260628-directive-proxy/internal/adapter/exchange/capture"
	"github.com/lwmacct/260628-directive-proxy/internal/config"
	"github.com/lwmacct/260628-directive-proxy/internal/core/directive"
	"github.com/lwmacct/260628-directive-proxy/internal/core/exchange"
	"github.com/lwmacct/260628-directive-proxy/internal/core/proxy"
	"github.com/lwmacct/260628-directive-proxy/internal/service"
	"github.com/lwmacct/260628-directive-proxy/internal/types"
)

const httpTLSMinVersion = tls.VersionTLS12

type runtime struct {
	exchanges       *service.ExchangeService
	observer        proxy.Observer
	controlAuth     *httpauth.Auth
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
	var sourceAccess *sourcehttp.Guard
	var sourceEngine *sourceaccess.Engine
	if cfg.Proxy.Directive.SourceAccess.Enabled {
		sourceAccess, sourceEngine, err = newDirectiveSourceAccess(cfg.Proxy.Directive.SourceAccess)
		if err != nil {
			tlsRuntime.Close()
			return nil, fmt.Errorf("configure source access: %w", err)
		}
	}
	controlAuth, err := newControlAuth(ctx, cfg.Server.HTTP)
	if err != nil {
		if sourceEngine != nil {
			sourceEngine.Close()
		}
		tlsRuntime.Close()
		return nil, fmt.Errorf("configure authentication: %w", err)
	}
	exchanges := service.NewExchangeService(exchange.DefaultCapacity, exchange.DefaultMaxBodyBytes)
	remoteConfig := cfg.Proxy.Directive.Remote
	directiveReader := newDirectiveRemoteReader(remoteConfig)
	return &runtime{
		exchanges:       exchanges,
		observer:        capture.NewObserver(exchanges),
		controlAuth:     controlAuth,
		sourceAccess:    sourceAccess,
		sourceEngine:    sourceEngine,
		tls:             tlsRuntime,
		directiveReader: directiveReader,
	}, nil
}

func newControlAuth(ctx context.Context, cfg config.ServerHTTP) (*httpauth.Auth, error) {
	methods := make([]httpauth.Method, 0, len(cfg.Auth.Methods))
	var authorizers []httpauth.Authorizer
	for _, configured := range cfg.Auth.Methods {
		switch configured {
		case config.AuthMethodToken:
			tokenMethod, err := statictoken.New(types.ControlTokenNamespace, cfg.Auth.Token)
			if err != nil {
				return nil, err
			}
			methods = append(methods, tokenMethod)
		case config.AuthMethodOIDC:
			oidcMethod, err := dexgithub.New(ctx, cfg.Auth.OIDC.MethodConfig(), oidc.Options{})
			if err != nil {
				return nil, err
			}
			authorizer, err := dexgithub.NewUsernameAuthorizer(cfg.Auth.OIDC.AllowedUsers)
			if err != nil {
				return nil, err
			}
			authorizers = append(authorizers, authorizer)
			methods = append(methods, oidcMethod)
		}
	}
	return httpauth.New(httpauth.Config{ExternalURLs: cfg.Auth.ExternalURLs, Session: cfg.Auth.Session}, methods, httpauth.Options{Authorizer: httpauth.AuthorizeAll(authorizers...)})
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
