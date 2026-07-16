package handler

import (
	"github.com/lwmacct/260628-directive-proxy/internal/core/event"
	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
	"github.com/lwmacct/260628-directive-proxy/internal/core/proxyrequest"
)

type Services struct {
	Requests    proxyrequest.Tracker
	Modules     module.HealthProvider
	EventOutput event.HealthProvider
}
