package config

import "strings"

func Validate(cfg Config) (Config, error) {
	if strings.TrimSpace(cfg.Server.HTTP.Listen) == "" {
		return cfg, ErrInvalidHTTP
	}
	if cfg.Server.HTTP.TLS.Enabled {
		if strings.TrimSpace(cfg.Server.HTTP.TLS.CertFile) == "" || strings.TrimSpace(cfg.Server.HTTP.TLS.KeyFile) == "" {
			return cfg, ErrInvalidHTTP
		}
		if cfg.Server.HTTP.TLS.ReloadInterval <= 0 {
			cfg.Server.HTTP.TLS.ReloadInterval = DefaultConfig().Server.HTTP.TLS.ReloadInterval
		}
	}
	if strings.TrimSpace(cfg.Proxy.PathPrefix) == "" || !strings.HasPrefix(cfg.Proxy.PathPrefix, "/") {
		return cfg, ErrInvalidProxy
	}
	cfg.Proxy.PathPrefix = "/" + strings.Trim(strings.TrimSpace(cfg.Proxy.PathPrefix), "/")
	if cfg.Proxy.Transport.MaxIdleConns < 0 || cfg.Proxy.Transport.MaxIdleConnsPerHost < 0 ||
		cfg.Proxy.Transport.MaxConnsPerHost < 0 || cfg.Proxy.Transport.IdleConnTimeout < 0 {
		return cfg, ErrInvalidTransport
	}
	return cfg, nil
}
