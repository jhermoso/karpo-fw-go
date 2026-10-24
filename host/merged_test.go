package host_test

import (
	"context"
	"net/http/httptest"
	"testing"

	bilapp "github.com/jhermoso/karpo-fw-go/contexts/billing/application"
	orapp "github.com/jhermoso/karpo-fw-go/contexts/orders/application"
	parapp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pardomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	reapp "github.com/jhermoso/karpo-fw-go/contexts/receivables/application"
	"github.com/jhermoso/karpo-fw-go/contexts/security"
	secapp "github.com/jhermoso/karpo-fw-go/contexts/security/application"
	"github.com/jhermoso/karpo-fw-go/host"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
)

// Al fusionar una ficha en Parties, los listados de pedidos, facturas y cobros dejan fuera sus
// registros, que vuelven con includeMergedParties; por su identidad se siguen leyendo; y al
// deshacer la fusión vuelven a salir (docs/DECISIONES-COMUNES.md, D-100).
func TestListings_LeaveOutTheRecordsOfAMergedParty(t *testing.T) {
	ctx := context.Background()
	t.Setenv(security.EnvBootstrapUser, "root")
	t.Setenv(security.EnvBootstrapPassword, bootPassword)
	sw := hotswap.New(memory.NewStore("memory"))
	t.Cleanup(func() { _ = sw.Close(ctx) })
	h, err := host.Compose(sw, host.Options{JWTSecret: []byte("merged-listings-test-secret")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.Start(ctx); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)
	c := client{t: t, url: srv.URL}
	var tok secapp.Tokens
	c.must(c.do("POST", "/api/auth/login", "", map[string]any{"username": "root", "password": bootPassword}, &tok), 200, "login")
	c.must(c.do("POST", "/api/auth/change-password", tok.AccessToken, map[string]any{"currentPassword": bootPassword, "newPassword": rootPassword}, &tok), 200, "change password")
	root := tok.AccessToken

	admin, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "admin", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
	admin.GlobalAdmin = true
	actx := authz.WithContext(ctx, admin)
	ok := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	acme, err := h.Parties.Service.RegisterOrganization.Handle(actx, parapp.RegisterOrganization{LegalName: "Acme", Roles: []string{pardomain.RoleInternalOrganization.String()}})
	ok(err)
	customer := func(name string) parapp.PartyDTO {
		p, err := h.Parties.Service.RegisterOrganization.Handle(actx, parapp.RegisterOrganization{LegalName: name,
			Affiliation: &parapp.NewAffiliation{Organization: acme.ID, RelationshipType: pardomain.RelCustomer.String()}})
		ok(err)
		return p
	}
	kept, dup := customer("Talleres Pérez"), customer("Talleres Perez")

	// Un pedido, una factura y un cobro de cada cliente, antes de fusionar.
	orders, invoices, collections := map[string]string{}, map[string]string{}, map[string]string{}
	for _, p := range []string{kept.ID, dup.ID} {
		o, err := h.Orders.Service.DraftOrder.Handle(actx, orapp.DraftOrder{Company: acme.ID, Customer: p, Date: vocab.MustDate(2026, 10, 20)})
		ok(err)
		orders[p] = o.ID
		inv, err := h.Billing.Service.DraftInvoice.Handle(actx, bilapp.DraftInvoice{Seller: acme.ID, Customer: p})
		ok(err)
		invoices[p] = inv.ID
		col, err := h.Receivables.Service.RegisterCollection.Handle(actx, reapp.RegisterCollection{Seller: acme.ID, Payer: p,
			Date: vocab.MustDate(2026, 10, 20), Amount: "10", Method: "transfer"})
		ok(err)
		collections[p] = col.ID
	}

	type row struct {
		ID       string `json:"id"`
		Customer string `json:"customer"`
		Payer    string `json:"payer"`
	}
	list := func(path string) map[string]bool {
		t.Helper()
		var page fw.Page[row]
		c.must(c.do("GET", path, root, nil, &page), 200, path)
		out := map[string]bool{}
		for _, it := range page.Items {
			out[it.Customer+it.Payer] = true
		}
		return out
	}
	listings := []string{"/api/orders/orders?company=" + acme.ID, "/api/billing/invoices?seller=" + acme.ID,
		"/api/receivables/collections?seller=" + acme.ID}
	expect := func(what string, withDup bool) {
		t.Helper()
		for _, path := range listings {
			got := list(path)
			if !got[kept.ID] || got[dup.ID] != withDup || len(got) != map[bool]int{false: 1, true: 2}[withDup] {
				t.Fatalf("%s, %s: %v", what, path, got)
			}
			if all := list(path + "&includeMergedParties=true"); len(all) != 2 {
				t.Fatalf("%s, %s with includeMergedParties: %v", what, path, all)
			}
		}
	}
	expect("before the merge", true)

	survivor, err := h.Parties.Service.RegisterOrganization.Handle(actx, parapp.RegisterOrganization{LegalName: "Talleres Pérez SL"})
	ok(err)
	var mark parapp.RelationshipDTO
	c.must(c.do("POST", "/api/party-relationships", root, map[string]any{"type": pardomain.RelDuplicateOf.String(),
		"fromParty": dup.ID, "toParty": survivor.ID}, &mark), 201, "merge")
	expect("after the merge", false)
	// Filtrar por la ficha fusionada trae sus registros sin pedir includeMergedParties (D-108).
	for _, path := range []string{"/api/orders/orders?company=" + acme.ID + "&customer=" + dup.ID,
		"/api/billing/invoices?seller=" + acme.ID + "&customer=" + dup.ID, "/api/receivables/collections?seller=" + acme.ID + "&payer=" + dup.ID} {
		if got := list(path); len(got) != 1 || !got[dup.ID] {
			t.Fatalf("%s: %v", path, got)
		}
	}
	for what, path := range map[string]string{"order": "/api/orders/orders/" + orders[dup.ID], "invoice": "/api/billing/invoices/" + invoices[dup.ID],
		"collection": "/api/receivables/collections/" + collections[dup.ID]} {
		c.must(c.do("GET", path, root, nil, nil), 200, "the "+what+" of the merged party, by id")
	}

	// Deshacer la fusión es terminar la relación «Duplicate Of»: sus registros vuelven a salir.
	c.must(c.do("POST", "/api/party-relationships/"+mark.ID+"/terminate", root, map[string]any{}, nil), 200, "undo the merge")
	expect("after undoing the merge", true)
}
