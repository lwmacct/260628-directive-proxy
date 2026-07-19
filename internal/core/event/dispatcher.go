package event

import (
	"context"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	vmmetrics "github.com/VictoriaMetrics/metrics"
)

type queuedRecord struct {
	record Record
	size   int64
}

type Dispatcher struct {
	sink         Sink
	queues       []chan queuedRecord
	maxRecords   int64
	maxBytes     int64
	queuedBytes  atomic.Int64
	queuedCount  atomic.Int64
	dropped      atomic.Uint64
	sinkFailures atomic.Uint64
	lastFailNano atomic.Int64
	failed       atomic.Bool
	closed       atomic.Bool
	metricsOnce  sync.Once
	wg           sync.WaitGroup
}

func NewDispatcher(ctx context.Context, binding Config) (*Dispatcher, error) {
	if binding.Sink == nil {
		return nil, fmt.Errorf("event sink is nil")
	}
	if binding.Workers <= 0 {
		binding.Workers = 1
	}
	if binding.QueueMaxRecords <= 0 {
		binding.QueueMaxRecords = 1024
	}
	if binding.QueueMaxBytes <= 0 {
		binding.QueueMaxBytes = 64 << 20
	}
	if err := binding.Sink.Start(ctx); err != nil {
		return nil, fmt.Errorf("start event sink: %w", err)
	}
	runner := &Dispatcher{
		sink: binding.Sink, maxRecords: int64(binding.QueueMaxRecords), maxBytes: binding.QueueMaxBytes,
		queues: make([]chan queuedRecord, binding.Workers),
	}
	for index := range runner.queues {
		runner.queues[index] = make(chan queuedRecord, binding.QueueMaxRecords)
		runner.wg.Add(1)
		go runner.run(index, runner.queues[index])
	}
	return runner, nil
}

func (r *Dispatcher) RegisterMetrics(set *vmmetrics.Set, prefix string) {
	if r == nil || set == nil {
		return
	}
	r.metricsOnce.Do(func() {
		set.NewGauge(prefix+"event_output_enabled", func() float64 { return 1 })
		set.NewGauge(prefix+"event_output_queue_limit_records", func() float64 { return float64(r.maxRecords) })
		set.NewGauge(prefix+"event_output_queue_limit_bytes", func() float64 { return float64(r.maxBytes) })
		set.NewGauge(prefix+"event_output_queue_records", func() float64 { return float64(r.queuedCount.Load()) })
		set.NewGauge(prefix+"event_output_queue_bytes", func() float64 { return float64(r.queuedBytes.Load()) })
		set.NewGauge(prefix+"event_output_healthy", func() float64 {
			if r.sinkHealth().Status == "ok" {
				return 1
			}
			return 0
		})
		set.RegisterMetricsWriter(func(writer io.Writer) {
			vmmetrics.WriteCounterUint64(writer, prefix+"event_output_dropped_records_total", r.dropped.Load())
			vmmetrics.WriteCounterUint64(writer, prefix+"event_output_failures_total", r.sinkFailures.Load())
		})
	})
}

func (r *Dispatcher) enqueue(record Record, copyBorrowed bool) bool {
	if r == nil || r.closed.Load() {
		return false
	}
	size := estimateRecordBytes(record)
	if !r.reserve(size) {
		r.dropped.Add(1)
		return false
	}
	if copyBorrowed {
		record.Data = cloneBorrowedMap(record.Data)
	}
	record.retain()
	index := shard(record.TraceID, len(r.queues))
	select {
	case r.queues[index] <- queuedRecord{record: record, size: size}:
		return true
	default:
		r.queuedBytes.Add(-size)
		r.queuedCount.Add(-1)
		r.dropped.Add(1)
		record.release()
		return false
	}
}

func (r *Dispatcher) reserve(size int64) bool {
	for {
		current := r.queuedCount.Load()
		if current >= r.maxRecords {
			return false
		}
		if r.queuedCount.CompareAndSwap(current, current+1) {
			break
		}
	}
	for {
		current := r.queuedBytes.Load()
		if current+size > r.maxBytes {
			r.queuedCount.Add(-1)
			return false
		}
		if r.queuedBytes.CompareAndSwap(current, current+size) {
			return true
		}
	}
}

func (r *Dispatcher) run(shardIndex int, queue <-chan queuedRecord) {
	defer r.wg.Done()
	for item := range queue {
		err := r.sink.Write(context.Background(), shardIndex, item.record)
		item.record.release()
		r.queuedBytes.Add(-item.size)
		r.queuedCount.Add(-1)
		if err != nil {
			r.failed.Store(true)
			r.sinkFailures.Add(1)
			r.lastFailNano.Store(time.Now().UTC().UnixNano())
			slog.Error("event sink failed", "error", err)
		} else {
			r.failed.Store(false)
		}
	}
}

func cloneBorrowedMap(data map[string]any) map[string]any {
	if data == nil {
		return nil
	}
	cloned := make(map[string]any, len(data))
	for key, value := range data {
		cloned[key] = cloneBorrowedValue(value)
	}
	return cloned
}

func cloneBorrowedValue(value any) any {
	switch typed := value.(type) {
	case []byte:
		return append([]byte(nil), typed...)
	case map[string]any:
		return cloneBorrowedMap(typed)
	case []any:
		cloned := make([]any, len(typed))
		for index, item := range typed {
			cloned[index] = cloneBorrowedValue(item)
		}
		return cloned
	default:
		return value
	}
}

func (r *Dispatcher) sinkHealth() Status {
	status := r.sink.Health()
	if status.Status == "" {
		status.Status = "ok"
	}
	status.QueuedRecords = r.queuedCount.Load()
	status.QueuedBytes = r.queuedBytes.Load()
	status.DroppedRecords = r.dropped.Load()
	if r.failed.Load() || status.DroppedRecords > 0 {
		status.Status = "degraded"
	}
	if value := r.lastFailNano.Load(); value > 0 {
		last := time.Unix(0, value).UTC()
		if status.LastFailureAt.Before(last) {
			status.LastFailureAt = last
		}
	}
	return status
}

func (r *Dispatcher) Close(ctx context.Context) error {
	if r == nil || !r.closed.CompareAndSwap(false, true) {
		return nil
	}
	for _, queue := range r.queues {
		close(queue)
	}
	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		_ = r.sink.Close(ctx)
		return ctx.Err()
	}
	return r.sink.Close(ctx)
}

func shard(traceID string, count int) int {
	if count <= 1 {
		return 0
	}
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(traceID))
	return int(hasher.Sum32() % uint32(count))
}

func estimateRecordBytes(record Record) int64 {
	size := int64(len(record.SchemaVersion) + len(record.Producer) + len(record.Topic) + len(record.TraceID) + len(record.OccurredAt) + 64)
	return size + estimateValueBytes(record.Metadata) + estimateValueBytes(record.Data)
}

func estimateValueBytes(value any) int64 {
	switch typed := value.(type) {
	case nil:
		return 0
	case string:
		return int64(len(typed))
	case []byte:
		return int64(len(typed))
	case []string:
		var total int64
		for _, item := range typed {
			total += int64(len(item))
		}
		return total
	case map[string]any:
		var total int64
		for key, item := range typed {
			total += int64(len(key)) + estimateValueBytes(item)
		}
		return total
	case map[string]string:
		var total int64
		for key, item := range typed {
			total += int64(len(key) + len(item))
		}
		return total
	case map[string][]string:
		var total int64
		for key, items := range typed {
			total += int64(len(key)) + estimateValueBytes(items)
		}
		return total
	case []any:
		var total int64
		for _, item := range typed {
			total += estimateValueBytes(item)
		}
		return total
	default:
		return 32
	}
}
