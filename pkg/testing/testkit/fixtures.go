// Package testkit ofrece arneses de prueba, dobles y datos de prueba comunes para las
// aplicaciones de Karpo.
package testkit

import (
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/events"
	"github.com/jhermoso/karpo-fw-go/pkg/events/inprocess"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/time/fake"
)

// Harness agrupa datos de prueba comunes: un reloj falso (instalado también como reloj del
// dominio), un bus en proceso, un almacén en memoria (unidad de trabajo) con su outbox y un
// registro de eventos.
type Harness struct {
	Clock    *fake.FakeClock
	Bus      *inprocess.Bus
	Store    *memory.Store
	Outbox   *memory.Outbox
	Registry *events.Registry
}

// NewHarness crea un Harness congelado en initialTime. Cuando t no es nil, el reloj del dominio
// se restaura al final de la prueba.
func NewHarness(t testing.TB, initialTime time.Time) *Harness {
	clock := fake.New(initialTime)
	restore := domain.SetClock(clock)
	if t != nil {
		t.Cleanup(restore)
	}
	store := memory.NewStore("testkit")
	return &Harness{
		Clock:    clock,
		Bus:      inprocess.New(),
		Store:    store,
		Outbox:   memory.NewOutbox(store),
		Registry: events.NewRegistry(),
	}
}
