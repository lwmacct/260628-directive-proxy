package config

import (
	"strings"

	"github.com/lwmacct/260711-go-pkg-httpauth/pkg/httpauth"
	"github.com/lwmacct/260711-go-pkg-httpauth/pkg/httpauth/oidc/dexgithub"
	"github.com/lwmacct/260713-go-pkg-sourceaccess/pkg/sourceaccess"
	"github.com/lwmacct/260713-go-pkg-sourceaccess/pkg/sourcehttp"
	"github.com/lwmacct/260714-go-pkg-fluent/pkg/fluent"
)

func Validate(cfg Server) (Server, error) {
	if strings.TrimSpace(cfg.HTTP.Listen) == "" {
		return cfg, ErrInvalidHTTP
	}
	if cfg.HTTP.MaxHeaderBytes <= 0 {
		return cfg, ErrInvalidHTTP
	}
	if cfg.HTTP.TLS.Enabled {
		cfg.HTTP.TLS.DefaultCertificate = strings.TrimSpace(cfg.HTTP.TLS.DefaultCertificate)
		for index := range cfg.HTTP.TLS.Certificates {
			source := &cfg.HTTP.TLS.Certificates[index]
			source.ID = strings.TrimSpace(source.ID)
			source.Certificate = strings.TrimSpace(source.Certificate)
			source.PrivateKey = strings.TrimSpace(source.PrivateKey)
		}
		if err := cfg.HTTP.TLS.Validate(); err != nil {
			return cfg, ErrInvalidHTTP
		}
	}
	if len(cfg.HTTP.Auth.Methods) == 0 {
		return cfg, ErrInvalidAuth
	}
	validatedCore, err := (httpauth.Config{ExternalURLs: cfg.HTTP.Auth.ExternalURLs, Session: cfg.HTTP.Auth.Session}).Validate()
	if err != nil {
		return cfg, ErrInvalidAuth
	}
	cfg.HTTP.Auth.ExternalURLs = validatedCore.ExternalURLs
	cfg.HTTP.Auth.Session = validatedCore.Session
	seen := make(map[AuthMethod]struct{}, len(cfg.HTTP.Auth.Methods))
	for _, method := range cfg.HTTP.Auth.Methods {
		if _, exists := seen[method]; exists {
			return cfg, ErrInvalidAuth
		}
		seen[method] = struct{}{}
		switch method {
		case AuthMethodToken:
			if _, err := cfg.HTTP.Auth.Token.Validate(); err != nil {
				return cfg, ErrInvalidAuth
			}
		case AuthMethodOIDC:
			validatedAuth, err := cfg.HTTP.Auth.OIDC.MethodConfig().Validate()
			if err != nil {
				return cfg, ErrInvalidAuth
			}
			cfg.HTTP.Auth.OIDC.Issuer = validatedAuth.Issuer
			cfg.HTTP.Auth.OIDC.ClientID = validatedAuth.ClientID
			cfg.HTTP.Auth.OIDC.ClientSecret = validatedAuth.ClientSecret
			cfg.HTTP.Auth.OIDC.SessionTTL = validatedAuth.SessionTTL
			authorizer, err := dexgithub.NewUsernameAuthorizer(cfg.HTTP.Auth.OIDC.AllowedUsers)
			if err != nil {
				return cfg, ErrInvalidAuth
			}
			_ = authorizer
			for index, username := range cfg.HTTP.Auth.OIDC.AllowedUsers {
				cfg.HTTP.Auth.OIDC.AllowedUsers[index] = strings.ToLower(strings.TrimSpace(username))
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
	validatedFluent, err := validateFluentOutput(cfg.Fluent)
	if err != nil {
		return cfg, ErrInvalidFluent
	}
	cfg.Fluent = validatedFluent
	remote := cfg.Proxy.Directive.Remote
	if cfg.Proxy.Directive.MaxTokenBytes <= 0 || cfg.Proxy.Directive.MaxInlineBytes <= 0 ||
		cfg.Proxy.Directive.MaxInlineBytes > cfg.Proxy.Directive.MaxTokenBytes ||
		remote.Timeout <= 0 || remote.MaxResponseBytes <= 0 || remote.HTTP.MaxRequestBytes <= 0 ||
		remote.Redis.ClientCacheCapacity <= 0 || remote.Redis.ClientIdleTimeout < 0 || remote.Redis.PoolSize <= 0 {
		return cfg, ErrInvalidDirective
	}
	return cfg, nil
}

func validateFluentOutput(cfg fluent.Config) (fluent.Config, error) {
	if !cfg.Enabled {
		return cfg, nil
	}
	cfg.Endpoint = strings.TrimSpace(cfg.Endpoint)
	cfg.TagPrefix = strings.Trim(strings.TrimSpace(cfg.TagPrefix), ".")
	if cfg.TagPrefix == "" || cfg.Validate() != nil {
		return cfg, ErrInvalidFluent
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
