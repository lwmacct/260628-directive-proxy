package server

import (
	"net/http"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/config"
	"github.com/lwmacct/260628-llm-relay-dproxy/internal/proxydirective"
	"github.com/lwmacct/260628-llm-relay-dproxy/internal/proxyhttp"
	"github.com/lwmacct/260628-llm-relay-dproxy/internal/requestid"
	"github.com/lwmacct/260628-llm-relay-dproxy/internal/service"
)

func newServiceRuntime(cfg *config.Config) (*runtime, error) {
	idGen := requestid.NewGenerator()
	transport := proxyhttp.NewProxyAwareTransportWithOptions(http.DefaultTransport, proxyhttp.ProxyTransportOptions{
		MaxIdleConns:        cfg.Proxy.Transport.MaxIdleConns,
		MaxIdleConnsPerHost: cfg.Proxy.Transport.MaxIdleConnsPerHost,
		MaxConnsPerHost:     cfg.Proxy.Transport.MaxConnsPerHost,
		IdleConnTimeout:     cfg.Proxy.Transport.IdleConnTimeout,
		DisableKeepAlives:   cfg.Proxy.Transport.DisableKeepAlives,
	})

	proxyHandler := proxyhttp.NewHandler(proxydirective.NewResolver(), transport, proxyhttp.HandlerOptions{
		IDGenerator: idGen,
	})
	proxy := service.NewProxyService(proxyHandler)

	return &runtime{
		transport: transport,
		idGen:     idGen,
		proxy:     proxy,
	}, nil
}
