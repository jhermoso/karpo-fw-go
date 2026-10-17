// Package apptest ofrece el fixture común de las pruebas de las implementaciones de aplicación:
// un almacén en memoria, un repositorio de Widget, un Orchestrator que registra en el outbox
// y un Relay que entrega a un bus en proceso.
package apptest

import (
	"context"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/orchestration"
	"github.com/jhermoso/karpo-fw-go/pkg/application/outbox"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/events"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	rt "github.com/jhermoso/karpo-fw-go/pkg/testing/repotest"
	"github.com/jhermoso/karpo-fw-go/pkg/testing/testkit"
)

// Fixture conecta las implementaciones de aplicación sobre adaptadores en memoria.
type Fixture struct {
	H     *testkit.Harness
	Repo  *memory.Repository[rt.WidgetID, *rt.Widget]
	Orch  *orchestration.Orchestrator[rt.WidgetID, *rt.Widget]
	Relay *outbox.Relay
}

// New crea un Fixture con el reloj del dominio congelado en un instante fijo.
func New(t *testing.T) *Fixture {
	h := testkit.NewHarness(t, time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC))
	repo := memory.NewRepository[rt.WidgetID, *rt.Widget](h.Store, memory.WithClone((*rt.Widget).Clone))
	orch := orchestration.New[rt.WidgetID, *rt.Widget](repo, h.Store, orchestration.WithOutbox(outbox.NewRecorder(h.Outbox)))
	events.Register[rt.WidgetCreated](h.Registry)
	events.Register[rt.WidgetRenamed](h.Registry)
	return &Fixture{H: h, Repo: repo, Orch: orch, Relay: outbox.NewRelay(h.Outbox, h.Registry, h.Bus)}
}

// Create da de alta un widget nuevo mediante el orquestador.
func (f *Fixture) Create(t *testing.T, name string) *rt.Widget {
	t.Helper()
	w, err := rt.NewWidget(rt.NewWidgetID(), name, 100, true, nil, domain.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Orch.Create(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	return w
}

// Pending devuelve los mensajes pendientes del outbox.
func (f *Fixture) Pending(t *testing.T) []application.OutboxMessage {
	t.Helper()
	msgs, err := f.H.Outbox.Pending(context.Background(), 100, 10)
	if err != nil {
		t.Fatal(err)
	}
	return msgs
}
