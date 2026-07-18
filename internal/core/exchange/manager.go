package exchange

import (
	"net/http"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/program"
)

type Manager struct {
	maxRoundTrips  int
	programRuntime *program.Runtime
}

func NewManager(options ManagerOptions, programRuntime *program.Runtime) *Manager {
	if options.MaxRoundTrips < 1 {
		options.MaxRoundTrips = 1
	}
	return &Manager{maxRoundTrips: options.MaxRoundTrips, programRuntime: programRuntime}
}

func (manager *Manager) Start(req *http.Request) *Exchange {
	if manager == nil || req == nil {
		return nil
	}
	now := time.Now().UTC()
	return newExchange(manager, req, now)
}
