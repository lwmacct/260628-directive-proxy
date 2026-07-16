package server

import (
	"fmt"
	"net/http"

	"github.com/lwmacct/260628-directive-proxy/internal/config"
	"github.com/lwmacct/260628-directive-proxy/internal/core/bodystore"
	"github.com/lwmacct/260628-directive-proxy/internal/core/retry"
)

func newTestBodyStore(cfg config.ProxyBodyStore) *bodystore.Controller {
	return bodystore.New(bodystore.Config{
		MemoryMaxBytes:     cfg.MemoryMaxBytes,
		MemoryPerBodyBytes: cfg.MemoryPerBodyBytes,
		DiskMaxBytes:       cfg.DiskMaxBytes,
		MaxBodyBytes:       cfg.MaxBodyBytes,
		ChunkBytes:         cfg.ChunkBytes,
		TempDir:            cfg.TempDir,
	})
}

func setTestRetryID(req *http.Request, seed byte) string {
	retryID := fmt.Sprintf("01982d4f-7c2a-7%03x-8%03x-%012x", seed, seed, uint64(seed))
	req.Header.Set(retry.IDHeader, retryID)
	return retryID
}
