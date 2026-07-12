package handler

import "time"

type ListProxyExchangesInputDTO struct {
	Limit int `query:"limit" minimum:"0" maximum:"10000" doc:"Maximum number of recent exchanges to return; zero returns all retained records"`
}

type ListProxyExchangesOutputDTO struct {
	Body ProxyExchangeSnapshotDTO
}

type GetProxyExchangeInputDTO struct {
	ID uint64 `path:"id" doc:"Proxy exchange record ID"`
}

type GetProxyExchangeOutputDTO struct {
	Body ProxyExchangeRecordDTO
}

type UpdateProxyExchangeSettingsInputDTO struct {
	Body UpdateProxyExchangeSettingsRequestDTO
}

type UpdateProxyExchangeSettingsRequestDTO struct {
	Enabled      bool   `json:"enabled" doc:"Whether to capture new proxy exchanges"`
	Capacity     *int   `json:"capacity,omitempty" minimum:"1" maximum:"10000" doc:"Maximum retained exchange records"`
	MaxBodyBytes *int64 `json:"max_body_bytes,omitempty" minimum:"0" maximum:"10485760" doc:"Maximum captured bytes per request or response body"`
}

type UpdateProxyExchangeSettingsOutputDTO struct {
	Body ProxyExchangeSnapshotDTO
}

type ClearProxyExchangesOutputDTO struct {
	Body ProxyExchangeSnapshotDTO
}

type ProxyExchangeSnapshotDTO struct {
	Enabled      bool                     `json:"enabled"`
	Capacity     int                      `json:"capacity"`
	MaxBodyBytes int64                    `json:"max_body_bytes"`
	Total        uint64                   `json:"total"`
	Items        []ProxyExchangeRecordDTO `json:"items"`
}

type ProxyExchangeRecordDTO struct {
	ID                     uint64               `json:"id"`
	StartedAt              time.Time            `json:"started_at"`
	CompletedAt            time.Time            `json:"completed_at"`
	DurationMillis         int64                `json:"duration_millis"`
	Method                 string               `json:"method"`
	Host                   string               `json:"host,omitempty"`
	URL                    string               `json:"url"`
	TargetURL              string               `json:"target_url,omitempty"`
	DirectiveSource        string               `json:"directive_source,omitempty"`
	DirectiveKey           string               `json:"directive_key,omitempty"`
	DirectiveLookupMillis  int64                `json:"directive_lookup_millis,omitempty"`
	StatusCode             int                  `json:"status_code"`
	RequestHeaders         map[string][]string  `json:"request_headers,omitempty"`
	OutboundRequestHeaders map[string][]string  `json:"outbound_request_headers,omitempty"`
	ResponseHeaders        map[string][]string  `json:"response_headers,omitempty"`
	RequestBody            ProxyExchangeBodyDTO `json:"request_body"`
	ResponseBody           ProxyExchangeBodyDTO `json:"response_body"`
}

type ProxyExchangeBodyDTO struct {
	Text          string `json:"text,omitempty"`
	Base64        string `json:"base64,omitempty"`
	Bytes         int64  `json:"bytes"`
	CapturedBytes int    `json:"captured_bytes"`
	Truncated     bool   `json:"truncated"`
}
