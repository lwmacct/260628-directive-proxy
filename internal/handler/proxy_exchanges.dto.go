package handler

import "github.com/lwmacct/260628-llm-relay-dproxy/internal/core/proxy"

type ListProxyExchangesInputDTO struct {
	Limit int `query:"limit" minimum:"0" maximum:"1000" doc:"Maximum number of completed records to return; 0 means all retained records"`
}

type ListProxyExchangesOutputDTO struct {
	Body proxy.ExchangeSnapshot
}

type GetProxyExchangeInputDTO struct {
	ID uint64 `path:"id" doc:"Proxy exchange record ID"`
}

type GetProxyExchangeOutputDTO struct {
	Body proxy.ExchangeRecord
}

type UpdateProxyExchangeSettingsInputDTO struct {
	Body UpdateProxyExchangeSettingsRequestDTO
}

type UpdateProxyExchangeSettingsRequestDTO struct {
	Enabled      bool   `json:"enabled" doc:"Whether to capture new proxy exchanges"`
	Capacity     *int   `json:"capacity,omitempty" minimum:"1" maximum:"10000" doc:"Maximum retained exchange records"`
	MaxBodyBytes *int64 `json:"max_body_bytes,omitempty" minimum:"0" maximum:"10485760" doc:"Maximum captured bytes for each request or response body; 0 disables body capture"`
}

type UpdateProxyExchangeSettingsOutputDTO struct {
	Body proxy.ExchangeSnapshot
}

type ClearProxyExchangesOutputDTO struct {
	Body proxy.ExchangeSnapshot
}
