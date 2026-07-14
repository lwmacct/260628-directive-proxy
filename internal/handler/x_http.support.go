package handler

import (
	"github.com/lwmacct/260628-directive-proxy/internal/core/observability"
	"github.com/lwmacct/260628-directive-proxy/internal/core/proxyrequest"
)

type Services struct {
	Requests      proxyrequest.Tracker
	Observability observability.HealthProvider
}
