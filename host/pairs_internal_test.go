package host

import (
	"context"
	"testing"

	orapp "github.com/jhermoso/karpo-fw-go/contexts/orders/application"
	parapp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pardomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	reapp "github.com/jhermoso/karpo-fw-go/contexts/receivables/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
)

// Una pareja es de quien tiene la empresa que vende, no de quien compra; y lo que quien llama no
// puede leer de un contexto se queda vacío, no se enseña como «sin límite, sin bloqueo».
func TestPairs_ScopeAndUnreadableContexts(t *testing.T) {
	ctx := context.Background()
	sw := hotswap.New(memory.NewStore("memory"))
	t.Cleanup(func() { _ = sw.Close(ctx) })
	h, err := Compose(sw, Options{JWTSecret: []byte("pairs-internal-secret")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.Start(ctx); err != nil {
		t.Fatal(err)
	}
	admin, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "admin", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
	admin.GlobalAdmin = true
	actx := authz.WithContext(ctx, admin)
	ok := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	company := func(name string) parapp.PartyDTO {
		p, err := h.Parties.Service.RegisterOrganization.Handle(actx, parapp.RegisterOrganization{LegalName: name, Roles: []string{pardomain.RoleInternalOrganization.String()}})
		ok(err)
		return p
	}
	acme, levante := company("Acme"), company("Acme Levante")
	talleres, err := h.Parties.Service.RegisterOrganization.Handle(actx, parapp.RegisterOrganization{LegalName: "Talleres Pérez",
		Affiliation: &parapp.NewAffiliation{Organization: acme.ID, RelationshipType: pardomain.RelCustomer.String()}})
	ok(err)
	// Acme compra a Acme Levante: esa pareja es de Acme Levante.
	_, err = h.Parties.Service.EstablishRelationship.Handle(actx, parapp.EstablishRelationship{Type: pardomain.RelCustomer.String(), From: acme.ID, To: levante.ID})
	ok(err)
	limit := "100"
	_, err = h.Receivables.Service.SetCredit.Handle(actx, reapp.SetCredit{Seller: acme.ID, Customer: talleres.ID, Limit: &limit, Blocked: true})
	ok(err)
	_, err = h.Orders.Service.SetTerms.Handle(actx, orapp.SetTerms{Company: acme.ID, Customer: talleres.ID, BlockDelivery: true})
	ok(err)

	// Alguien de Acme que lee parties y condiciones de venta, pero no el crédito de Cobros.
	clerk, err := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "clerk", Kind: authz.Service,
		Grants:      []authz.Grant{{OrganizationID: fw.MustParseUUID(acme.ID), Level: authz.Full}},
		Permissions: []authz.Permission{parapp.PermPartyRead, parapp.PermRelationshipRead, orapp.PermTermsRead}})
	ok(err)
	rows, err := h.Pairs().rows(authz.WithContext(ctx, clerk), "customer", pairFilter{}, "")
	ok(err)
	if len(rows) != 1 || rows[0].Name != "Talleres Pérez" {
		t.Fatalf("only the pairs where Acme sells: %+v", rows)
	}
	r := rows[0]
	if r.CreditLimit != nil || r.BlockOrders != nil {
		t.Fatalf("what Receivables does not let the clerk read is not shown: %+v", r)
	}
	if r.BlockDelivery == nil || !*r.BlockDelivery {
		t.Fatalf("what Orders lets the clerk read is shown: %+v", r)
	}

	// Sin permiso para leer parties, la vista no se abre.
	blind, err := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "blind", Kind: authz.Service,
		Grants:      []authz.Grant{{OrganizationID: fw.MustParseUUID(acme.ID), Level: authz.Full}},
		Permissions: []authz.Permission{parapp.PermRelationshipRead}})
	ok(err)
	if _, err := h.Pairs().rows(authz.WithContext(ctx, blind), "customer", pairFilter{}, ""); err == nil {
		t.Fatal("the pairs show whole parties: reading parties is needed")
	}
}
