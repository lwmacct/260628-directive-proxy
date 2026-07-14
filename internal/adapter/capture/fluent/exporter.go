package fluentcapture

import (
	"context"
	"crypto/tls"
	"fmt"
	"hash/fnv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lwmacct/260714-go-pkg-fluent/pkg/fluent"

	"github.com/lwmacct/260628-directive-proxy/internal/core/capture"
)

type Config struct {
	Endpoint              string
	Connections           int
	QueueCapacity         int
	ConnectTimeout        time.Duration
	HandshakeTimeout      time.Duration
	WriteTimeout          time.Duration
	ACKTimeout            time.Duration
	RetryMaxAttempts      int
	RetryMinBackoff       time.Duration
	RetryMaxBackoff       time.Duration
	TagPrefix             string
	DeliveryAtLeastOnce   bool
	TLSInsecureSkipVerify bool
}

type Exporter struct {
	mu              sync.RWMutex
	clients         []*fluent.Client
	closed          bool
	healthy         atomic.Bool
	lastFailureNano atomic.Int64
}

func New(config Config) (*Exporter, error) {
	if config.Connections <= 0 {
		config.Connections = 1
	}
	clients := make([]*fluent.Client, 0, config.Connections)
	for range config.Connections {
		clientConfig := fluent.DefaultConfig(config.Endpoint)
		clientConfig.TagPrefix = config.TagPrefix
		clientConfig.Queue.Capacity = config.QueueCapacity
		clientConfig.Retry.MaxAttempts = config.RetryMaxAttempts
		clientConfig.Retry.MinBackoff = config.RetryMinBackoff
		clientConfig.Retry.MaxBackoff = config.RetryMaxBackoff
		clientConfig.Timeout.Connect = config.ConnectTimeout
		clientConfig.Timeout.Handshake = config.HandshakeTimeout
		clientConfig.Timeout.Write = config.WriteTimeout
		clientConfig.Timeout.ACK = config.ACKTimeout
		if config.DeliveryAtLeastOnce {
			clientConfig.Delivery = fluent.DeliveryAtLeastOnce
		}
		if config.TLSInsecureSkipVerify {
			clientConfig.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: true} //nolint:gosec // Explicit development-only option.
		}
		client, err := fluent.New(clientConfig)
		if err != nil {
			for _, opened := range clients {
				_ = opened.Close()
			}
			return nil, fmt.Errorf("create fluent capture exporter: %w", err)
		}
		if err := client.Connect(context.Background()); err != nil {
			_ = client.Close()
			for _, opened := range clients {
				_ = opened.Close()
			}
			return nil, fmt.Errorf("connect fluent capture exporter: %w", err)
		}
		clients = append(clients, client)
	}
	exporter := &Exporter{clients: clients}
	exporter.healthy.Store(true)
	return exporter, nil
}

func (e *Exporter) Emit(tag string, event capture.Event) error {
	if e == nil {
		return nil
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.closed || len(e.clients) == 0 {
		return fmt.Errorf("fluent capture exporter is closed")
	}
	client := e.clients[clientIndex(event.TraceID, len(e.clients))]
	err := client.Send(context.Background(), fluent.Event{Tag: tag, Time: event.Time, Record: event.Record()})
	if err != nil {
		e.healthy.Store(false)
		e.lastFailureNano.Store(time.Now().UTC().UnixNano())
	} else {
		e.healthy.Store(true)
	}
	return err
}

func (e *Exporter) CaptureHealth() capture.HealthStatus {
	if e == nil {
		return capture.HealthStatus{Status: "unavailable"}
	}
	e.mu.RLock()
	closed := e.closed
	e.mu.RUnlock()
	if closed {
		return capture.HealthStatus{Status: "unavailable"}
	}
	status := "ok"
	if !e.healthy.Load() {
		status = "degraded"
	}
	result := capture.HealthStatus{Status: status}
	if value := e.lastFailureNano.Load(); value > 0 {
		result.LastFailureAt = time.Unix(0, value).UTC()
	}
	return result
}

func (e *Exporter) Close() error {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil
	}
	e.closed = true
	var first error
	for _, client := range e.clients {
		if err := client.Close(); err != nil && first == nil {
			first = err
		}
	}
	e.clients = nil
	return first
}

func clientIndex(traceID string, count int) int {
	if count <= 1 {
		return 0
	}
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(traceID))
	return int(hasher.Sum32() % uint32(count))
}
