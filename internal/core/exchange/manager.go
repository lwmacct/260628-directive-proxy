package exchange

import (
	"crypto/subtle"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/lwmacct/260628-directive-proxy/internal/core/module"
	"github.com/lwmacct/260628-directive-proxy/internal/core/retry"
)

type Manager struct {
	mu                sync.RWMutex
	active            map[string]*Exchange
	byRetryID         map[[32]byte]*Exchange
	terminalByTrace   map[string]tombstone
	terminalByRetryID map[[32]byte]tombstone
	maxAttempts       int
	commandRetention  time.Duration
	moduleRuntime     *module.Runtime
}

type tombstone struct {
	results map[int]RetryResult
	expires time.Time
}

func NewManager(options ManagerOptions, moduleRuntime *module.Runtime) *Manager {
	if options.MaxAttempts < 1 {
		options.MaxAttempts = 1
	}
	if options.CommandRetention <= 0 {
		options.CommandRetention = time.Minute
	}
	return &Manager{
		active:            make(map[string]*Exchange),
		byRetryID:         make(map[[32]byte]*Exchange),
		terminalByTrace:   make(map[string]tombstone),
		terminalByRetryID: make(map[[32]byte]tombstone),
		maxAttempts:       options.MaxAttempts,
		commandRetention:  options.CommandRetention,
		moduleRuntime:     moduleRuntime,
	}
}

func (manager *Manager) Start(req *http.Request, identity retry.Identity) *Exchange {
	if manager == nil || req == nil {
		return nil
	}
	now := time.Now().UTC()
	current := newExchange(manager, req, identity, now)
	manager.mu.Lock()
	manager.pruneTerminalLocked(now)
	if identity.Valid() {
		digest := identity.Digest()
		_, terminalExists := manager.terminalByRetryID[digest]
		if _, exists := manager.byRetryID[digest]; exists || terminalExists {
			manager.mu.Unlock()
			current.closeRun()
			return nil
		}
		manager.byRetryID[digest] = current
	}
	manager.active[current.traceID] = current
	manager.mu.Unlock()
	return current
}

func (manager *Manager) ListActive() []Snapshot {
	if manager == nil {
		return []Snapshot{}
	}
	manager.mu.RLock()
	exchanges := make([]*Exchange, 0, len(manager.active))
	for _, current := range manager.active {
		exchanges = append(exchanges, current)
	}
	manager.mu.RUnlock()
	items := make([]Snapshot, 0, len(exchanges))
	for _, current := range exchanges {
		if item, ok := current.Snapshot(); ok {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		left := items[i].AttemptStartedAt
		if left.IsZero() {
			left = items[i].StartedAt
		}
		right := items[j].AttemptStartedAt
		if right.IsZero() {
			right = items[j].StartedAt
		}
		return left.Before(right)
	})
	return items
}

func (manager *Manager) GetActive(traceID string) (Snapshot, bool) {
	if manager == nil {
		return Snapshot{}, false
	}
	manager.mu.RLock()
	current := manager.active[traceID]
	manager.mu.RUnlock()
	if current == nil {
		return Snapshot{}, false
	}
	return current.Snapshot()
}

func (manager *Manager) RetryByTraceID(traceID string, expectedAttempt int, trigger Trigger) (RetryResult, error) {
	if manager == nil {
		return RetryResult{}, ErrNotFound
	}
	manager.mu.RLock()
	current := manager.active[traceID]
	if current != nil {
		manager.mu.RUnlock()
		return current.requestRetry(expectedAttempt, trigger)
	}
	tomb, exists := manager.terminalByTrace[traceID]
	manager.mu.RUnlock()
	if exists && time.Now().Before(tomb.expires) {
		if result, found := tomb.results[expectedAttempt]; found {
			return result, nil
		}
		return RetryResult{}, ErrAttemptChanged
	}
	return RetryResult{}, ErrNotFound
}

func (manager *Manager) RetryByRetryID(digest [32]byte, expectedAttempt int, trigger Trigger) (RetryResult, error) {
	if manager == nil {
		return RetryResult{}, ErrNotFound
	}
	manager.mu.RLock()
	current := manager.byRetryID[digest]
	if current != nil {
		manager.mu.RUnlock()
		stored := current.identity.Digest()
		if !current.identity.Valid() || subtle.ConstantTimeCompare(stored[:], digest[:]) != 1 {
			return RetryResult{}, ErrNotFound
		}
		return current.requestRetry(expectedAttempt, trigger)
	}
	tomb, exists := manager.terminalByRetryID[digest]
	manager.mu.RUnlock()
	if exists && time.Now().Before(tomb.expires) {
		if result, found := tomb.results[expectedAttempt]; found {
			return result, nil
		}
		return RetryResult{}, ErrAttemptChanged
	}
	return RetryResult{}, ErrNotFound
}

func (manager *Manager) remove(current *Exchange) {
	if manager == nil || current == nil {
		return
	}
	identity, results := current.terminalData()
	manager.mu.Lock()
	removed := false
	if manager.active[current.traceID] == current {
		delete(manager.active, current.traceID)
		removed = true
	}
	if identity.Valid() && manager.byRetryID[identity.Digest()] == current {
		delete(manager.byRetryID, identity.Digest())
	}
	if removed && (identity.Valid() || len(results) > 0) {
		tomb := tombstone{results: results, expires: time.Now().Add(manager.commandRetention)}
		if len(results) > 0 {
			manager.terminalByTrace[current.traceID] = tomb
		}
		if identity.Valid() {
			manager.terminalByRetryID[identity.Digest()] = tomb
		}
	}
	manager.mu.Unlock()
}

func (manager *Manager) pruneTerminalLocked(now time.Time) {
	for key, tomb := range manager.terminalByTrace {
		if !now.Before(tomb.expires) {
			delete(manager.terminalByTrace, key)
		}
	}
	for key, tomb := range manager.terminalByRetryID {
		if !now.Before(tomb.expires) {
			delete(manager.terminalByRetryID, key)
		}
	}
}
