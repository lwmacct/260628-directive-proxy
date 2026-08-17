package server

import (
	"github.com/lwmacct/260628-directive-proxy/internal/config"
	"github.com/lwmacct/260628-directive-proxy/internal/core/bodystore"
)

const testHMACSecret = "test-directive-hmac-secret"

func newTestServerConfig() config.Server {
	cfg := config.DefaultConfig().Server
	cfg.Proxy.Directive.HMACSecret = testHMACSecret
	return cfg
}

func newTestBodyStore(cfg config.ProxyBodyStore) *bodystore.Controller {
	return bodystore.New(bodystore.Config{
		MemoryMaxBytes: cfg.MemoryMaxBytes, MaxBodyBytes: cfg.MaxBodyBytes,
		ChunkBytes: cfg.ChunkBytes, QueueMaxRequests: cfg.QueueMaxRequests,
	})
}
