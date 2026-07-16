package server

import (
	"context"
	"crypto/tls"
	"errors"
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
	"github.com/lwmacct/260714-go-pkg-fluent/pkg/fluent"

	proxyrequestadapter "github.com/lwmacct/260628-directive-proxy/internal/adapter/proxyrequest"
	"github.com/lwmacct/260628-directive-proxy/internal/config"
	"github.com/lwmacct/260628-directive-proxy/internal/core/bodymemory"
	"github.com/lwmacct/260628-directive-proxy/internal/core/directive"
	"github.com/lwmacct/260628-directive-proxy/internal/core/observability"
	"github.com/lwmacct/260628-directive-proxy/internal/core/proxy"
	captureplugin "github.com/lwmacct/260628-directive-proxy/internal/plugin/capture"
	llmperfplugin "github.com/lwmacct/260628-directive-proxy/internal/plugin/llmperf"
	llmusageplugin "github.com/lwmacct/260628-directive-proxy/internal/plugin/llmusage"
	"github.com/lwmacct/260628-directive-proxy/internal/types"
)

const httpTLSMinVersion = tls.VersionTLS12

type runtime struct {
	requests        *proxyrequestadapter.ProxyRequestService
	bodyMemory      *bodymemory.Controller
	proxyTransport  http.RoundTripper
	observability   *observability.Pipeline
	adminAuth       *httpauth.Auth
	sourceAccess    *sourcehttp.Guard
	sourceEngine    *sourceaccess.Engine
	tls             *tlsRuntime
	directiveReader *directiveRemoteReader
}

func newRuntime(ctx context.Context, cfg *config.Server) (*runtime, error) {
	tlsRuntime, err := newTLSRuntime(ctx, cfg.HTTP.TLS)
	if err != nil {
		return nil, fmt.Errorf("configure tls: %w", err)
	}
	var sourceAccess *sourcehttp.Guard
	var sourceEngine *sourceaccess.Engine
	if cfg.Proxy.Directive.SourceAccess.Enabled {
		sourceAccess, sourceEngine, err = newDirectiveSourceAccess(cfg.Proxy.Directive.SourceAccess)
		if err != nil {
			_ = tlsRuntime.Close()
			return nil, fmt.Errorf("configure source access: %w", err)
		}
	}
	adminAuth, err := newAdminAuth(ctx, cfg.HTTP)
	if err != nil {
		if sourceEngine != nil {
			sourceEngine.Close()
		}
		_ = tlsRuntime.Close()
		return nil, fmt.Errorf("configure authentication: %w", err)
	}
	observationPipeline, err := newObservabilityPipeline(ctx, cfg.Fluent)
	if err != nil {
		if sourceEngine != nil {
			sourceEngine.Close()
		}
		_ = tlsRuntime.Close()
		return nil, fmt.Errorf("configure observability: %w", err)
	}
	requests := proxyrequestadapter.NewProxyRequestService(proxyrequestadapter.ProxyRequestOptions{
		MaxAttempts:      cfg.Proxy.Retry.MaxAttempts,
		CommandRetention: cfg.Proxy.Retry.CommandRetention,
	}, observationPipeline)
	bodyMemory := bodymemory.New(bodymemory.Config{
		MaxActiveBytes: cfg.Proxy.BodyMemory.MaxActiveBytes,
		MaxBodyBytes:   cfg.Proxy.BodyMemory.MaxBodyBytes,
		QueueMax:       cfg.Proxy.BodyMemory.QueueMax,
		QueueWait:      cfg.Proxy.BodyMemory.QueueWait,
	})
	baseTransport := proxy.NewProxyAwareTransportWithOptions(http.DefaultTransport.(*http.Transport), proxy.ProxyTransportOptions{
		MaxIdleConns:        cfg.Proxy.Transport.MaxIdleConns,
		MaxIdleConnsPerHost: cfg.Proxy.Transport.MaxIdleConnsPerHost,
		MaxConnsPerHost:     cfg.Proxy.Transport.MaxConnsPerHost,
		IdleConnTimeout:     cfg.Proxy.Transport.IdleConnTimeout,
		DisableKeepAlives:   cfg.Proxy.Transport.DisableKeepAlives,
	})
	retryTransport, err := proxy.NewRetryTransport(baseTransport, proxy.RetryTransportOptions{})
	if err != nil {
		_ = observationPipeline.Close(context.Background())
		if sourceEngine != nil {
			sourceEngine.Close()
		}
		_ = tlsRuntime.Close()
		return nil, fmt.Errorf("configure retry transport: %w", err)
	}
	remoteConfig := cfg.Proxy.Directive.Remote
	directiveReader := newDirectiveRemoteReader(remoteConfig)
	return &runtime{
		requests:        requests,
		bodyMemory:      bodyMemory,
		proxyTransport:  retryTransport,
		observability:   observationPipeline,
		adminAuth:       adminAuth,
		sourceAccess:    sourceAccess,
		sourceEngine:    sourceEngine,
		tls:             tlsRuntime,
		directiveReader: directiveReader,
	}, nil
}

func newObservabilityPipeline(ctx context.Context, fluentConfig fluent.Config) (*observability.Pipeline, error) {
	if !fluentConfig.Enabled {
		return observability.NewDisabledPipeline(), nil
	}
	plugins := []observability.Plugin{captureplugin.New(), llmusageplugin.New(), llmperfplugin.New()}
	sink := newFluentSink(fluentConfig)
	return observability.NewPipeline(ctx, plugins, observability.SinkConfig{
		Sink:            sink,
		QueueMaxRecords: fluentConfig.Buffer.MaxEvents,
		QueueMaxBytes:   int64(fluentConfig.Buffer.MaxBytes),
	})
}

func newAdminAuth(ctx context.Context, cfg config.ServerHTTP) (*httpauth.Auth, error) {
	methods := make([]httpauth.Method, 0, len(cfg.Auth.Methods))
	var authorizers []httpauth.Authorizer
	for _, configured := range cfg.Auth.Methods {
		switch configured {
		case config.AuthMethodToken:
			tokenMethod, err := statictoken.New(types.AdminTokenNamespace, cfg.Auth.Token)
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

func newProxyHandler(cfg *config.Server, reader directive.RemoteReader, tracker *proxyrequestadapter.ProxyRequestService, bodyMemory *bodymemory.Controller, transport http.RoundTripper) http.Handler {
	remoteConfig := cfg.Proxy.Directive.Remote
	options := proxy.HandlerOptions{
		BodyMemory:      bodyMemory,
		BodyReadTimeout: cfg.Proxy.BodyMemory.ReadTimeout,
	}
	if tracker != nil {
		options.Tracker = tracker
		options.TrackBeforeResolve = true
	}
	return proxy.NewHandler(directive.NewResolver(directive.ResolverOptions{
		RemoteReader:   reader,
		LookupTimeout:  remoteConfig.Timeout,
		MaxTokenBytes:  cfg.Proxy.Directive.MaxTokenBytes,
		MaxInlineBytes: cfg.Proxy.Directive.MaxInlineBytes,
	}), transport, options)
}

func (rt *runtime) Close(ctx context.Context) error {
	if rt == nil {
		return nil
	}
	var errs []error
	if rt.sourceEngine != nil {
		rt.sourceEngine.Close()
		rt.sourceEngine = nil
		rt.sourceAccess = nil
	}
	if rt.tls != nil {
		if err := rt.tls.Close(); err != nil {
			errs = append(errs, err)
		}
		rt.tls = nil
	}
	if rt.directiveReader != nil {
		if err := rt.directiveReader.Close(); err != nil {
			errs = append(errs, err)
		}
		rt.directiveReader = nil
	}
	if rt.observability != nil {
		if err := rt.observability.Close(ctx); err != nil {
			errs = append(errs, err)
		}
		rt.observability = nil
	}
	return errors.Join(errs...)
}

type tlsRuntime struct {
	config *tls.Config
	store  *tlsreload.Store
}

func newTLSRuntime(ctx context.Context, cfg tlsreload.Config) (*tlsRuntime, error) {
	if !cfg.Enabled {
		return &tlsRuntime{}, nil
	}

	store, err := tlsreload.New(ctx, cfg, tlsreload.WithLogger(slog.Default()))
	if err != nil {
		return nil, err
	}

	return &tlsRuntime{
		config: &tls.Config{
			MinVersion:     httpTLSMinVersion,
			GetCertificate: store.GetCertificate,
		},
		store: store,
	}, nil
}

func (rt *tlsRuntime) Close() error {
	if rt == nil || rt.store == nil {
		return nil
	}
	err := rt.store.Close()
	rt.store = nil
	return err
}
