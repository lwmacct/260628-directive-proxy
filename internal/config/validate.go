package config

import (
	"fmt"
	"net/netip"
	"strings"

	"github.com/lwmacct/260711-go-pkg-authme/pkg/authme"
	"github.com/lwmacct/260711-go-pkg-authme/pkg/authme/adapters/dexgithub"
	"github.com/lwmacct/260713-go-pkg-sourceaccess/pkg/sourcehttp"
	"github.com/lwmacct/260714-go-pkg-fluent/pkg/fluent"
)

func Validate(cfg Server) (Server, error) {
	if strings.TrimSpace(cfg.HTTP.Listen) == "" {
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
	coreConfig := authme.Config{Prefix: cfg.HTTP.AuthMe.PathPrefix, Origins: cfg.HTTP.AuthMe.Origins, Session: cfg.HTTP.AuthMe.Session}
	validatedCore, err := coreConfig.Normalize()
	if err != nil {
		return cfg, fmt.Errorf("%w: authme: %w", ErrInvalidAuth, err)
	}
	cfg.HTTP.AuthMe.PathPrefix = validatedCore.Prefix
	cfg.HTTP.AuthMe.Origins = validatedCore.Origins
	cfg.HTTP.AuthMe.Session = validatedCore.Session
	if cfg.HTTP.AuthMe.StaticToken.Enabled {
		validatedToken, err := cfg.HTTP.AuthMe.StaticToken.Normalize()
		if err != nil {
			return cfg, fmt.Errorf("%w: authme static token: %w", ErrInvalidAuth, err)
		}
		cfg.HTTP.AuthMe.StaticToken = validatedToken
	}
	if cfg.HTTP.AuthMe.DexGitHub.Enabled {
		validatedAuth, err := cfg.HTTP.AuthMe.DexGitHub.Normalize()
		if err != nil {
			return cfg, fmt.Errorf("%w: authme dexgithub: %w", ErrInvalidAuth, err)
		}
		cfg.HTTP.AuthMe.DexGitHub = validatedAuth
		authorizer, err := dexgithub.NewUsernameAuthorizer(cfg.HTTP.AuthMe.AllowedGitHubUsers)
		if err != nil {
			return cfg, fmt.Errorf("%w: authme allowed GitHub users: %w", ErrInvalidAuth, err)
		}
		_ = authorizer
		for index, username := range cfg.HTTP.AuthMe.AllowedGitHubUsers {
			cfg.HTTP.AuthMe.AllowedGitHubUsers[index] = strings.ToLower(strings.TrimSpace(username))
		}
	}
	if !cfg.HTTP.AuthMe.StaticToken.Enabled && !cfg.HTTP.AuthMe.DexGitHub.Enabled {
		return cfg, fmt.Errorf("%w: authme: at least one authentication method must be enabled", ErrInvalidAuth)
	}
	if cfg.Proxy.Directive.SourceAccess.Enabled {
		validatedAccess, err := validateDirectiveSourceAccess(cfg.Proxy.Directive.SourceAccess)
		if err != nil {
			return cfg, ErrInvalidAccess
		}
		cfg.Proxy.Directive.SourceAccess = validatedAccess
	}
	if cfg.Proxy.Transport.MaxIdleConns <= 0 || cfg.Proxy.Transport.MaxIdleConnsPerHost <= 0 ||
		cfg.Proxy.Transport.MaxIdleConnsPerHost > cfg.Proxy.Transport.MaxIdleConns ||
		cfg.Proxy.Transport.MaxConnsPerHost < 0 || cfg.Proxy.Transport.IdleConnTimeout <= 0 {
		return cfg, ErrInvalidTransport
	}
	recoveryConfig := cfg.Proxy.Recovery
	if recoveryConfig.MaxAttemptsLimit < 1 || recoveryConfig.MaxElapsedLimit <= 0 ||
		recoveryConfig.MaxCallbackTimeout <= 0 || recoveryConfig.MaxCapturedBodyBytes <= 0 ||
		recoveryConfig.MaxCallbackResponseBytes <= 0 {
		return cfg, ErrInvalidRecovery
	}
	bodyStore := cfg.Proxy.BodyStore
	if bodyStore.MemoryMaxBytes <= 0 || bodyStore.MemoryPerBodyBytes <= 0 ||
		bodyStore.MemoryPerBodyBytes > bodyStore.MemoryMaxBytes || bodyStore.MaxBodyBytes <= 0 ||
		bodyStore.MemoryPerBodyBytes > bodyStore.MaxBodyBytes || bodyStore.DiskMaxBytes < bodyStore.MaxBodyBytes ||
		bodyStore.ChunkBytes < 4<<10 || bodyStore.ChunkBytes > 1<<20 ||
		strings.TrimSpace(bodyStore.TempDir) == "" || bodyStore.ReadTimeout <= 0 {
		return cfg, ErrInvalidBodyStore
	}
	validatedFluent, err := validateFluentOutput(cfg.Fluent)
	if err != nil {
		return cfg, ErrInvalidFluent
	}
	cfg.Fluent = validatedFluent
	remote := cfg.Proxy.Directive.Remote
	remote.File.Root = strings.TrimSpace(remote.File.Root)
	if strings.TrimSpace(cfg.Proxy.Directive.TokenSecret) == "" || cfg.Proxy.Directive.MaxTokenBytes <= 0 ||
		remote.Timeout <= 0 || remote.MaxPayloadBytes <= 0 ||
		remote.Redis.ClientCacheCapacity <= 0 || remote.Redis.ClientIdleTimeout < 0 || remote.Redis.PoolSize <= 0 ||
		remote.File.Root == "" {
		return cfg, ErrInvalidDirective
	}
	cfg.Proxy.Directive.Remote = remote
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
