package server

import (
	"context"
	"fmt"
	"net/http"

	"github.com/lwmacct/260628-llm-relay-dproxy/internal/config"
	"github.com/lwmacct/260628-llm-relay-dproxy/internal/eventbus"
	"github.com/lwmacct/260628-llm-relay-dproxy/internal/infra/kafka"
	"github.com/lwmacct/260628-llm-relay-dproxy/internal/plugins/capture"
	"github.com/lwmacct/260628-llm-relay-dproxy/internal/plugins/usage"
	"github.com/lwmacct/260628-llm-relay-dproxy/internal/proxydirective"
	"github.com/lwmacct/260628-llm-relay-dproxy/internal/proxyhttp"
	"github.com/lwmacct/260628-llm-relay-dproxy/internal/service"
)

func newServiceRuntime(cfg *config.Config) (*runtime, error) {
	publisher, err := buildPublisher(cfg.Event.Kafka)
	if err != nil {
		return nil, err
	}

	idGen := eventbus.NewIDGenerator()
	baseTransport := proxyhttp.NewProxyAwareTransportWithOptions(http.DefaultTransport, proxyhttp.ProxyTransportOptions{
		MaxIdleConns:        cfg.Proxy.Transport.MaxIdleConns,
		MaxIdleConnsPerHost: cfg.Proxy.Transport.MaxIdleConnsPerHost,
		MaxConnsPerHost:     cfg.Proxy.Transport.MaxConnsPerHost,
		IdleConnTimeout:     cfg.Proxy.Transport.IdleConnTimeout,
		DisableKeepAlives:   cfg.Proxy.Transport.DisableKeepAlives,
	})

	var usageDelivery eventbus.Publisher
	usageSink := publisher
	if cfg.Plugins.Usage.Enabled {
		var err error
		usageSink, usageDelivery, err = buildUsageSink(publisher, cfg.Plugins.Usage.Delivery)
		if err != nil {
			_ = publisher.Close(context.Background())
			return nil, err
		}
		baseTransport = usage.NewTransport(baseTransport, usageSink, usage.Options{
			IDGenerator: idGen,
			Mode:        usage.Mode(cfg.Plugins.Usage.Mode),
			Fields:      cfg.Plugins.Usage.Fields,
		})
	}

	transport := capture.NewTransport(baseTransport, publisher, capture.Options{
		IDGenerator:     idGen,
		AbnormalCapture: cfg.Event.Kafka.Enabled && cfg.Event.Kafka.CaptureAbnormal,
	})
	proxyHandler := proxyhttp.NewHandler(proxydirective.NewResolver(), transport, proxyhttp.HandlerOptions{
		IDGenerator:     idGen,
		AbnormalCapture: cfg.Event.Kafka.Enabled && cfg.Event.Kafka.CaptureAbnormal,
		AbnormalSink:    publisher,
	})
	proxy := service.NewProxyService(proxyHandler)

	return &runtime{
		publisher:     publisher,
		usageDelivery: usageDelivery,
		transport:     transport,
		idGen:         idGen,
		proxy:         proxy,
	}, nil
}

func buildUsageSink(base eventbus.Publisher, cfg config.UsageDelivery) (eventbus.Publisher, eventbus.Publisher, error) {
	publishers := make([]eventbus.Publisher, 0, 2)
	if cfg.Kafka {
		publishers = append(publishers, base)
	}
	if !cfg.Enabled {
		return eventbus.NewMultiPublisher(publishers...), nil, nil
	}
	delivery, err := usage.NewDeliveryPublisher(usage.DeliveryOptions{
		URL:           cfg.URL,
		Token:         cfg.Token,
		MaxBacklog:    cfg.MaxBacklog,
		BatchSize:     cfg.BatchSize,
		FlushInterval: cfg.FlushInterval,
		Timeout:       cfg.Timeout,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("create usage delivery publisher: %w", err)
	}
	publishers = append(publishers, delivery)
	return eventbus.NewMultiPublisher(publishers...), delivery, nil
}

func buildPublisher(kafkaCfg config.Kafka) (eventbus.Publisher, error) {
	base := eventbus.Publisher(eventbus.NopPublisher{})
	if kafkaCfg.Enabled {
		publisher, err := kafka.NewPublisher(kafka.Config{
			Brokers:           kafkaCfg.BrokerList(),
			EnsureTopics:      kafkaCfg.EnsureTopics,
			TopicPrefix:       kafkaCfg.TopicPrefix,
			PublishTimeout:    kafkaCfg.PublishTimeout,
			MaxPublishRetries: kafkaCfg.MaxPublishRetries,
			SASL: kafka.SASL{
				Username: kafkaCfg.SASL.Username,
				Password: kafkaCfg.SASL.Password,
			},
		})
		if err != nil {
			return nil, fmt.Errorf("create kafka publisher: %w", err)
		}
		base = publisher
	}
	if !kafkaCfg.Enabled {
		return base, nil
	}
	return eventbus.NewAsyncPublisher(base, eventbus.AsyncOptions{
		QueueSize: config.DefaultEventQueueSize,
	}), nil
}
