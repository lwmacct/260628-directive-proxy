package server

import (
	"net/http"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/config"
	"github.com/lwmacct/260628-llm-relay-dproxy/internal/proxy"
	"github.com/lwmacct/260628-llm-relay-dproxy/internal/proxydirective"
)

func newServiceRuntime(cfg *config.Config) (*runtime, error) {
	idGen := proxy.NewGenerator()
	transport := proxy.NewProxyAwareTransportWithOptions(http.DefaultTransport.(*http.Transport), proxy.ProxyTransportOptions{
		MaxIdleConns:        cfg.Proxy.Transport.MaxIdleConns,
		MaxIdleConnsPerHost: cfg.Proxy.Transport.MaxIdleConnsPerHost,
		MaxConnsPerHost:     cfg.Proxy.Transport.MaxConnsPerHost,
		IdleConnTimeout:     cfg.Proxy.Transport.IdleConnTimeout,
		DisableKeepAlives:   cfg.Proxy.Transport.DisableKeepAlives,
	})

	proxyHandler := proxy.NewHandler(proxydirective.NewResolver(), transport, proxy.HandlerOptions{
		IDGenerator: idGen,
	})

	return &runtime{
		transport: transport,
		idGen:     idGen,
		proxy:     proxyHandler,
	}, nil
}
