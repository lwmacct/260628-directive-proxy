package handler

import (
	"github.com/lwmacct/260628-directive-proxy/internal/core/capture"
	"github.com/lwmacct/260628-directive-proxy/internal/core/proxyrequest"
)

type Services struct {
	Requests proxyrequest.Tracker
	Capture  capture.HealthProvider
}
