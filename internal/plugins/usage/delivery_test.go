package usage

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/eventbus"
)

func TestDeliveryPublisherPostsBatchAndRemovesOn204(t *testing.T) {
	var gotContentType string
	var gotEvents []eventbus.Event
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&gotEvents); err != nil {
			t.Errorf("decode request failed: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	publisher, err := NewDeliveryPublisher(DeliveryOptions{
		URL:           server.URL,
		MaxBacklog:    10,
		BatchSize:     10,
		FlushInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("new publisher failed: %v", err)
	}
	defer publisher.Close(context.Background())

	if err := publisher.Publish(context.Background(), eventbus.Event{Type: EventTypeUsage, EventID: "evt-1"}); err != nil {
		t.Fatalf("publish failed: %v", err)
	}
	if err := publisher.Publish(context.Background(), eventbus.Event{Type: EventTypeUsage, EventID: "evt-2"}); err != nil {
		t.Fatalf("publish failed: %v", err)
	}
	if err := publisher.requestFlush(context.Background(), true); err != nil {
		t.Fatalf("flush failed: %v", err)
	}
	if gotContentType != "application/json" {
		t.Fatalf("unexpected content type: %q", gotContentType)
	}
	if len(gotEvents) != 2 || gotEvents[0].EventID != "evt-1" || gotEvents[1].EventID != "evt-2" {
		t.Fatalf("unexpected delivered events: %#v", gotEvents)
	}
	if got := publisher.backlogLen(); got != 0 {
		t.Fatalf("expected empty backlog, got %d", got)
	}
}

func TestDeliveryPublisherFlushesAutomaticallyAtBatchSize(t *testing.T) {
	delivered := make(chan []eventbus.Event, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var events []eventbus.Event
		if err := json.NewDecoder(r.Body).Decode(&events); err != nil {
			t.Errorf("decode request failed: %v", err)
		}
		delivered <- events
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	publisher, err := NewDeliveryPublisher(DeliveryOptions{
		URL:           server.URL,
		MaxBacklog:    10,
		BatchSize:     2,
		FlushInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("new publisher failed: %v", err)
	}
	defer publisher.Close(context.Background())

	if err := publisher.Publish(context.Background(), eventbus.Event{Type: EventTypeUsage, EventID: "evt-1"}); err != nil {
		t.Fatalf("publish failed: %v", err)
	}
	if err := publisher.Publish(context.Background(), eventbus.Event{Type: EventTypeUsage, EventID: "evt-2"}); err != nil {
		t.Fatalf("publish failed: %v", err)
	}

	select {
	case events := <-delivered:
		if len(events) != 2 {
			t.Fatalf("unexpected delivered events: %#v", events)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for batch delivery")
	}
}

func TestDeliveryPublisherFlushesPartialBatchOnInterval(t *testing.T) {
	delivered := make(chan []eventbus.Event, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var events []eventbus.Event
		if err := json.NewDecoder(r.Body).Decode(&events); err != nil {
			t.Errorf("decode request failed: %v", err)
		}
		delivered <- events
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	publisher, err := NewDeliveryPublisher(DeliveryOptions{
		URL:           server.URL,
		MaxBacklog:    10,
		BatchSize:     10,
		FlushInterval: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("new publisher failed: %v", err)
	}
	defer publisher.Close(context.Background())

	if err := publisher.Publish(context.Background(), eventbus.Event{Type: EventTypeUsage, EventID: "evt-1"}); err != nil {
		t.Fatalf("publish failed: %v", err)
	}

	select {
	case events := <-delivered:
		if len(events) != 1 || events[0].EventID != "evt-1" {
			t.Fatalf("unexpected delivered events: %#v", events)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for interval delivery")
	}
}

func TestDeliveryPublisherKeepsBacklogUntil204(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	publisher, err := NewDeliveryPublisher(DeliveryOptions{
		URL:           server.URL,
		MaxBacklog:    10,
		BatchSize:     10,
		FlushInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("new publisher failed: %v", err)
	}
	defer publisher.Close(context.Background())

	if err := publisher.Publish(context.Background(), eventbus.Event{Type: EventTypeUsage, EventID: "evt-1"}); err != nil {
		t.Fatalf("publish failed: %v", err)
	}
	if err := publisher.requestFlush(context.Background(), true); err == nil {
		t.Fatal("expected non-204 flush failure")
	}
	if got := publisher.backlogLen(); got != 1 {
		t.Fatalf("expected retained backlog, got %d", got)
	}
	if err := publisher.requestFlush(context.Background(), true); err != nil {
		t.Fatalf("retry flush failed: %v", err)
	}
	if got := publisher.backlogLen(); got != 0 {
		t.Fatalf("expected empty backlog, got %d", got)
	}
}

func TestDeliveryPublisherDoesNotRetryEveryEventAfterFailure(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	publisher, err := NewDeliveryPublisher(DeliveryOptions{
		URL:           server.URL,
		MaxBacklog:    10,
		BatchSize:     2,
		FlushInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("new publisher failed: %v", err)
	}
	defer publisher.Close(context.Background())

	for i := 0; i < 5; i++ {
		if err := publisher.Publish(context.Background(), eventbus.Event{Type: EventTypeUsage, EventID: "evt"}); err != nil {
			t.Fatalf("publish failed: %v", err)
		}
	}
	time.Sleep(50 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected one failed delivery attempt before retry interval, got %d", got)
	}
	if got := publisher.backlogLen(); got != 5 {
		t.Fatalf("expected retained backlog, got %d", got)
	}
}

func TestDeliveryPublisherCapsBacklog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	publisher, err := NewDeliveryPublisher(DeliveryOptions{
		URL:           server.URL,
		MaxBacklog:    1,
		BatchSize:     10,
		FlushInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("new publisher failed: %v", err)
	}
	defer publisher.Close(context.Background())

	if err := publisher.Publish(context.Background(), eventbus.Event{Type: EventTypeUsage, EventID: "evt-1"}); err != nil {
		t.Fatalf("publish failed: %v", err)
	}
	if err := publisher.Publish(context.Background(), eventbus.Event{Type: EventTypeUsage, EventID: "evt-2"}); !errors.Is(err, ErrDeliveryBacklogFull) {
		t.Fatalf("expected backlog full error, got %v", err)
	}
	if got := publisher.backlogLen(); got != 1 {
		t.Fatalf("unexpected backlog len: %d", got)
	}
}
