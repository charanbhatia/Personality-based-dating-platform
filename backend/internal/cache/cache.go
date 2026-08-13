// Package cache defines the small key/value port the matching domain uses for
// trait lookups, plus two implementations that need no infrastructure.
//
// Person C owns Redis (platform/redis). When their client lands, pass an adapter
// satisfying Cache and nothing in the domain changes. Memory is the default so a
// single API replica still gets the M2 trait cache; it is process-local, so TTLs
// are kept short enough that a stale read after a retake self-heals quickly.
package cache

import (
	"context"
	"math/rand"
	"sync"
	"time"
)

// Cache is a best-effort byte cache. Implementations must treat a miss as
// (nil, false, nil) and must never be load-bearing for correctness: every caller
// falls back to Postgres.
type Cache interface {
	Get(ctx context.Context, key string) ([]byte, bool, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Delete(ctx context.Context, keys ...string) error
}

// Noop disables caching. Useful in tests and when a replica set is running
// without Redis, where a process-local cache would serve inconsistent reads.
type Noop struct{}

func (Noop) Get(context.Context, string) ([]byte, bool, error)        { return nil, false, nil }
func (Noop) Set(context.Context, string, []byte, time.Duration) error { return nil }
func (Noop) Delete(context.Context, ...string) error                  { return nil }

// DefaultMaxEntries bounds the in-process cache so an unbounded key space (one
// entry per user) cannot grow into the heap.
const DefaultMaxEntries = 50_000

type entry struct {
	value     []byte
	expiresAt time.Time
}

// Memory is a TTL cache safe for concurrent use.
type Memory struct {
	mu         sync.RWMutex
	items      map[string]entry
	maxEntries int
	now        func() time.Time
}

// NewMemory builds an in-process cache. maxEntries <= 0 uses DefaultMaxEntries.
func NewMemory(maxEntries int) *Memory {
	if maxEntries <= 0 {
		maxEntries = DefaultMaxEntries
	}
	return &Memory{
		items:      make(map[string]entry),
		maxEntries: maxEntries,
		now:        time.Now,
	}
}

func (m *Memory) Get(_ context.Context, key string) ([]byte, bool, error) {
	m.mu.RLock()
	e, ok := m.items[key]
	m.mu.RUnlock()
	if !ok {
		return nil, false, nil
	}
	if m.now().After(e.expiresAt) {
		m.mu.Lock()
		// Re-check under the write lock: a concurrent Set may have refreshed it.
		if current, still := m.items[key]; still && !m.now().Before(current.expiresAt) {
			delete(m.items, key)
		}
		m.mu.Unlock()
		return nil, false, nil
	}
	// Copy so a caller mutating the slice cannot corrupt the cached entry.
	out := make([]byte, len(e.value))
	copy(out, e.value)
	return out, true, nil
}

func (m *Memory) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	if ttl <= 0 {
		return nil
	}
	stored := make([]byte, len(value))
	copy(stored, value)

	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.items) >= m.maxEntries {
		if _, replacing := m.items[key]; !replacing {
			m.evictLocked()
		}
	}
	m.items[key] = entry{value: stored, expiresAt: m.now().Add(ttl)}
	return nil
}

func (m *Memory) Delete(_ context.Context, keys ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, k := range keys {
		delete(m.items, k)
	}
	return nil
}

// Len reports the current entry count, including not-yet-swept expired items.
func (m *Memory) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.items)
}

// evictLocked drops expired entries and, if that frees nothing, a small random
// sample. Random eviction keeps Set O(1) without the bookkeeping of an LRU; for
// a same-size-per-key trait cache the hit-rate difference is not worth it.
func (m *Memory) evictLocked() {
	now := m.now()
	for k, e := range m.items {
		if now.After(e.expiresAt) {
			delete(m.items, k)
		}
	}
	if len(m.items) < m.maxEntries {
		return
	}
	target := len(m.items) / 100
	if target < 1 {
		target = 1
	}
	for k := range m.items {
		delete(m.items, k)
		target--
		if target <= 0 {
			return
		}
	}
}

// StartJanitor sweeps expired entries until ctx is cancelled, so keys that are
// never read again do not occupy memory until an eviction forces a scan.
func (m *Memory) StartJanitor(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	// Jitter the first tick so replicas started together do not sweep in lockstep.
	timer := time.NewTimer(interval/2 + time.Duration(rand.Int63n(int64(interval/2)+1)))
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			m.mu.Lock()
			now := m.now()
			for k, e := range m.items {
				if now.After(e.expiresAt) {
					delete(m.items, k)
				}
			}
			m.mu.Unlock()
			timer.Reset(interval)
		}
	}
}
