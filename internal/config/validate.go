package config

import (
	"net/url"
	"strings"
)

func Validate(cfg Config) (Config, error) {
	if strings.TrimSpace(cfg.Server.HTTP.Listen) == "" {
		return cfg, ErrInvalidHTTP
	}
	if cfg.Server.HTTP.TLS.Enabled {
		cfg.Server.HTTP.TLS.CertFile = strings.TrimSpace(cfg.Server.HTTP.TLS.CertFile)
		cfg.Server.HTTP.TLS.KeyFile = strings.TrimSpace(cfg.Server.HTTP.TLS.KeyFile)
		if err := cfg.Server.HTTP.TLS.Validate(); err != nil {
			return cfg, ErrInvalidHTTP
		}
	}
	if err := validateAuth(&cfg.Server.HTTP.Auth); err != nil {
		return cfg, err
	}
	if cfg.Proxy.Transport.MaxIdleConns < 0 || cfg.Proxy.Transport.MaxIdleConnsPerHost < 0 ||
		cfg.Proxy.Transport.MaxConnsPerHost < 0 || cfg.Proxy.Transport.IdleConnTimeout < 0 {
		return cfg, ErrInvalidTransport
	}
	return cfg, nil
}

func validateAuth(cfg *ServerHTTPAuth) error {
	cfg.Issuer = strings.TrimRight(strings.TrimSpace(cfg.Issuer), "/")
	cfg.ClientID = strings.TrimSpace(cfg.ClientID)
	cfg.CallbackURL = strings.TrimSpace(cfg.CallbackURL)
	cfg.PublicURL = strings.TrimRight(strings.TrimSpace(cfg.PublicURL), "/")
	if cfg.Issuer == "" || cfg.ClientID == "" || cfg.CallbackURL == "" || cfg.PublicURL == "" || cfg.SessionTTL <= 0 {
		return ErrInvalidAuth
	}
	issuer, issuerErr := url.Parse(cfg.Issuer)
	callback, callbackErr := url.Parse(cfg.CallbackURL)
	public, publicErr := url.Parse(cfg.PublicURL)
	if issuerErr != nil || issuer.Scheme != "https" || issuer.Host == "" || issuer.RawQuery != "" || issuer.Fragment != "" ||
		callbackErr != nil || callback.Host == "" || callback.RawQuery != "" || callback.Fragment != "" ||
		(callback.Scheme != "https" && !(callback.Scheme == "http" && callback.Hostname() == "localhost")) ||
		publicErr != nil || public.Host == "" || public.RawQuery != "" || public.Fragment != "" || public.Path != "" ||
		(public.Scheme != "https" && !(public.Scheme == "http" && public.Hostname() == "localhost")) ||
		callback.Scheme != public.Scheme || !strings.EqualFold(callback.Hostname(), public.Hostname()) {
		return ErrInvalidAuth
	}
	seen := make(map[string]struct{}, len(cfg.AllowedUsers))
	for i := range cfg.AllowedUsers {
		cfg.AllowedUsers[i] = strings.ToLower(strings.TrimSpace(cfg.AllowedUsers[i]))
		if cfg.AllowedUsers[i] == "" {
			return ErrInvalidAuth
		}
		if _, exists := seen[cfg.AllowedUsers[i]]; exists {
			return ErrInvalidAuth
		}
		seen[cfg.AllowedUsers[i]] = struct{}{}
	}
	if len(seen) == 0 {
		return ErrInvalidAuth
	}
	return nil
}
