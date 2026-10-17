// Package memory ofrece una implementación en memoria y segura entre goroutines de cache.Cache.
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

// MemoryCache es una caché en memoria, segura entre goroutines, con caducidad por TTL.
type MemoryCache[K comparable, V any] struct {
	mu    sync.RWMutex
	items map[K]item[V]
	clock time.Clock
}

// New crea una MemoryCache con el reloj real del sistema.
func New[K comparable, V any]() *MemoryCache[K, V] {
	return NewWithClock[K, V](real.New())
}

// NewWithClock crea una MemoryCache con un Clock propio para pruebas deterministas.
func NewWithClock[K comparable, V any](clock time.Clock) *MemoryCache[K, V] {
	return &MemoryCache[K, V]{
		items: make(map[K]item[V]),
		clock: clock,
	}
}

// Get recupera un valor por su clave. Si el elemento ha caducado, se borra y devuelve false.
func (c *MemoryCache[K, V]) Get(_ context.Context, key K) (V, bool) {
	c.mu.RLock()
	it, ok := c.items[key]
	c.mu.RUnlock()

	if !ok {
		var zero V
		return zero, false
	}

	if it.hasTTL && c.clock.Now().After(it.expiresAt) {
		// Limpia la entrada caducada
		c.mu.Lock()
		delete(c.items, key)
		c.mu.Unlock()

		var zero V
		return zero, false
	}

	return it.value, true
}

// Set guarda un valor con un TTL opcional.
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

// Delete quita una entrada por su clave.
func (c *MemoryCache[K, V]) Delete(_ context.Context, key K) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.items, key)
	return nil
}

// Clear vacía todas las entradas.
func (c *MemoryCache[K, V]) Clear(_ context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items = make(map[K]item[V])
	return nil
}

var _ cache.Cache[string, any] = (*MemoryCache[string, any])(nil)
