package service

import "github.com/lwmacct/260628-directive-proxy/internal/core/exchange"

func utilEmptyExchangeSnapshot() exchange.Snapshot {
	return exchange.Snapshot{Items: []exchange.Record{}}
}

func utilCloneExchangeRecord(record exchange.Record) exchange.Record {
	record.RequestHeaders = utilCloneExchangeHeaders(record.RequestHeaders)
	record.OutboundRequestHeaders = utilCloneExchangeHeaders(record.OutboundRequestHeaders)
	record.ResponseHeaders = utilCloneExchangeHeaders(record.ResponseHeaders)
	return record
}

func utilCloneExchangeHeaders(headers map[string][]string) map[string][]string {
	if len(headers) == 0 {
		return nil
	}
	clone := make(map[string][]string, len(headers))
	for name, values := range headers {
		clone[name] = append([]string(nil), values...)
	}
	return clone
}
