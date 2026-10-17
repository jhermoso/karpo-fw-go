// Package fake ofrece una implementación controlable de Clock para pruebas deterministas.
package fake

import (
	"sync"
	stdtime "time"

	"github.com/jhermoso/karpo-fw-go/pkg/time"
)

// FakeClock es una implementación simulada de time.Clock, segura entre goroutines.
type FakeClock struct {
	mu      sync.RWMutex
	current stdtime.Time
}

// New crea un FakeClock nuevo iniciado en la hora indicada.
func New(initial stdtime.Time) *FakeClock {
	return &FakeClock{
		current: initial,
	}
}

// Now devuelve la hora congelada o avanzada a mano.
func (c *FakeClock) Now() stdtime.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.current
}

// Since devuelve la diferencia entre la hora simulada actual y t.
func (c *FakeClock) Since(t stdtime.Time) stdtime.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.current.Sub(t)
}

// Sleep simula una pausa avanzando el reloj d de inmediato.
func (c *FakeClock) Sleep(d stdtime.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = c.current.Add(d)
}

// Advance adelanta d la hora simulada.
func (c *FakeClock) Advance(d stdtime.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = c.current.Add(d)
}

// Set fija la hora simulada en una hora concreta.
func (c *FakeClock) Set(target stdtime.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = target
}

var _ time.Clock = (*FakeClock)(nil)
