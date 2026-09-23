package ent_test

import (
	"context"
	"database/sql"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"

	"github.com/jhermoso/karpo-fw-go/pkg/persistence/ent"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/ent/contact"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/ent/party"
)

func TestEntGraphQueries(t *testing.T) {
	ctx := context.Background()

	// Open in-memory SQLite database using pure-Go driver
	db, err := sql.Open("sqlite", "file:ent?mode=memory&cache=shared&_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("failed opening in-memory sqlite db: %v", err)
	}
	defer db.Close()

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := ent.NewClient(ent.Driver(drv))
	defer client.Close()

	// Run auto-migration for the schema
	if err := client.Schema.Create(ctx); err != nil {
		t.Fatalf("failed creating schema resources: %v", err)
	}

	// 1. Create a Party with graph relationships (Contacts)
	acme, err := client.Party.Create().
		SetTaxID("B12345678").
		SetLegalName("Acme Corporation S.L.").
		SetIsActive(true).
		Save(ctx)
	if err != nil {
		t.Fatalf("failed creating party: %v", err)
	}

	// 2. Add Contacts linked to Acme via graph edge
	_, err = client.Contact.Create().
		SetEmail("contact@acme.com").
		SetCity("Madrid").
		SetOwner(acme).
		Save(ctx)
	if err != nil {
		t.Fatalf("failed creating contact 1: %v", err)
	}

	_, err = client.Contact.Create().
		SetEmail("billing@acme.com").
		SetCity("Barcelona").
		SetOwner(acme).
		Save(ctx)
	if err != nil {
		t.Fatalf("failed creating contact 2: %v", err)
	}

	// 3. LINQ-like Query: Find Party by TaxID, Include Contacts (Eager loading graph edge)
	foundParty, err := client.Party.Query().
		Where(
			party.TaxID("B12345678"),
			party.IsActive(true),
		).
		WithContacts().
		Only(ctx)
	if err != nil {
		t.Fatalf("failed querying party with contacts: %v", err)
	}

	if foundParty.LegalName != "Acme Corporation S.L." {
		t.Errorf("unexpected legal name: %s", foundParty.LegalName)
	}
	if len(foundParty.Edges.Contacts) != 2 {
		t.Fatalf("expected 2 eager-loaded contacts, got %d", len(foundParty.Edges.Contacts))
	}

	// 4. Reverse Graph Query: Find all Contacts where Owner has TaxID == "B12345678" and City == "Madrid"
	madridContacts, err := client.Contact.Query().
		Where(
			contact.City("Madrid"),
			contact.HasOwnerWith(party.TaxID("B12345678")),
		).
		All(ctx)
	if err != nil {
		t.Fatalf("failed querying contacts: %v", err)
	}

	if len(madridContacts) != 1 || madridContacts[0].Email != "contact@acme.com" {
		t.Errorf("unexpected query result: %v", madridContacts)
	}
}
