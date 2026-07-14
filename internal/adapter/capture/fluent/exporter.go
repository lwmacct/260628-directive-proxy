package fluentcapture

import (
	"fmt"
	"hash/fnv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fluent/fluent-logger-golang/fluent"

	"github.com/lwmacct/260628-directive-proxy/internal/core/capture"
)

type Config struct {
	Network               string
	Host                  string
	Port                  int
	SocketPath            string
	Connections           int
	Timeout               time.Duration
	WriteTimeout          time.Duration
	ReadTimeout           time.Duration
	RetryWaitMillis       int
	MaxRetry              int
	MaxRetryWaitMillis    int
	TagPrefix             string
	RequestAck            bool
	TLSInsecureSkipVerify bool
}

type Exporter struct {
	mu              sync.RWMutex
	clients         []*fluent.Fluent
	closed          bool
	healthy         atomic.Bool
	lastFailureNano atomic.Int64
}

func New(config Config) (*Exporter, error) {
	if config.Connections <= 0 {
		config.Connections = 1
	}
	clients := make([]*fluent.Fluent, 0, config.Connections)
	for range config.Connections {
		client, err := fluent.New(fluent.Config{
			FluentNetwork:         config.Network,
			FluentHost:            config.Host,
			FluentPort:            config.Port,
			FluentSocketPath:      config.SocketPath,
			Timeout:               config.Timeout,
			WriteTimeout:          config.WriteTimeout,
			ReadTimeout:           config.ReadTimeout,
			RetryWait:             config.RetryWaitMillis,
			MaxRetry:              config.MaxRetry,
			MaxRetryWait:          config.MaxRetryWaitMillis,
			TagPrefix:             strings.TrimSpace(config.TagPrefix),
			Async:                 false,
			SubSecondPrecision:    true,
			RequestAck:            config.RequestAck,
			TlsInsecureSkipVerify: config.TLSInsecureSkipVerify,
		})
		if err != nil {
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
	err := client.PostWithTime(tag, event.Time, event.Record())
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
