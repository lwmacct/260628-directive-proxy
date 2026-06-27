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
	if cfg.Event.Kafka.Enabled {
		if len(cfg.Event.Kafka.BrokerList()) == 0 || strings.TrimSpace(cfg.Event.Kafka.TopicPrefix) == "" {
			return cfg, ErrInvalidKafka
		}
		if (strings.TrimSpace(cfg.Event.Kafka.SASL.Username) == "") != (strings.TrimSpace(cfg.Event.Kafka.SASL.Password) == "") {
			return cfg, ErrInvalidKafka
		}
	}
	if cfg.Plugins.Usage.Enabled {
		mode := strings.TrimSpace(cfg.Plugins.Usage.Mode)
		if mode == "" {
			cfg.Plugins.Usage.Mode = "include"
		}
		if mode != "" && mode != "include" && mode != "exclude" {
			return cfg, ErrInvalidUsage
		}
		if cfg.Plugins.Usage.Delivery.Enabled && strings.TrimSpace(cfg.Plugins.Usage.Delivery.URL) == "" {
			return cfg, ErrInvalidUsage
		}
	}
	return cfg, nil
}
