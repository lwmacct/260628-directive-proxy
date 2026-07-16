package exchange

import (
	"net/http"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
)

type Manager struct {
	maxAttempts   int
	moduleRuntime *module.Runtime
}

func NewManager(options ManagerOptions, moduleRuntime *module.Runtime) *Manager {
	if options.MaxAttempts < 1 {
		options.MaxAttempts = 1
	}
	return &Manager{maxAttempts: options.MaxAttempts, moduleRuntime: moduleRuntime}
}

func (manager *Manager) Start(req *http.Request) *Exchange {
	if manager == nil || req == nil {
		return nil
	}
	now := time.Now().UTC()
	return newExchange(manager, req, now)
}
