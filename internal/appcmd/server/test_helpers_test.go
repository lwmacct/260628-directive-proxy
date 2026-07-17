package server

import (
	"github.com/lwmacct/260628-directive-proxy/internal/config"
	"github.com/lwmacct/260628-directive-proxy/internal/core/bodystore"
)

const testDirectiveSecret = "test-directive-token-secret"

func newTestServerConfig() config.Server {
	cfg := config.DefaultConfig().Server
	cfg.Proxy.Directive.TokenSecret = testDirectiveSecret
	return cfg
}

func newTestBodyStore(cfg config.ProxyBodyStore) *bodystore.Controller {
	return bodystore.New(bodystore.Config{
		MemoryMaxBytes: cfg.MemoryMaxBytes, MemoryPerBodyBytes: cfg.MemoryPerBodyBytes,
		DiskMaxBytes: cfg.DiskMaxBytes, MaxBodyBytes: cfg.MaxBodyBytes,
		ChunkBytes: cfg.ChunkBytes, TempDir: cfg.TempDir,
	})
}
