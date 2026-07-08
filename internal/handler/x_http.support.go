package handler

import "github.com/lwmacct/260628-llm-relay-dproxy/internal/core/proxy"

type Config struct{}

type Services struct {
	Exchanges *proxy.ExchangeRecorder
}
