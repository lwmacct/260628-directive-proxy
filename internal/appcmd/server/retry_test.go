package server

import (
	"encoding/base64"
	"net/http"

	"github.com/lwmacct/260628-directive-proxy/internal/config"
	"github.com/lwmacct/260628-directive-proxy/internal/core/bodymemory"
	"github.com/lwmacct/260628-directive-proxy/internal/core/proxyrequest"
)

func newTestBodyMemory(cfg config.ProxyBodyMemory) *bodymemory.Controller {
	return bodymemory.New(bodymemory.Config{
		MaxActiveBytes: cfg.MaxActiveBytes,
		MaxBodyBytes:   cfg.MaxBodyBytes,
		QueueMax:       cfg.QueueMax,
		QueueWait:      cfg.QueueWait,
	})
}

func setTestRetryIdentity(req *http.Request, seed byte) (string, string) {
	requestID := base64.RawURLEncoding.EncodeToString(testBytes(seed, 16))
	capability := base64.RawURLEncoding.EncodeToString(testBytes(seed+32, 32))
	req.Header.Set(proxyrequest.RequestIDHeader, requestID)
	req.Header.Set(proxyrequest.RetryCapabilityHeader, capability)
	return requestID, capability
}

func testBytes(value byte, size int) []byte {
	data := make([]byte, size)
	for index := range data {
		data[index] = value
	}
	return data
}
