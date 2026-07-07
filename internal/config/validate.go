package config

import "strings"

func Validate(cfg Config) (Config, error) {
	if strings.TrimSpace(cfg.Server.HTTP.Listen) == "" {
		return cfg, ErrInvalidHTTP
	}
	if cfg.Server.HTTP.TLS.Enabled {
		cfg.Server.HTTP.TLS.CertFile = strings.TrimSpace(cfg.Server.HTTP.TLS.CertFile)
		cfg.Server.HTTP.TLS.KeyFile = strings.TrimSpace(cfg.Server.HTTP.TLS.KeyFile)
		if err := cfg.Server.HTTP.TLS.TLSReloadConfig().Validate(); err != nil {
			return cfg, ErrInvalidHTTP
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
