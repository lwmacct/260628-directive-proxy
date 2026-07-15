package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/lwmacct/260614-go-pkg-tlsreload/pkg/tlsreload"
	"github.com/lwmacct/260711-go-pkg-httpauth/pkg/httpauth"
	"github.com/lwmacct/260711-go-pkg-httpauth/pkg/httpauth/oidc"
	"github.com/lwmacct/260711-go-pkg-httpauth/pkg/httpauth/oidc/dexgithub"
	"github.com/lwmacct/260711-go-pkg-httpauth/pkg/httpauth/statictoken"
	"github.com/lwmacct/260713-go-pkg-sourceaccess/pkg/sourceaccess"
	"github.com/lwmacct/260713-go-pkg-sourceaccess/pkg/sourcehttp"

	proxyrequestadapter "github.com/lwmacct/260628-directive-proxy/internal/adapter/proxyrequest"
	"github.com/lwmacct/260628-directive-proxy/internal/config"
	"github.com/lwmacct/260628-directive-proxy/internal/core/bodymemory"
	"github.com/lwmacct/260628-directive-proxy/internal/core/directive"
	"github.com/lwmacct/260628-directive-proxy/internal/core/observability"
	"github.com/lwmacct/260628-directive-proxy/internal/core/proxy"
	fluentoutput "github.com/lwmacct/260628-directive-proxy/internal/output/fluent"
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
	observationPipeline, err := newObservabilityPipeline(ctx, cfg.Observability)
	if err != nil {
		if sourceEngine != nil {
			sourceEngine.Close()
		}
		tlsRuntime.Close()
		return nil, fmt.Errorf("configure observability: %w", err)
	}
	instanceID := cfg.Observability.InstanceID
	if instanceID == "" {
		instanceID, _ = os.Hostname()
	}
	requests := proxyrequestadapter.NewProxyRequestService(proxyrequestadapter.ProxyRequestOptions{
		MaxAttempts:      cfg.Proxy.Retry.MaxAttempts,
		CommandRetention: cfg.Proxy.Retry.CommandRetention,
		InstanceID:       instanceID,
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
		tlsRuntime.Close()
		return nil, fmt.Errorf("configure retry transport: %w", err)
	}
	remoteConfig := cfg.Proxy.Directive.Remote
	directiveReader := newDirectiveRemoteReader(remoteConfig)
	return &runtime{
		requests:        requests,
		bodyMemory:      bodyMemory,
		proxyTransport:  retryTransport,
		observability:   observationPipeline,
		controlAuth:     controlAuth,
		sourceAccess:    sourceAccess,
		sourceEngine:    sourceEngine,
		tls:             tlsRuntime,
		directiveReader: directiveReader,
	}, nil
}

func newObservabilityPipeline(ctx context.Context, cfg config.Observability) (*observability.Pipeline, error) {
	plugins := make([]observability.Plugin, 0, len(cfg.Plugins))
	for _, configured := range cfg.Plugins {
		switch configured.Type {
		case config.ObservationPluginCapture:
			if configured.Capture == nil {
				return nil, fmt.Errorf("capture plugin config is missing")
			}
			plugins = append(plugins, captureplugin.New(captureplugin.Config{
				Name: configured.Name, BodyChunkBytes: configured.Capture.BodyChunkBytes, MaxSSEEventBytes: configured.Capture.MaxSSEEventBytes,
				RedactHeaders: configured.Capture.RedactHeaders, RedactQuery: configured.Capture.RedactQuery,
				MaxRetainedResponseBytes: cfg.ResponseCaptureMemory.MaxRetainedBytes, ResponseOverflow: cfg.ResponseCaptureMemory.Overflow,
			}))
		case config.ObservationPluginLLMUsage:
			if configured.LLMUsage == nil {
				return nil, fmt.Errorf("llm usage plugin config is missing")
			}
			plugins = append(plugins, llmusageplugin.New(llmusageplugin.Config{
				Name: configured.Name, MaxSSEMetadataBytes: configured.LLMUsage.MaxSSEMetadataBytes,
				MaxResultBytes: configured.LLMUsage.MaxResultBytes, MaxNestingDepth: configured.LLMUsage.MaxNestingDepth,
			}))
		case config.ObservationPluginLLMPerf:
			if configured.LLMPerf == nil {
				return nil, fmt.Errorf("llm perf plugin config is missing")
			}
			plugins = append(plugins, llmperfplugin.New(llmperfplugin.Config{
				Name: configured.Name, MaxSSEMetadataBytes: configured.LLMPerf.MaxSSEMetadataBytes,
				MaxRetainedBytes: configured.LLMPerf.MaxRetainedBytes, MaxNestingDepth: configured.LLMPerf.MaxNestingDepth,
			}))
		default:
			return nil, fmt.Errorf("unsupported observation plugin %q", configured.Type)
		}
	}
	fluentConfig := cfg.Sink.Fluent
	output := fluentoutput.New(fluentoutput.Config{
		Endpoint: fluentConfig.Endpoint, Connections: fluentConfig.Connections,
		ClientQueueCapacity: fluentConfig.ClientQueueCapacity, ConnectTimeout: fluentConfig.ConnectTimeout,
		HandshakeTimeout: fluentConfig.HandshakeTimeout, WriteTimeout: fluentConfig.WriteTimeout,
		ACKTimeout: fluentConfig.ACKTimeout, RetryMaxAttempts: fluentConfig.RetryMaxAttempts,
		RetryMinBackoff: fluentConfig.RetryMinBackoff, RetryMaxBackoff: fluentConfig.RetryMaxBackoff,
		TagPrefix: fluentConfig.TagPrefix, DeliveryAtLeastOnce: fluentConfig.Delivery == config.FluentDeliveryAtLeastOnce,
		TLSInsecureSkipVerify: fluentConfig.TLSInsecureSkipVerify,
	})
	return observability.NewPipeline(ctx, plugins, observability.SinkConfig{Sink: output, Workers: cfg.Sink.Workers, QueueCapacity: cfg.Sink.Queue.Capacity, QueueMaxBytes: cfg.Sink.Queue.MaxBytes})
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

func newProxyHandler(cfg *config.Config, reader directive.RemoteReader, tracker *proxyrequestadapter.ProxyRequestService, bodyMemory *bodymemory.Controller, transport http.RoundTripper) http.Handler {
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
		rt.tls.Close()
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
