package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/lwmacct/260614-go-pkg-tlsreload/pkg/tlsreload"
	"github.com/lwmacct/260711-go-pkg-authme/pkg/authme"
	"github.com/lwmacct/260711-go-pkg-authme/pkg/authme/adapters/dexgithub"
	"github.com/lwmacct/260711-go-pkg-authme/pkg/authme/adapters/statictoken"
	"github.com/lwmacct/260714-go-pkg-fluent/pkg/fluent"

	"github.com/lwmacct/260628-directive-proxy/internal/adapter/recoveryhttp"
	"github.com/lwmacct/260628-directive-proxy/internal/config"
	"github.com/lwmacct/260628-directive-proxy/internal/core/bodystore"
	"github.com/lwmacct/260628-directive-proxy/internal/core/directive"
	"github.com/lwmacct/260628-directive-proxy/internal/core/event"
	"github.com/lwmacct/260628-directive-proxy/internal/core/exchange"
	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
	"github.com/lwmacct/260628-directive-proxy/internal/core/program"
	"github.com/lwmacct/260628-directive-proxy/internal/core/proxy"
	"github.com/lwmacct/260628-directive-proxy/internal/core/recovery"
	"github.com/lwmacct/260628-directive-proxy/internal/modules/capture"
	"github.com/lwmacct/260628-directive-proxy/internal/modules/llmperf"
	"github.com/lwmacct/260628-directive-proxy/internal/modules/llmusage"
)

const httpTLSMinVersion = tls.VersionTLS12

type runtime struct {
	exchangeFactory  *exchange.Manager
	bodyStore        *bodystore.Controller
	proxyTransport   http.RoundTripper
	programRuntime   *program.Runtime
	eventOutput      *event.Dispatcher
	adminAuth        *authme.Auth
	sourceAccess     *directiveSourceAccess
	tls              *tlsRuntime
	directiveRemotes *directiveRemotes
	recoveryHTTP     *recoveryhttp.Definition
	recoveryCompiler *recovery.Registry
}

func newRuntime(ctx context.Context, cfg *config.Server) (*runtime, error) {
	tlsRuntime, err := newTLSRuntime(ctx, cfg.HTTP.TLS)
	if err != nil {
		return nil, fmt.Errorf("configure tls: %w", err)
	}
	var sourceAccess *directiveSourceAccess
	if cfg.Proxy.Directive.SourceAccess.Enabled {
		sourceAccess, err = newDirectiveSourceAccess(ctx, cfg.Proxy.Directive.SourceAccess)
		if err != nil {
			_ = tlsRuntime.Close()
			return nil, fmt.Errorf("configure source access: %w", err)
		}
	}
	adminAuth, err := newAdminAuth(ctx, cfg.HTTP)
	if err != nil {
		if sourceAccess != nil {
			sourceAccess.Close()
		}
		_ = tlsRuntime.Close()
		return nil, fmt.Errorf("configure authentication: %w", err)
	}
	eventOutput, err := newEventDispatcher(ctx, cfg.Fluent)
	if err != nil {
		if sourceAccess != nil {
			sourceAccess.Close()
		}
		_ = tlsRuntime.Close()
		return nil, fmt.Errorf("configure event output: %w", err)
	}
	programRuntime, err := newProgramRuntime(eventOutput)
	if err != nil {
		if eventOutput != nil {
			_ = eventOutput.Close(context.Background())
		}
		if sourceAccess != nil {
			sourceAccess.Close()
		}
		_ = tlsRuntime.Close()
		return nil, fmt.Errorf("configure program runtime: %w", err)
	}
	exchangeFactory := exchange.NewManager(exchange.ManagerOptions{MaxRoundTrips: cfg.Proxy.Recovery.MaxRoundTripsLimit}, programRuntime)
	bodyStore := bodystore.New(bodystore.Config{
		MemoryMaxBytes:     cfg.Proxy.BodyStore.MemoryMaxBytes,
		MemoryPerBodyBytes: cfg.Proxy.BodyStore.MemoryPerBodyBytes,
		DiskMaxBytes:       cfg.Proxy.BodyStore.DiskMaxBytes,
		MaxBodyBytes:       cfg.Proxy.BodyStore.MaxBodyBytes,
		ChunkBytes:         cfg.Proxy.BodyStore.ChunkBytes,
		TempDir:            cfg.Proxy.BodyStore.TempDir,
	})
	baseTransport := proxy.NewProxyAwareTransportWithOptions(http.DefaultTransport.(*http.Transport), proxy.ProxyTransportOptions{
		MaxIdleConns:        cfg.Proxy.Transport.MaxIdleConns,
		MaxIdleConnsPerHost: cfg.Proxy.Transport.MaxIdleConnsPerHost,
		MaxConnsPerHost:     cfg.Proxy.Transport.MaxConnsPerHost,
		IdleConnTimeout:     cfg.Proxy.Transport.IdleConnTimeout,
	})
	recoveryHTTP := recoveryhttp.New(recoveryhttp.Options{
		MaxResponseBytes: cfg.Proxy.Recovery.MaxCallbackResponseBytes,
		MaxTimeout:       cfg.Proxy.Recovery.MaxCallbackTimeout,
	})
	recoveryCompiler, err := recovery.NewRegistry(recoveryHTTP)
	if err != nil {
		_ = recoveryHTTP.Close()
		programRuntime.Close()
		if eventOutput != nil {
			_ = eventOutput.Close(context.Background())
		}
		if sourceAccess != nil {
			sourceAccess.Close()
		}
		_ = tlsRuntime.Close()
		return nil, fmt.Errorf("configure recovery controllers: %w", err)
	}
	recoveryTransport, err := proxy.NewRecoveryTransport(baseTransport, proxy.RecoveryTransportOptions{
		MaxRecoveryRoundTrips: cfg.Proxy.Recovery.MaxRoundTripsLimit,
		MaxRecoveryElapsed:    cfg.Proxy.Recovery.MaxElapsedLimit,
		MaxRecoveryBodyBytes:  cfg.Proxy.Recovery.MaxCapturedBodyBytes,
	})
	if err != nil {
		_ = recoveryHTTP.Close()
		programRuntime.Close()
		if eventOutput != nil {
			_ = eventOutput.Close(context.Background())
		}
		if sourceAccess != nil {
			sourceAccess.Close()
		}
		_ = tlsRuntime.Close()
		return nil, fmt.Errorf("configure recovery transport: %w", err)
	}
	remoteConfig := cfg.Proxy.Directive.Remote
	directiveRemotes := newDirectiveRemotes(remoteConfig, cfg.Proxy.Transport)
	return &runtime{
		exchangeFactory:  exchangeFactory,
		bodyStore:        bodyStore,
		proxyTransport:   recoveryTransport,
		programRuntime:   programRuntime,
		eventOutput:      eventOutput,
		adminAuth:        adminAuth,
		sourceAccess:     sourceAccess,
		tls:              tlsRuntime,
		directiveRemotes: directiveRemotes,
		recoveryHTTP:     recoveryHTTP,
		recoveryCompiler: recoveryCompiler,
	}, nil
}

func newProgramRuntime(emission event.Provider) (*program.Runtime, error) {
	definitions := []module.Definition{capture.New(), llmusage.New(), llmperf.New()}
	return program.NewRuntime(definitions, emission)
}

func newEventDispatcher(ctx context.Context, fluentConfig fluent.Config) (*event.Dispatcher, error) {
	if !fluentConfig.Enabled {
		return nil, nil
	}
	return event.NewDispatcher(ctx, event.Config{
		Sink:            newFluentSink(fluentConfig),
		QueueMaxRecords: fluentConfig.Buffer.MaxEvents,
		QueueMaxBytes:   int64(fluentConfig.Buffer.MaxBytes),
	})
}

func newAdminAuth(ctx context.Context, cfg config.ServerHTTP) (*authme.Auth, error) {
	methods := make([]authme.Method, 0, 2)
	var authorizers []authme.Authorizer
	if cfg.AuthMe.StaticToken.Enabled {
		tokenConfig := cfg.AuthMe.StaticToken
		tokenMethod, err := statictoken.New(tokenConfig)
		if err != nil {
			return nil, err
		}
		methods = append(methods, tokenMethod)
	}
	if cfg.AuthMe.DexGitHub.Enabled {
		oidcMethod, err := dexgithub.New(ctx, cfg.AuthMe.DexGitHub)
		if err != nil {
			return nil, err
		}
		authorizer, err := dexgithub.NewUsernameAuthorizer(cfg.AuthMe.AllowedGitHubUsers)
		if err != nil {
			return nil, err
		}
		authorizers = append(authorizers, authorizer)
		methods = append(methods, oidcMethod)
	}
	return authme.New(authme.Config{Prefix: cfg.AuthMe.PathPrefix, Origins: cfg.AuthMe.Origins, Session: cfg.AuthMe.Session}, authme.WithMethods(methods...), authme.WithAuthorizer(authme.Chain(authorizers...)))
}

func newProxyHandler(cfg *config.Server, remotes *directiveRemotes, compiler program.Compiler, recoveryCompiler recovery.Compiler, exchangeFactory *exchange.Manager, bodyStore *bodystore.Controller, transport http.RoundTripper) http.Handler {
	remoteConfig := cfg.Proxy.Directive.Remote
	options := proxy.HandlerOptions{
		BodyStore:       bodyStore,
		BodyReadTimeout: cfg.Proxy.BodyStore.ReadTimeout,
	}
	if exchangeFactory != nil {
		options.ExchangeFactory = exchangeFactory
		options.TrackBeforeResolve = true
	}
	resolverOptions := directive.ResolverOptions{
		LookupTimeout:    remoteConfig.Timeout,
		MaxTokenBytes:    cfg.Proxy.Directive.MaxTokenBytes,
		TokenSecret:      cfg.Proxy.Directive.TokenSecret,
		Compiler:         compiler,
		RecoveryCompiler: recoveryCompiler,
	}
	if remotes != nil {
		resolverOptions.HTTPReader = remotes.http
		resolverOptions.RedisReader = remotes.redis
		resolverOptions.FileReader = remotes.file
	}
	return proxy.NewHandler(directive.NewResolver(resolverOptions), transport, options)
}

func (rt *runtime) Close(ctx context.Context) error {
	if rt == nil {
		return nil
	}
	var errs []error
	if rt.sourceAccess != nil {
		rt.sourceAccess.Close()
		rt.sourceAccess = nil
	}
	if rt.tls != nil {
		if err := rt.tls.Close(); err != nil {
			errs = append(errs, err)
		}
		rt.tls = nil
	}
	if rt.directiveRemotes != nil {
		if err := rt.directiveRemotes.Close(); err != nil {
			errs = append(errs, err)
		}
		rt.directiveRemotes = nil
	}
	if rt.recoveryHTTP != nil {
		if err := rt.recoveryHTTP.Close(); err != nil {
			errs = append(errs, err)
		}
		rt.recoveryHTTP = nil
		rt.recoveryCompiler = nil
	}
	if rt.programRuntime != nil {
		rt.programRuntime.Close()
		rt.programRuntime = nil
	}
	if rt.eventOutput != nil {
		if err := rt.eventOutput.Close(ctx); err != nil {
			errs = append(errs, err)
		}
		rt.eventOutput = nil
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
