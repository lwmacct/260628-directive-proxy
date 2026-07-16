package config

import (
	"net/netip"
	"strings"

	"github.com/lwmacct/260628-directive-proxy/internal/types"
	"github.com/lwmacct/260711-go-pkg-authme/pkg/authme"
	"github.com/lwmacct/260711-go-pkg-authme/pkg/authme/adapters/dexgithub"
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
	coreConfig := authme.Config{Origins: cfg.HTTP.Auth.Origins, Session: cfg.HTTP.Auth.Session}
	validatedCore, err := coreConfig.Normalize()
	if err != nil {
		return cfg, ErrInvalidAuth
	}
	cfg.HTTP.Auth.Origins = validatedCore.Origins
	cfg.HTTP.Auth.Session = validatedCore.Session
	seen := make(map[AuthMethod]struct{}, len(cfg.HTTP.Auth.Methods))
	for _, method := range cfg.HTTP.Auth.Methods {
		if _, exists := seen[method]; exists {
			return cfg, ErrInvalidAuth
		}
		seen[method] = struct{}{}
		switch method {
		case AuthMethodStaticToken:
			tokenConfig := cfg.HTTP.Auth.StaticToken
			if tokenConfig.Namespace == "" {
				tokenConfig.Namespace = types.AdminTokenNamespace
			}
			validatedToken, err := tokenConfig.Normalize()
			if err != nil {
				return cfg, ErrInvalidAuth
			}
			cfg.HTTP.Auth.StaticToken = validatedToken
		case AuthMethodDexGitHub:
			if cfg.HTTP.Auth.DexGitHub.SessionTTL <= 0 {
				return cfg, ErrInvalidAuth
			}
			validatedAuth, err := cfg.HTTP.Auth.DexGitHub.Normalize()
			if err != nil {
				return cfg, ErrInvalidAuth
			}
			cfg.HTTP.Auth.DexGitHub = validatedAuth
			authorizer, err := dexgithub.NewUsernameAuthorizer(cfg.HTTP.Auth.AllowedGitHubUsers)
			if err != nil {
				return cfg, ErrInvalidAuth
			}
			_ = authorizer
			for index, username := range cfg.HTTP.Auth.AllowedGitHubUsers {
				cfg.HTTP.Auth.AllowedGitHubUsers[index] = strings.ToLower(strings.TrimSpace(username))
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
	normalized, err := cfg.Config.Normalize()
	if err != nil || normalized.Enabled && len(normalized.Rules) == 0 {
		return cfg, ErrInvalidAccess
	}
	httpConfig := sourcehttp.Config{
		TrustedProxies: cfg.TrustedProxies,
		Headers:        []sourcehttp.Header{sourcehttp.HeaderForwarded, sourcehttp.HeaderXForwardedFor, sourcehttp.HeaderXRealIP},
	}
	if err := httpConfig.Validate(); err != nil {
		return cfg, ErrInvalidAccess
	}
	cfg.Config = normalized
	cfg.TrustedProxies = normalizeTrustedProxies(cfg.TrustedProxies)
	return cfg, nil
}

func normalizeTrustedProxies(values []string) []string {
	result := make([]string, len(values))
	for index, raw := range values {
		value := strings.TrimSpace(raw)
		if address, err := netip.ParseAddr(value); err == nil {
			address = address.WithZone("").Unmap()
			result[index] = netip.PrefixFrom(address, address.BitLen()).String()
			continue
		}
		prefix := netip.MustParsePrefix(value)
		address := prefix.Addr()
		bits := prefix.Bits()
		if address.Is4In6() {
			address = address.Unmap()
			bits -= 96
		}
		result[index] = netip.PrefixFrom(address.WithZone("").Unmap(), bits).Masked().String()
	}
	return result
}
