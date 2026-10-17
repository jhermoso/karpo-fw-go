package parties_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/jhermoso/karpo-fw-go/examples/parties/contracts"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/messaging/inprocess"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
)

// crmPartyRegistered es la copia aguas abajo del contrato de Parties: un lector tolerante con
// sólo los campos que necesita el CRM (los campos de más de aguas arriba se ignoran).
type crmPartyRegistered struct {
	PartyID   string `json:"partyId"`
	LegalName string `json:"legalName"`
}

func (crmPartyRegistered) IntegrationEventType() string { return "parties.party-registered.v1" }

type crmPartyRenamed struct {
	PartyID   string `json:"partyId"`
	LegalName string `json:"legalName"`
}

func (crmPartyRenamed) IntegrationEventType() string { return "parties.party-renamed.v1" }

// TestParties_IntegrationEvents: Parties publica su Published Language a través del outbox de
// integración; dos contextos aguas abajo lo consumen a través de sus propios inbox (el CRM en
// SQLite, el contexto de facturación en memoria). Un consumidor que falla una vez obliga a
// reenviar a todos, y los inbox lo hacen inofensivo.
func TestParties_IntegrationEvents(t *testing.T) {
	ctx := context.Background()
	e := compose(t)
	acme := e.register("Acme Integration", "B12345674")
	e.register("Globex Integration", "A58818501")
	if status, body := e.do("PUT", "/parties/"+acme.ID.String()+"/legal-name", map[string]string{"legalName": "Acme Renamed"}, nil); status != 200 {
		t.Fatalf("rename: %d %s", status, body)
	}
	// ContactAdded es interno: llega al outbox de dominio pero no al outbox de integración.
	if status, body := e.do("POST", "/parties/"+acme.ID.String()+"/contacts", map[string]any{"kind": "email", "value": "a@acme.test", "primary": true}, nil); status != 200 {
		t.Fatalf("contact: %d %s", status, body)
	}

	// CRM: su propia base de datos SQLite con un inbox; su modelo de lectura vive en la misma base.
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "crm.db")+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	crmDB := sqlite.Open(raw, sqlrepo.WithName("crm"))
	for _, s := range append(sqlite.InboxDDL(""), `CREATE TABLE crm_customers (party_id TEXT PRIMARY KEY, name TEXT NOT NULL)`) {
		if _, err := crmDB.ExecContext(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	crmInbox, _ := sqlrepo.NewInbox(crmDB, "")
	crm := messaging.NewConsumer("crm", crmInbox, crmDB)
	messaging.Handle(crm, func(ctx context.Context, evt crmPartyRegistered, env application.Envelope) error {
		if env.Source != contracts.Source || env.Subject != evt.PartyID || env.CorrelationID == "" {
			t.Errorf("envelope: %+v", env)
		}
		_, err := crmDB.ExecContext(ctx, "INSERT INTO crm_customers (party_id, name) VALUES (?, ?)", evt.PartyID, evt.LegalName)
		return err // un duplicado violaría la clave primaria: el inbox debe impedirlo
	})
	messaging.Handle(crm, func(ctx context.Context, evt crmPartyRenamed, _ application.Envelope) error {
		_, err := crmDB.ExecContext(ctx, "UPDATE crm_customers SET name = ? WHERE party_id = ?", evt.LegalName, evt.PartyID)
		return err
	})

	// Facturación: en memoria; falla la primera vez que ve el alta de Acme, así que el mensaje se
	// reenvía también al CRM, cuyo INSERT violaría entonces su clave primaria.
	billingStore := memory.NewStore("billing")
	billing := messaging.NewConsumer("billing", memory.NewInbox(billingStore), billingStore)
	registered, failures := map[string]int{}, 0
	messaging.Handle(billing, func(_ context.Context, evt contracts.PartyRegisteredV1, _ application.Envelope) error {
		if evt.TaxID == "B12345674" && failures == 0 {
			failures++
			return errors.New("billing temporarily unavailable")
		}
		registered[evt.TaxID]++
		return nil
	})

	broker := inprocess.NewBroker()
	broker.Subscribe(crm.Name(), crm, crm.Types()...)
	broker.Subscribe(billing.Name(), billing, billing.Types()...)
	relay := messaging.NewRelay(contracts.Source, e.integ, broker)

	n, err := relay.RelayOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 { // Globex y el cambio de nombre entregados; Acme falló en facturación y sigue pendiente
		t.Fatalf("first pass delivered %d", n)
	}
	if n, err := relay.RelayOnce(ctx); err != nil || n != 1 {
		t.Fatalf("second pass: %d %v", n, err)
	}
	if left, _ := e.integ.Pending(ctx, 100, 10); len(left) != 0 {
		t.Fatalf("integration outbox not drained: %d", len(left))
	}

	var name string
	var customers int
	_ = raw.QueryRow("SELECT COUNT(*) FROM crm_customers").Scan(&customers)
	_ = raw.QueryRow("SELECT name FROM crm_customers WHERE party_id = ?", acme.ID.String()).Scan(&name)
	if customers != 2 || name != "Acme Renamed" {
		t.Fatalf("CRM read model: %d customers, acme=%q", customers, name)
	}
	if registered["B12345674"] != 1 || registered["A58818501"] != 1 || failures != 1 {
		t.Fatalf("billing: %v, failures %d", registered, failures)
	}
}
