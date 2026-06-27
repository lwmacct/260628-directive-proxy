package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/eventbus"
	"github.com/twmb/franz-go/pkg/kerr"
)

const (
	initialPublishRetryBackoff = 200 * time.Millisecond
	maxPublishRetryBackoff     = 5 * time.Second
)

type Publisher struct {
	producer          *Producer
	topicPrefix       string
	publishTimeout    time.Duration
	maxPublishRetries int
}

func NewPublisher(cfg Config) (*Publisher, error) {
	cfg = cfg.withDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	slog.Info("initializing kafka sink",
		"ensure_topics", cfg.EnsureTopics,
		"topic_prefix", strings.TrimSpace(cfg.TopicPrefix),
	)

	producer, err := NewProducer(cfg)
	if err != nil {
		return nil, err
	}
	publisher := &Publisher{
		producer:          producer,
		topicPrefix:       strings.TrimRight(strings.TrimSpace(cfg.TopicPrefix), "."),
		publishTimeout:    cfg.PublishTimeout,
		maxPublishRetries: cfg.MaxPublishRetries,
	}
	ensureCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := producer.EnsureTopics(ensureCtx, publisher.Topics()); err != nil {
		_ = producer.Close(context.Background())
		return nil, err
	}
	return publisher, nil
}

func (p *Publisher) Publish(ctx context.Context, event eventbus.Event) error {
	if p == nil || event.Type == "" {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	topic := p.Topic(event)
	if strings.TrimSpace(topic) == "" {
		return fmt.Errorf("unknown kafka topic for event type %q", event.Type)
	}
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	publishTimeout := p.publishTimeout
	if publishTimeout <= 0 {
		publishTimeout = 10 * time.Second
	}
	maxPublishRetries := p.maxPublishRetries
	if maxPublishRetries <= 0 {
		maxPublishRetries = 3
	}
	publishCtx, cancel := context.WithTimeout(ctx, publishTimeout)
	defer cancel()

	key := event.RequestID
	if key == "" {
		key = event.EventID
	}
	backoff := initialPublishRetryBackoff
	for attempt := 1; attempt <= maxPublishRetries+1; attempt++ {
		err = p.producer.ProduceSync(publishCtx, topic, key, data)
		if err == nil {
			return nil
		}
		slog.Error("kafka publish failed", "topic", topic, "event_type", event.Type, "event_id", event.EventID, "request_id", event.RequestID, "attempt", attempt, "error", err)
		if isPermanentPublishError(err) {
			slog.Warn("dropping kafka event after permanent publish failure", "topic", topic, "event_type", event.Type, "event_id", event.EventID, "request_id", event.RequestID, "attempt", attempt, "error", err)
			return err
		}
		if attempt > maxPublishRetries {
			slog.Warn("dropping kafka event after retry limit", "topic", topic, "event_type", event.Type, "event_id", event.EventID, "request_id", event.RequestID, "attempt", attempt, "error", err)
			return err
		}
		select {
		case <-publishCtx.Done():
			return publishCtx.Err()
		case <-time.After(backoff):
		}
		if backoff < maxPublishRetryBackoff {
			backoff *= 2
			if backoff > maxPublishRetryBackoff {
				backoff = maxPublishRetryBackoff
			}
		}
	}
	return err
}

func (p *Publisher) Topic(event eventbus.Event) string {
	class := event.Class()
	if class == "" {
		return ""
	}
	return p.topicPrefix + "." + string(class)
}

func (p *Publisher) Topics() []string {
	prefix := strings.TrimRight(strings.TrimSpace(p.topicPrefix), ".")
	if prefix == "" {
		return nil
	}
	return []string{
		prefix + "." + string(eventbus.ClassCapture),
		prefix + "." + string(eventbus.ClassStream),
		prefix + "." + string(eventbus.ClassUsage),
	}
}

func isPermanentPublishError(err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, kerr.MessageTooLarge),
		errors.Is(err, kerr.RecordListTooLarge),
		errors.Is(err, kerr.InvalidRecord),
		errors.Is(err, kerr.InvalidRecordState):
		return true
	default:
		return false
	}
}

func (p *Publisher) Close(ctx context.Context) error {
	if p == nil {
		return nil
	}
	var errs []error
	if p.producer != nil {
		if err := p.producer.Close(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("close kafka publisher: %w", errors.Join(errs...))
}

func NewSink(cfg Config) (*Publisher, error) {
	return NewPublisher(cfg)
}
