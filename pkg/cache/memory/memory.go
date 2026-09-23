// Package memory provides an in-memory, thread-safe implementation of cache.Cache.
package memory

import (
	"context"
	"sync"
	stdtime "time"

	"github.com/jhermoso/karpo-fw-go/pkg/cache"
	"github.com/jhermoso/karpo-fw-go/pkg/time"
	"github.com/jhermoso/karpo-fw-go/pkg/time/real"
)

type item[V any] struct {
	value     V
	expiresAt stdtime.Time
	hasTTL    bool
}

// MemoryCache is a thread-safe in-memory cache supporting TTL expiration.
type MemoryCache[K comparable, V any] struct {
	mu    sync.RWMutex
	items map[K]item[V]
	clock time.Clock
}

// New creates a MemoryCache using the real system clock.
func New[K comparable, V any]() *MemoryCache[K, V] {
	return NewWithClock[K, V](real.New())
}

// NewWithClock creates a MemoryCache with a custom Clock for deterministic testing.
func NewWithClock[K comparable, V any](clock time.Clock) *MemoryCache[K, V] {
	return &MemoryCache[K, V]{
		items: make(map[K]item[V]),
		clock: clock,
	}
}

// Get retrieves a value by key. If the item has expired, it is deleted and returns false.
func (c *MemoryCache[K, V]) Get(_ context.Context, key K) (V, bool) {
	c.mu.RLock()
	it, ok := c.items[key]
	c.mu.RUnlock()

	if !ok {
		var zero V
		return zero, false
	}

	if it.hasTTL && c.clock.Now().After(it.expiresAt) {
		// Clean up expired entry
		c.mu.Lock()
		delete(c.items, key)
		c.mu.Unlock()

		var zero V
		return zero, false
	}

	return it.value, true
}

// Set stores a value with an optional TTL.
func (c *MemoryCache[K, V]) Set(_ context.Context, key K, value V, ttl stdtime.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	it := item[V]{
		value: value,
	}
	if ttl > 0 {
		it.expiresAt = c.clock.Now().Add(ttl)
		it.hasTTL = true
	}

	c.items[key] = it
	return nil
}

// Delete removes an entry by key.
func (c *MemoryCache[K, V]) Delete(_ context.Context, key K) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.items, key)
	return nil
}

// Clear flushes all entries.
func (c *MemoryCache[K, V]) Clear(_ context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items = make(map[K]item[V])
	return nil
}

var _ cache.Cache[string, any] = (*MemoryCache[string, any])(nil)
