package fluentoutput

import (
	"context"
	"crypto/tls"
	"fmt"
	"hash/fnv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lwmacct/260714-go-pkg-fluent/pkg/fluent"

	"github.com/lwmacct/260628-directive-proxy/internal/core/observability"
)

type Config struct {
	Endpoint              string
	Connections           int
	ClientQueueCapacity   int
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

type Output struct {
	config          Config
	mu              sync.RWMutex
	clients         []*fluent.Client
	started         bool
	closed          bool
	healthy         atomic.Bool
	lastFailureNano atomic.Int64
}

func New(config Config) *Output {
	if config.Connections <= 0 {
		config.Connections = 1
	}
	if config.ClientQueueCapacity <= 0 {
		config.ClientQueueCapacity = 1024
	}
	return &Output{config: config}
}

func (o *Output) Start(ctx context.Context) error {
	if o == nil {
		return fmt.Errorf("fluent output is nil")
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.started {
		return nil
	}
	if o.closed {
		return fmt.Errorf("fluent output is closed")
	}
	clients := make([]*fluent.Client, 0, o.config.Connections)
	for range o.config.Connections {
		clientConfig := fluent.DefaultConfig(o.config.Endpoint)
		clientConfig.TagPrefix = o.config.TagPrefix
		clientConfig.Queue.Capacity = o.config.ClientQueueCapacity
		clientConfig.Retry.MaxAttempts = o.config.RetryMaxAttempts
		clientConfig.Retry.MinBackoff = o.config.RetryMinBackoff
		clientConfig.Retry.MaxBackoff = o.config.RetryMaxBackoff
		clientConfig.Timeout.Connect = o.config.ConnectTimeout
		clientConfig.Timeout.Handshake = o.config.HandshakeTimeout
		clientConfig.Timeout.Write = o.config.WriteTimeout
		clientConfig.Timeout.ACK = o.config.ACKTimeout
		if o.config.DeliveryAtLeastOnce {
			clientConfig.Delivery = fluent.DeliveryAtLeastOnce
		}
		if o.config.TLSInsecureSkipVerify {
			clientConfig.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: true} //nolint:gosec // Explicit development-only option.
		}
		client, err := fluent.New(clientConfig)
		if err != nil {
			closeClients(clients)
			return fmt.Errorf("create fluent client: %w", err)
		}
		if err := client.Connect(ctx); err != nil {
			_ = client.Close()
			closeClients(clients)
			return fmt.Errorf("connect fluent output: %w", err)
		}
		clients = append(clients, client)
	}
	o.clients = clients
	o.started = true
	o.healthy.Store(true)
	return nil
}

func (o *Output) Write(ctx context.Context, record observability.Record) error {
	if o == nil {
		return fmt.Errorf("fluent output is nil")
	}
	o.mu.RLock()
	defer o.mu.RUnlock()
	if !o.started || o.closed || len(o.clients) == 0 {
		return fmt.Errorf("fluent output is unavailable")
	}
	client := o.clients[clientIndex(record.TraceID, len(o.clients))]
	err := client.Send(ctx, fluent.Event{Tag: record.Topic, Time: record.Time, Record: record.Map()})
	if err != nil {
		o.healthy.Store(false)
		o.lastFailureNano.Store(time.Now().UTC().UnixNano())
	} else {
		o.healthy.Store(true)
	}
	return err
}

func (o *Output) Health() observability.HealthStatus {
	if o == nil {
		return observability.HealthStatus{Status: "unavailable"}
	}
	o.mu.RLock()
	available := o.started && !o.closed && len(o.clients) > 0
	o.mu.RUnlock()
	if !available {
		return observability.HealthStatus{Status: "unavailable"}
	}
	status := observability.HealthStatus{Status: "ok"}
	if !o.healthy.Load() {
		status.Status = "degraded"
	}
	if value := o.lastFailureNano.Load(); value > 0 {
		status.LastFailureAt = time.Unix(0, value).UTC()
	}
	return status
}

func (o *Output) Close(context.Context) error {
	if o == nil {
		return nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return nil
	}
	o.closed = true
	var first error
	for _, client := range o.clients {
		if err := client.Close(); err != nil && first == nil {
			first = err
		}
	}
	o.clients = nil
	return first
}

func closeClients(clients []*fluent.Client) {
	for _, client := range clients {
		_ = client.Close()
	}
}

func clientIndex(traceID string, count int) int {
	if count <= 1 {
		return 0
	}
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(traceID))
	return int(hasher.Sum32() % uint32(count))
}
