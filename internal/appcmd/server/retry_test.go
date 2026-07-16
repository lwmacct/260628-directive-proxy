package server

import (
	"fmt"
	"net/http"

	"github.com/lwmacct/260628-directive-proxy/internal/config"
	"github.com/lwmacct/260628-directive-proxy/internal/core/bodymemory"
	"github.com/lwmacct/260628-directive-proxy/internal/core/retry"
)

func newTestBodyMemory(cfg config.ProxyBodyMemory) *bodymemory.Controller {
	return bodymemory.New(bodymemory.Config{
		MaxActiveBytes: cfg.MaxActiveBytes,
		MaxBodyBytes:   cfg.MaxBodyBytes,
		QueueMax:       cfg.QueueMax,
		QueueWait:      cfg.QueueWait,
	})
}

func setTestRetryID(req *http.Request, seed byte) string {
	retryID := fmt.Sprintf("01982d4f-7c2a-7%03x-8%03x-%012x", seed, seed, uint64(seed))
	req.Header.Set(retry.IDHeader, retryID)
	return retryID
}
