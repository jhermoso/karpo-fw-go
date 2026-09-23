// Package cache defines generic caching contracts for Karpo services.
package cache

import (
	"context"
	"time"
)

// Cache is a generic, thread-safe cache contract for keys of type K and values of type V.
type Cache[K comparable, V any] interface {
	// Get retrieves an item by key. Returns the value and true if found and not expired.
	Get(ctx context.Context, key K) (V, bool)

	// Set stores an item with an optional TTL (time to live). If ttl <= 0, the item does not expire.
	Set(ctx context.Context, key K, value V, ttl time.Duration) error

	// Delete removes an item by key.
	Delete(ctx context.Context, key K) error

	// Clear flushes all entries in the cache.
	Clear(ctx context.Context) error
}
