package config

import (
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
		cfg.Server.HTTP.TLS.CertFile = strings.TrimSpace(cfg.Server.HTTP.TLS.CertFile)
		cfg.Server.HTTP.TLS.KeyFile = strings.TrimSpace(cfg.Server.HTTP.TLS.KeyFile)
		if err := cfg.Server.HTTP.TLS.Validate(); err != nil {
			return cfg, ErrInvalidHTTP
		}
	}
	if !cfg.Server.HTTP.Auth.Token.Enabled && !cfg.Server.HTTP.Auth.OIDC.Enabled {
		return cfg, ErrInvalidAuth
	}
	validatedCore, err := (httpauth.Config{ExternalURLs: cfg.Server.HTTP.Auth.ExternalURLs, Session: cfg.Server.HTTP.Auth.Session}).Validate()
	if err != nil {
		return cfg, ErrInvalidAuth
	}
	cfg.Server.HTTP.Auth.ExternalURLs = validatedCore.ExternalURLs
	cfg.Server.HTTP.Auth.Session = validatedCore.Session
	if cfg.Server.HTTP.Auth.Token.Enabled {
		_, err := cfg.Server.HTTP.Auth.Token.MethodConfig().Validate()
		if err != nil {
			return cfg, ErrInvalidAuth
		}
	}
	if cfg.Server.HTTP.Auth.OIDC.Enabled {
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
	remote := cfg.Proxy.Directive.Remote
	if cfg.Proxy.Directive.MaxTokenBytes <= 0 || cfg.Proxy.Directive.MaxInlineBytes <= 0 ||
		cfg.Proxy.Directive.MaxInlineBytes > cfg.Proxy.Directive.MaxTokenBytes ||
		remote.Timeout <= 0 || remote.MaxResponseBytes <= 0 || remote.HTTP.MaxRequestBytes <= 0 ||
		remote.Redis.ClientCacheCapacity <= 0 || remote.Redis.ClientIdleTimeout < 0 || remote.Redis.PoolSize <= 0 {
		return cfg, ErrInvalidDirective
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
