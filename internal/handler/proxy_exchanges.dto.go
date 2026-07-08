package handler

import "github.com/lwmacct/260628-llm-relay-dproxy/internal/core/proxy"

type ListProxyExchangesInputDTO struct {
	Limit int `query:"limit" minimum:"0" maximum:"1000" doc:"Maximum number of completed records to return; 0 means all retained records"`
}

type ListProxyExchangesOutputDTO struct {
	Body proxy.ExchangeSnapshot
}
