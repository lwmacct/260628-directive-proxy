package server

import (
	"github.com/lwmacct/260628-directive-proxy/internal/config"
	"github.com/lwmacct/260628-directive-proxy/internal/core/bodystore"
)

func newTestBodyStore(cfg config.ProxyBodyStore) *bodystore.Controller {
	return bodystore.New(bodystore.Config{
		MemoryMaxBytes: cfg.MemoryMaxBytes, MemoryPerBodyBytes: cfg.MemoryPerBodyBytes,
		DiskMaxBytes: cfg.DiskMaxBytes, MaxBodyBytes: cfg.MaxBodyBytes,
		ChunkBytes: cfg.ChunkBytes, TempDir: cfg.TempDir,
	})
}
