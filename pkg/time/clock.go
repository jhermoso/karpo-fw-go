// Package time define la abstracción de reloj de las aplicaciones de Karpo.
// Desacoplar el tiempo del reloj del sistema permite pruebas deterministas de caducidades, tiempos de espera y flujos temporales.
package time

import (
	"time"
)

// Clock ofrece una abstracción sobre las consultas de hora y las pausas.
type Clock interface {
	// Now devuelve la hora actual.
	Now() time.Time

	// Since devuelve el tiempo transcurrido desde t.
	Since(t time.Time) time.Duration

	// Sleep detiene la ejecución en curso durante al menos d.
	Sleep(d time.Duration)
}
