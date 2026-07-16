package handler

import (
	"github.com/lwmacct/260628-directive-proxy/internal/core/event"
	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
)

type Services struct {
	ExchangeQuery    ExchangeQuery
	ExchangeCommands ExchangeCommands
	Modules          module.HealthProvider
	EventOutput      event.HealthProvider
}
