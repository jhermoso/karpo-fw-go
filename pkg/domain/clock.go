package domain

import (
	"sync/atomic"
	"time"
)

// Clock es la fuente de tiempo mínima que necesita el dominio. pkg/time/real y pkg/time/fake la satisfacen.
type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

var clock atomic.Value // guarda un clockHolder

type clockHolder struct{ c Clock }

func init() { clock.Store(clockHolder{systemClock{}}) }

// Now devuelve la hora UTC actual según el reloj del dominio.
// Los agregados la usan para fechar los eventos de dominio, de modo que las pruebas pueden hacer el tiempo determinista.
func Now() time.Time { return clock.Load().(clockHolder).c.Now().UTC() }

// SetClock sustituye el reloj del dominio y devuelve una función que restaura el anterior.
// Pensada para las raíces de composición y las pruebas.
func SetClock(c Clock) (restore func()) {
	if c == nil {
		c = systemClock{}
	}
	prev := clock.Swap(clockHolder{c})
	return func() { clock.Store(prev) }
}
