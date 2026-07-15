package config

import (
	"net/url"
	"strings"

	"github.com/lwmacct/260711-go-pkg-httpauth/pkg/httpauth"
	"github.com/lwmacct/260711-go-pkg-httpauth/pkg/httpauth/oidc/dexgithub"
	"github.com/lwmacct/260713-go-pkg-sourceaccess/pkg/sourceaccess"
	"github.com/lwmacct/260713-go-pkg-sourceaccess/pkg/sourcehttp"
)

func Validate(cfg Config) (Config, error) {
	if strings.TrimSpace(cfg.Server.HTTP.Listen) == "" {
		return cfg, ErrInvalidHTTP
	}
	if cfg.Server.HTTP.MaxHeaderBytes <= 0 {
		return cfg, ErrInvalidHTTP
	}
	if cfg.Server.HTTP.TLS.Enabled {
		cfg.Server.HTTP.TLS.DefaultCertificate = strings.TrimSpace(cfg.Server.HTTP.TLS.DefaultCertificate)
		for index := range cfg.Server.HTTP.TLS.Certificates {
			source := &cfg.Server.HTTP.TLS.Certificates[index]
			source.ID = strings.TrimSpace(source.ID)
			source.Certificate = strings.TrimSpace(source.Certificate)
			source.PrivateKey = strings.TrimSpace(source.PrivateKey)
		}
		if err := cfg.Server.HTTP.TLS.ReloadConfig().Validate(); err != nil {
			return cfg, ErrInvalidHTTP
		}
	}
	if len(cfg.Server.HTTP.Auth.Methods) == 0 {
		return cfg, ErrInvalidAuth
	}
	validatedCore, err := (httpauth.Config{ExternalURLs: cfg.Server.HTTP.Auth.ExternalURLs, Session: cfg.Server.HTTP.Auth.Session}).Validate()
	if err != nil {
		return cfg, ErrInvalidAuth
	}
	cfg.Server.HTTP.Auth.ExternalURLs = validatedCore.ExternalURLs
	cfg.Server.HTTP.Auth.Session = validatedCore.Session
	seen := make(map[AuthMethod]struct{}, len(cfg.Server.HTTP.Auth.Methods))
	for _, method := range cfg.Server.HTTP.Auth.Methods {
		if _, exists := seen[method]; exists {
			return cfg, ErrInvalidAuth
		}
		seen[method] = struct{}{}
		switch method {
		case AuthMethodToken:
			if _, err := cfg.Server.HTTP.Auth.Token.Validate(); err != nil {
				return cfg, ErrInvalidAuth
			}
		case AuthMethodOIDC:
			validatedAuth, err := cfg.Server.HTTP.Auth.OIDC.MethodConfig().Validate()
			if err != nil {
				return cfg, ErrInvalidAuth
			}
			cfg.Server.HTTP.Auth.OIDC.Issuer = validatedAuth.Issuer
			cfg.Server.HTTP.Auth.OIDC.ClientID = validatedAuth.ClientID
			cfg.Server.HTTP.Auth.OIDC.ClientSecret = validatedAuth.ClientSecret
			cfg.Server.HTTP.Auth.OIDC.SessionTTL = validatedAuth.SessionTTL
			authorizer, err := dexgithub.NewUsernameAuthorizer(cfg.Server.HTTP.Auth.OIDC.AllowedUsers)
			if err != nil {
				return cfg, ErrInvalidAuth
			}
			_ = authorizer
			for index, username := range cfg.Server.HTTP.Auth.OIDC.AllowedUsers {
				cfg.Server.HTTP.Auth.OIDC.AllowedUsers[index] = strings.ToLower(strings.TrimSpace(username))
			}
		default:
			return cfg, ErrInvalidAuth
		}
	}
	if cfg.Proxy.Directive.SourceAccess.Enabled {
		validatedAccess, err := validateDirectiveSourceAccess(cfg.Proxy.Directive.SourceAccess)
		if err != nil {
			return cfg, ErrInvalidAccess
		}
		cfg.Proxy.Directive.SourceAccess = validatedAccess
	}
	if cfg.Proxy.Transport.MaxIdleConns < 0 || cfg.Proxy.Transport.MaxIdleConnsPerHost < 0 ||
		cfg.Proxy.Transport.MaxConnsPerHost < 0 || cfg.Proxy.Transport.IdleConnTimeout < 0 {
		return cfg, ErrInvalidTransport
	}
	retry := cfg.Proxy.Retry
	if retry.MaxAttempts < 2 || retry.CommandRetention <= 0 {
		return cfg, ErrInvalidRetry
	}
	bodyMemory := cfg.Proxy.BodyMemory
	if bodyMemory.MaxActiveBytes <= 0 || bodyMemory.MaxBodyBytes <= 0 ||
		bodyMemory.MaxBodyBytes > bodyMemory.MaxActiveBytes || bodyMemory.QueueMax <= 0 ||
		bodyMemory.QueueWait <= 0 || bodyMemory.ReadTimeout <= 0 {
		return cfg, ErrInvalidRetry
	}
	validatedObservability, err := validateObservability(cfg.Observability)
	if err != nil {
		return cfg, ErrInvalidObservability
	}
	cfg.Observability = validatedObservability
	remote := cfg.Proxy.Directive.Remote
	if cfg.Proxy.Directive.MaxTokenBytes <= 0 || cfg.Proxy.Directive.MaxInlineBytes <= 0 ||
		cfg.Proxy.Directive.MaxInlineBytes > cfg.Proxy.Directive.MaxTokenBytes ||
		remote.Timeout <= 0 || remote.MaxResponseBytes <= 0 || remote.HTTP.MaxRequestBytes <= 0 ||
		remote.Redis.ClientCacheCapacity <= 0 || remote.Redis.ClientIdleTimeout < 0 || remote.Redis.PoolSize <= 0 {
		return cfg, ErrInvalidDirective
	}
	return cfg, nil
}

func validateObservability(cfg Observability) (Observability, error) {
	if !cfg.Fluent.Enabled {
		return cfg, nil
	}
	if cfg.Fluent.Connections <= 0 || cfg.Fluent.Queue.MaxRecords <= 0 || cfg.Fluent.Queue.MaxBytes <= 0 {
		return cfg, ErrInvalidObservability
	}
	validatedFluent, err := validateFluentOutput(cfg.Fluent)
	if err != nil {
		return cfg, err
	}
	cfg.Fluent = validatedFluent
	return cfg, nil
}

func validateFluentOutput(cfg FluentOutput) (FluentOutput, error) {
	fluent := &cfg
	fluent.Endpoint = strings.TrimSpace(fluent.Endpoint)
	fluent.Delivery = strings.ToLower(strings.TrimSpace(fluent.Delivery))
	fluent.TagPrefix = strings.Trim(strings.TrimSpace(fluent.TagPrefix), ".")
	if fluent.Connections <= 0 || fluent.ConnectTimeout <= 0 ||
		fluent.HandshakeTimeout <= 0 || fluent.WriteTimeout <= 0 || fluent.ACKTimeout <= 0 ||
		fluent.RetryMaxAttempts <= 0 || fluent.RetryMinBackoff <= 0 ||
		fluent.RetryMaxBackoff < fluent.RetryMinBackoff || fluent.TagPrefix == "" {
		return cfg, ErrInvalidObservability
	}
	endpoint, err := url.Parse(fluent.Endpoint)
	if err != nil || endpoint.Scheme == "" {
		return cfg, ErrInvalidObservability
	}
	switch strings.ToLower(endpoint.Scheme) {
	case "tcp", "tls", "ws", "wss":
		if endpoint.Host == "" {
			return cfg, ErrInvalidObservability
		}
	case "unix":
		if endpoint.Path == "" {
			return cfg, ErrInvalidObservability
		}
	default:
		return cfg, ErrInvalidObservability
	}
	if fluent.Delivery != FluentDeliveryUnconfirmed && fluent.Delivery != FluentDeliveryAtLeastOnce {
		return cfg, ErrInvalidObservability
	}
	return cfg, nil
}

func validateDirectiveSourceAccess(cfg DirectiveSourceAccess) (DirectiveSourceAccess, error) {
	policy, err := sourceaccess.CompileSources(cfg.AllowedSources)
	if err != nil || policy.Len() == 0 || cfg.DNS.Validate() != nil {
		return cfg, ErrInvalidAccess
	}
	rules := policy.Rules()
	cfg.AllowedSources = make([]string, len(rules))
	for index, rule := range rules {
		cfg.AllowedSources[index] = rule.Value
	}
	httpConfig, err := (sourcehttp.Config{
		TrustedProxies: cfg.TrustedProxies,
		Headers:        sourcehttp.DefaultHeaders(),
	}).Validate()
	if err != nil {
		return cfg, ErrInvalidAccess
	}
	cfg.TrustedProxies = httpConfig.TrustedProxies
	return cfg, nil
}
