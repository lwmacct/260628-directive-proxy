package service

import (
	"context"
	"testing"

	"github.com/lwmacct/260628-directive-proxy/internal/core/exchange"
)

type writerFunc func(context.Context, exchange.Record) error

func (f writerFunc) Write(ctx context.Context, record exchange.Record) error {
	return f(ctx, record)
}

func TestExchangeServiceRetainsAndPublishesCompletedRecords(t *testing.T) {
	var published exchange.Record
	service := NewExchangeService(2, 16, writerFunc(func(_ context.Context, record exchange.Record) error {
		published = record
		return nil
	}))
	service.Configure(true, 0, -1)

	capture, enabled := service.Begin()
	if !enabled || capture.ID != 1 || capture.MaxBodyBytes != 16 {
		t.Fatalf("unexpected capture: %#v enabled=%t", capture, enabled)
	}
	record := exchange.Record{ID: capture.ID, Method: "POST", RequestHeaders: map[string][]string{"X-Test": {"one"}}, OutboundRequestHeaders: map[string][]string{"X-Outbound": {"two"}}}
	if err := service.Complete(context.Background(), record); err != nil {
		t.Fatalf("complete failed: %v", err)
	}

	record.RequestHeaders["X-Test"][0] = "mutated"
	record.OutboundRequestHeaders["X-Outbound"][0] = "mutated"
	snapshot := service.Snapshot(0)
	if snapshot.Total != 1 || len(snapshot.Items) != 1 || snapshot.Items[0].RequestHeaders["X-Test"][0] != "one" || snapshot.Items[0].OutboundRequestHeaders["X-Outbound"][0] != "two" {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
	if published.ID != capture.ID || published.Method != "POST" {
		t.Fatalf("unexpected published record: %#v", published)
	}
}

func TestExchangeServiceResizesAndClearsRetention(t *testing.T) {
	service := NewExchangeService(2, 16)
	service.Configure(true, 0, -1)
	for i := uint64(1); i <= 3; i++ {
		capture, _ := service.Begin()
		if err := service.Complete(context.Background(), exchange.Record{ID: capture.ID}); err != nil {
			t.Fatalf("complete failed: %v", err)
		}
	}

	snapshot := service.Snapshot(0)
	if len(snapshot.Items) != 2 || snapshot.Items[0].ID != 3 || snapshot.Items[1].ID != 2 {
		t.Fatalf("unexpected retention order: %#v", snapshot.Items)
	}
	cleared := service.Clear()
	if !cleared.Settings.Enabled || cleared.Total != 0 || len(cleared.Items) != 0 {
		t.Fatalf("unexpected cleared state: %#v", cleared)
	}
	next, enabled := service.Begin()
	if !enabled || next.ID != 4 {
		t.Fatalf("clear must not reuse record IDs: %#v enabled=%t", next, enabled)
	}
}

func TestExchangeServiceIsDisabledByDefault(t *testing.T) {
	service := NewExchangeService(2, 16)
	if _, enabled := service.Begin(); enabled {
		t.Fatal("capture must be disabled by default")
	}
}
