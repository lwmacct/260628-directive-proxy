package kafka

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
	"github.com/twmb/franz-go/pkg/sasl/scram"
)

type Producer struct {
	client              *kgo.Client
	brokers             []string
	saslEnabled         bool
	partitions          int32
	replicas            int16
	ensureTopic         bool
	produceRecordFn     func(context.Context, string, string, []byte, func(error))
	topicMissingFn      func(context.Context, string) (bool, error)
	ensureTopicExistsFn func(context.Context, string) error
	mu                  sync.Mutex
	closed              bool
	repairMu            sync.Mutex
	repairs             map[string]*topicRepair
}

var errTopicMissingNotConfirmed = errors.New("topic missing not confirmed")

type topicRepair struct {
	done chan struct{}
	err  error
}

func NewProducer(cfg Config) (*Producer, error) {
	opts := []kgo.Opt{kgo.SeedBrokers(cfg.Brokers...)}
	saslEnabled := cfg.SASL.Username != "" && cfg.SASL.Password != ""
	if saslEnabled {
		opts = append(opts, kgo.SASL(scram.Auth{
			User: cfg.SASL.Username,
			Pass: cfg.SASL.Password,
		}.AsSha256Mechanism()))
	}

	client, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, err
	}

	producer := &Producer{
		client:      client,
		brokers:     append([]string(nil), cfg.Brokers...),
		saslEnabled: saslEnabled,
		partitions:  cfg.TopicPartitions,
		replicas:    cfg.TopicReplicationFactor,
		ensureTopic: cfg.EnsureTopics,
		repairs:     make(map[string]*topicRepair),
	}
	producer.produceRecordFn = producer.produceRecord
	producer.topicMissingFn = producer.topicMissing
	producer.ensureTopicExistsFn = producer.ensureTopicExists
	if err := producer.ensureReady(); err != nil {
		_ = producer.Close(context.Background())
		return nil, err
	}
	return producer, nil
}

func (p *Producer) Produce(ctx context.Context, topic string, key string, data []byte, done func(error)) {
	if p == nil {
		if done != nil {
			done(nil)
		}
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()
	if closed {
		if done != nil {
			done(nil)
		}
		return
	}
	p.produce(ctx, topic, key, data, done, true)
}

func (p *Producer) ProduceSync(ctx context.Context, topic string, key string, data []byte) error {
	if ctx == nil {
		ctx = context.Background()
	}
	done := make(chan error, 1)
	p.Produce(ctx, topic, key, data, func(err error) {
		done <- err
	})
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	}
}

func (p *Producer) produce(ctx context.Context, topic string, key string, data []byte, done func(error), allowRecovery bool) {
	if p.produceRecordFn == nil {
		if done != nil {
			done(errors.New("kafka producer is not initialized"))
		}
		return
	}
	p.produceRecordFn(ctx, topic, key, data, func(err error) {
		if err == nil {
			if done != nil {
				done(nil)
			}
			return
		}
		if allowRecovery && p.ensureTopic && isTopicMissingProduceError(err) {
			if recoverErr := p.recreateTopicIfConfirmedMissingWithContext(ctx, topic); recoverErr == nil {
				slog.Warn("kafka topic recreated after confirmed missing", "topic", topic)
				p.produce(ctx, topic, key, data, done, false)
				return
			} else if !errors.Is(recoverErr, errTopicMissingNotConfirmed) {
				slog.Error("kafka topic recovery failed", "topic", topic, "error", recoverErr)
			}
		}
		if done != nil {
			done(err)
		}
	})
}

func (p *Producer) produceRecord(ctx context.Context, topic string, key string, data []byte, done func(error)) {
	kafkaRecord := &kgo.Record{
		Topic: topic,
		Value: data,
	}
	if key != "" {
		kafkaRecord.Key = []byte(key)
	}
	p.client.Produce(ctx, kafkaRecord, func(_ *kgo.Record, err error) {
		if done != nil {
			done(err)
		}
	})
}

func (p *Producer) Close(context.Context) error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	p.client.Close()
	return nil
}

func (p *Producer) ensureReady() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.client.Ping(ctx); err != nil {
		return err
	}
	return nil
}

func (p *Producer) EnsureTopics(ctx context.Context, topics []string) error {
	if !p.ensureTopic {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for _, topic := range topics {
		if err := p.ensureTopicExists(ctx, topic); err != nil {
			return err
		}
	}
	return nil
}

func (p *Producer) recreateTopicIfConfirmedMissing(topic string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return p.recreateTopicIfConfirmedMissingWithContext(ctx, topic)
}

func (p *Producer) recreateTopicIfConfirmedMissingWithContext(ctx context.Context, topic string) error {
	p.repairMu.Lock()
	if p.repairs == nil {
		p.repairs = make(map[string]*topicRepair)
	}
	if repair := p.repairs[topic]; repair != nil {
		wait := repair.done
		p.repairMu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-wait:
			return repair.err
		}
	}
	repair := &topicRepair{done: make(chan struct{})}
	p.repairs[topic] = repair
	p.repairMu.Unlock()

	err := p.confirmAndRecreateTopic(ctx, topic)

	p.repairMu.Lock()
	repair.err = err
	delete(p.repairs, topic)
	p.repairMu.Unlock()
	close(repair.done)
	return err
}

func (p *Producer) confirmAndRecreateTopic(ctx context.Context, topic string) error {
	if p.topicMissingFn == nil || p.ensureTopicExistsFn == nil {
		return errors.New("kafka producer recovery is not initialized")
	}
	missing, err := p.topicMissingFn(ctx, topic)
	if err != nil {
		return fmt.Errorf("confirm topic state: %w", err)
	}
	if !missing {
		return errTopicMissingNotConfirmed
	}
	if err := p.ensureTopicExistsFn(ctx, topic); err != nil {
		return fmt.Errorf("recreate topic: %w", err)
	}
	return nil
}

func (p *Producer) topicMissing(ctx context.Context, topicName string) (bool, error) {
	req := kmsg.NewPtrMetadataRequest()
	req.AllowAutoTopicCreation = false
	topic := kmsg.NewMetadataRequestTopic()
	topic.Topic = kmsg.StringPtr(topicName)
	req.Topics = append(req.Topics, topic)

	resp, err := req.RequestWith(ctx, p.client)
	if err != nil {
		return false, err
	}
	if len(resp.Topics) == 0 {
		return false, errors.New("metadata response returned no topics")
	}
	topicErr := kerr.ErrorForCode(resp.Topics[0].ErrorCode)
	switch {
	case topicErr == nil:
		return false, nil
	case errors.Is(topicErr, kerr.UnknownTopicOrPartition), errors.Is(topicErr, kerr.UnknownTopicID):
		return true, nil
	default:
		return false, topicErr
	}
}

func isTopicMissingProduceError(err error) bool {
	return errors.Is(err, kerr.UnknownTopicOrPartition) || errors.Is(err, kerr.UnknownTopicID)
}

func (p *Producer) ensureTopicExists(ctx context.Context, topicName string) error {
	req := kmsg.NewPtrCreateTopicsRequest()
	topic := kmsg.NewCreateTopicsRequestTopic()
	topic.Topic = topicName
	topic.NumPartitions = p.partitions
	topic.ReplicationFactor = p.replicas
	req.Topics = append(req.Topics, topic)

	resp, err := req.RequestWith(ctx, p.client)
	if err != nil {
		return err
	}
	if len(resp.Topics) == 0 {
		return errors.New("create topics response returned no topics")
	}
	if err := kerr.ErrorForCode(resp.Topics[0].ErrorCode); err != nil && !errors.Is(err, kerr.TopicAlreadyExists) {
		return err
	}
	slog.Info("kafka topic ready", "topic", topicName)
	return nil
}
