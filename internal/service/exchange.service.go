package service

import (
	"context"
	"errors"
	"sync"

	"github.com/lwmacct/260628-directive-proxy/internal/core/exchange"
)

type ExchangeService struct {
	mu       sync.RWMutex
	settings exchange.Settings
	nextID   uint64
	total    uint64
	records  []exchange.Record
	writers  []exchange.Writer
}

func NewExchangeService(capacity int, maxBodyBytes int64, writers ...exchange.Writer) *ExchangeService {
	if capacity <= 0 {
		capacity = exchange.DefaultCapacity
	}
	if maxBodyBytes < 0 {
		maxBodyBytes = exchange.DefaultMaxBodyBytes
	}
	return &ExchangeService{
		settings: exchange.Settings{Capacity: capacity, MaxBodyBytes: maxBodyBytes},
		records:  make([]exchange.Record, 0, capacity),
		writers:  append([]exchange.Writer(nil), writers...),
	}
}

func (s *ExchangeService) Begin() (exchange.Capture, bool) {
	if s == nil {
		return exchange.Capture{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.settings.Enabled {
		return exchange.Capture{}, false
	}
	s.nextID++
	return exchange.Capture{ID: s.nextID, MaxBodyBytes: s.settings.MaxBodyBytes}, true
}

func (s *ExchangeService) Complete(ctx context.Context, record exchange.Record) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	s.total++
	if len(s.records) < s.settings.Capacity {
		s.records = append(s.records, utilCloneExchangeRecord(record))
	} else {
		copy(s.records, s.records[1:])
		s.records[len(s.records)-1] = utilCloneExchangeRecord(record)
	}
	writers := append([]exchange.Writer(nil), s.writers...)
	s.mu.Unlock()

	var errs []error
	for _, writer := range writers {
		if writer != nil {
			errs = append(errs, writer.Write(ctx, utilCloneExchangeRecord(record)))
		}
	}
	return errors.Join(errs...)
}

func (s *ExchangeService) Snapshot(limit int) exchange.Snapshot {
	if s == nil {
		return utilEmptyExchangeSnapshot()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshotLocked(limit)
}

func (s *ExchangeService) Get(id uint64) (exchange.Record, bool) {
	if s == nil {
		return exchange.Record{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := len(s.records) - 1; i >= 0; i-- {
		if s.records[i].ID == id {
			return utilCloneExchangeRecord(s.records[i]), true
		}
	}
	return exchange.Record{}, false
}

func (s *ExchangeService) Configure(enabled bool, capacity int, maxBodyBytes int64) exchange.Snapshot {
	if s == nil {
		return utilEmptyExchangeSnapshot()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settings.Enabled = enabled
	if capacity > 0 && capacity != s.settings.Capacity {
		s.resizeLocked(capacity)
	}
	if maxBodyBytes >= 0 {
		s.settings.MaxBodyBytes = maxBodyBytes
	}
	return s.snapshotLocked(0)
}

func (s *ExchangeService) Clear() exchange.Snapshot {
	if s == nil {
		return utilEmptyExchangeSnapshot()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.total = 0
	s.records = make([]exchange.Record, 0, s.settings.Capacity)
	return s.snapshotLocked(0)
}

func (s *ExchangeService) snapshotLocked(limit int) exchange.Snapshot {
	if limit <= 0 || limit > len(s.records) {
		limit = len(s.records)
	}
	items := make([]exchange.Record, 0, limit)
	for i := len(s.records) - 1; i >= 0 && len(items) < limit; i-- {
		items = append(items, utilCloneExchangeRecord(s.records[i]))
	}
	return exchange.Snapshot{Settings: s.settings, Total: s.total, Items: items}
}

func (s *ExchangeService) resizeLocked(capacity int) {
	if len(s.records) > capacity {
		s.records = append([]exchange.Record(nil), s.records[len(s.records)-capacity:]...)
	} else {
		s.records = append([]exchange.Record(nil), s.records...)
	}
	s.settings.Capacity = capacity
}
