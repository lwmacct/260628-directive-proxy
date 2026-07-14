package config

import (
	"path"
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
	if retry.Enabled {
		if retry.RetryableAfter < 0 || retry.MaxAttempts < 2 || retry.MaxActiveRequests <= 0 || retry.MaxBodyBytes <= 0 ||
			retry.MaxInflightBytes < retry.MaxBodyBytes {
			return cfg, ErrInvalidRetry
		}
	} else {
		cfg.Proxy.Retry.MaxAttempts = 1
		if cfg.Proxy.Retry.MaxActiveRequests <= 0 {
			cfg.Proxy.Retry.MaxActiveRequests = 4096
		}
	}
	if cfg.Proxy.Capture.Enabled {
		validatedCapture, err := validateCapture(cfg.Proxy.Capture)
		if err != nil {
			return cfg, ErrInvalidCapture
		}
		cfg.Proxy.Capture = validatedCapture
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

func validateCapture(cfg ProxyCapture) (ProxyCapture, error) {
	if cfg.BodyChunkBytes <= 0 || cfg.MaxSSEEventBytes <= 0 || len(cfg.RedactHeaders) == 0 {
		return cfg, ErrInvalidCapture
	}
	for index, values := range [][]string{cfg.RedactHeaders, cfg.RedactQuery} {
		seen := make(map[string]struct{}, len(values))
		for itemIndex, value := range values {
			value = strings.ToLower(strings.TrimSpace(value))
			if value == "" {
				return cfg, ErrInvalidCapture
			}
			if _, err := path.Match(value, "capture-test-value"); err != nil {
				return cfg, ErrInvalidCapture
			}
			if _, exists := seen[value]; exists {
				return cfg, ErrInvalidCapture
			}
			seen[value] = struct{}{}
			values[itemIndex] = value
		}
		if index == 0 {
			cfg.RedactHeaders = values
		} else {
			cfg.RedactQuery = values
		}
	}
	fluent := &cfg.Fluent
	fluent.Network = strings.ToLower(strings.TrimSpace(fluent.Network))
	fluent.Host = strings.TrimSpace(fluent.Host)
	fluent.SocketPath = strings.TrimSpace(fluent.SocketPath)
	fluent.TagPrefix = strings.Trim(strings.TrimSpace(fluent.TagPrefix), ".")
	if fluent.Connections <= 0 || fluent.Timeout <= 0 || fluent.WriteTimeout <= 0 || fluent.ReadTimeout <= 0 ||
		fluent.RetryWaitMillis <= 0 || fluent.MaxRetry <= 0 || fluent.MaxRetryWaitMillis <= 0 || fluent.TagPrefix == "" {
		return cfg, ErrInvalidCapture
	}
	switch fluent.Network {
	case "unix":
		if fluent.SocketPath == "" {
			return cfg, ErrInvalidCapture
		}
	case "tcp", "tls":
		if fluent.Host == "" || fluent.Port <= 0 || fluent.Port > 65535 {
			return cfg, ErrInvalidCapture
		}
	default:
		return cfg, ErrInvalidCapture
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
