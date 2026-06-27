package kafka

import (
	"context"
	"errors"
	"testing"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/eventbus"
	"github.com/twmb/franz-go/pkg/kerr"
)

func TestPublishUsesRequestIDAsKafkaKeyAndRoutesTopic(t *testing.T) {
	var gotKey string
	var gotTopic string
	sink := &Publisher{
		producer: &Producer{
			produceRecordFn: func(_ context.Context, topic string, key string, _ []byte, done func(error)) {
				gotTopic = topic
				gotKey = key
				done(nil)
			},
		},
		topicPrefix: "prod.proxy",
	}

	err := sink.Publish(context.Background(), eventbus.Event{
		Type:      eventbus.TypeCaptureRequest,
		EventID:   "evt-1",
		RequestID: "req-1",
	})
	if err != nil {
		t.Fatalf("unexpected publish error: %v", err)
	}
	if gotKey != "req-1" {
		t.Fatalf("unexpected kafka key: %q", gotKey)
	}
	if gotTopic != "prod.proxy.capture" {
		t.Fatalf("unexpected kafka topic: %q", gotTopic)
	}
}

func TestPublishRetriesUntilSuccess(t *testing.T) {
	var attempts int
	producer := &Producer{}
	producer.produceRecordFn = func(_ context.Context, _ string, _ string, _ []byte, done func(error)) {
		attempts++
		if attempts == 1 {
			done(errors.New("temporary publish failure"))
			return
		}
		done(nil)
	}

	sink := &Publisher{
		producer:    producer,
		topicPrefix: "prod.proxy",
	}

	err := sink.Publish(context.Background(), eventbus.Event{
		Type:      eventbus.TypeCaptureRequest,
		EventID:   "evt-1",
		RequestID: "req-1",
	})
	if err != nil {
		t.Fatalf("unexpected publish error: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("unexpected publish attempts: %d", attempts)
	}
}

func TestPublishDoesNotRetryPermanentError(t *testing.T) {
	var attempts int
	producer := &Producer{}
	producer.produceRecordFn = func(_ context.Context, _ string, _ string, _ []byte, done func(error)) {
		attempts++
		done(kerr.MessageTooLarge)
	}

	sink := &Publisher{
		producer:    producer,
		topicPrefix: "prod.proxy",
	}

	err := sink.Publish(context.Background(), eventbus.Event{
		Type:      eventbus.TypeCaptureRequest,
		EventID:   "evt-oversize",
		RequestID: "req-oversize",
	})
	if !errors.Is(err, kerr.MessageTooLarge) {
		t.Fatalf("expected message too large error, got %v", err)
	}
	if attempts != 1 {
		t.Fatalf("unexpected publish attempts: %d", attempts)
	}
}
