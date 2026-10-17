// Package real ofrece una implementación de Clock con el reloj estándar del sistema operativo.
package real

import (
	stdtime "time"

	"github.com/jhermoso/karpo-fw-go/pkg/time"
)

// RealClock delega directamente en las funciones de tiempo de la biblioteca estándar.
type RealClock struct{}

// New crea un RealClock nuevo.
func New() time.Clock {
	return &RealClock{}
}

// Now devuelve la hora local actual del sistema operativo.
func (c *RealClock) Now() stdtime.Time {
	return stdtime.Now()
}

// Since devuelve el tiempo transcurrido desde t.
func (c *RealClock) Since(t stdtime.Time) stdtime.Duration {
	return stdtime.Since(t)
}

// Sleep detiene la goroutine en curso durante al menos d.
func (c *RealClock) Sleep(d stdtime.Duration) {
	stdtime.Sleep(d)
}
