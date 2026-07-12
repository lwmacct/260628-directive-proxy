package handler

import "github.com/lwmacct/260628-llm-relay-dproxy/internal/core/exchange"

func ToProxyExchangeSnapshotDTO(snapshot exchange.Snapshot) ProxyExchangeSnapshotDTO {
	items := make([]ProxyExchangeRecordDTO, 0, len(snapshot.Items))
	for _, record := range snapshot.Items {
		items = append(items, ToProxyExchangeRecordDTO(record))
	}
	return ProxyExchangeSnapshotDTO{
		Enabled:      snapshot.Settings.Enabled,
		Capacity:     snapshot.Settings.Capacity,
		MaxBodyBytes: snapshot.Settings.MaxBodyBytes,
		Total:        snapshot.Total,
		Items:        items,
	}
}

func ToProxyExchangeRecordDTO(record exchange.Record) ProxyExchangeRecordDTO {
	return ProxyExchangeRecordDTO{
		ID:                     record.ID,
		StartedAt:              record.StartedAt,
		CompletedAt:            record.CompletedAt,
		DurationMillis:         record.DurationMillis,
		Method:                 record.Method,
		Host:                   record.Host,
		URL:                    record.URL,
		TargetURL:              record.TargetURL,
		DirectiveSource:        record.DirectiveSource,
		DirectiveKey:           record.DirectiveKey,
		DirectiveLookupMillis:  record.DirectiveLookupMillis,
		StatusCode:             record.StatusCode,
		RequestHeaders:         record.RequestHeaders,
		OutboundRequestHeaders: record.OutboundRequestHeaders,
		ResponseHeaders:        record.ResponseHeaders,
		RequestBody:            ToProxyExchangeBodyDTO(record.RequestBody),
		ResponseBody:           ToProxyExchangeBodyDTO(record.ResponseBody),
	}
}

func ToProxyExchangeBodyDTO(body exchange.Body) ProxyExchangeBodyDTO {
	return ProxyExchangeBodyDTO{
		Text:          body.Text,
		Base64:        body.Base64,
		Bytes:         body.Bytes,
		CapturedBytes: body.CapturedBytes,
		Truncated:     body.Truncated,
	}
}
