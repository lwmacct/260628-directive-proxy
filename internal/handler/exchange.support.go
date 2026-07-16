package handler

import "github.com/lwmacct/260628-directive-proxy/internal/core/exchange"

type ExchangeQuery interface {
	ListActive() []exchange.Snapshot
	GetActive(string) (exchange.Snapshot, bool)
}

type ExchangeCommands interface {
	RetryByTraceID(string, int, exchange.Trigger) (exchange.RetryResult, error)
	RetryByRetryID([32]byte, int, exchange.Trigger) (exchange.RetryResult, error)
}
