package host_test

import (
	"context"
	"net/http/httptest"
	"testing"

	orapp "github.com/jhermoso/karpo-fw-go/contexts/orders/application"
	parapp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pardomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	puapp "github.com/jhermoso/karpo-fw-go/contexts/purchases/application"
	reapp "github.com/jhermoso/karpo-fw-go/contexts/receivables/application"
	"github.com/jhermoso/karpo-fw-go/contexts/security"
	secapp "github.com/jhermoso/karpo-fw-go/contexts/security/application"
	"github.com/jhermoso/karpo-fw-go/host"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
)

// La vista por pareja (D-51, D-73): una fila por relación de cliente o de proveedor entre una
// empresa del grupo y un tercero, con lo que saben de esa pareja Parties, Cobros, Pedidos y Compras;
// filtrada y ordenada por esos datos.
func TestPairs_CustomerAndSupplierRelationships(t *testing.T) {
	ctx := context.Background()
	t.Setenv(security.EnvBootstrapUser, "root")
	t.Setenv(security.EnvBootstrapPassword, bootPassword)
	sw := hotswap.New(memory.NewStore("memory"))
	t.Cleanup(func() { _ = sw.Close(ctx) })
	h, err := host.Compose(sw, host.Options{JWTSecret: []byte("pairs-test-secret")})
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
	company := func(name string) parapp.PartyDTO {
		p, err := h.Parties.Service.RegisterOrganization.Handle(actx, parapp.RegisterOrganization{LegalName: name, Roles: []string{pardomain.RoleInternalOrganization.String()}})
		ok(err)
		return p
	}
	acme, levante := company("Acme"), company("Acme Levante")
	customer := func(name, org, number string) parapp.PartyDTO {
		p, err := h.Parties.Service.RegisterOrganization.Handle(actx, parapp.RegisterOrganization{LegalName: name,
			Affiliation: &parapp.NewAffiliation{Organization: org, RelationshipType: pardomain.RelCustomer.String()}})
		ok(err)
		rels, err := h.Parties.Service.Relationships.Handle(actx, parapp.PartyRelationships{PartyID: pardomain.PartyID{UUID: fw.MustParseUUID(p.ID)}})
		ok(err)
		id, _ := pardomain.ParseRelationshipID(rels[0].ID)
		_, err = h.Parties.Service.SetCustomerNumber.Handle(actx, parapp.SetCustomerNumber{ID: id, CustomerNumber: number})
		ok(err)
		return p
	}
	talleres := customer("Talleres Pérez", acme.ID, "C-002")
	ferreteria := customer("Ferretería Sol", acme.ID, "C-001")
	// Talleres compra también a Acme Levante: es otra pareja, con su número.
	_, err = h.Parties.Service.EstablishRelationship.Handle(actx, parapp.EstablishRelationship{Type: pardomain.RelCustomer.String(), From: talleres.ID, To: levante.ID})
	ok(err)

	// Lo que saben de cada pareja Cobros y Pedidos.
	limit := "5000"
	_, err = h.Receivables.Service.SetCredit.Handle(actx, reapp.SetCredit{Seller: acme.ID, Customer: talleres.ID, Limit: &limit})
	ok(err)
	_, err = h.Receivables.Service.SetCredit.Handle(actx, reapp.SetCredit{Seller: acme.ID, Customer: ferreteria.ID, Blocked: true})
	ok(err)
	_, err = h.Orders.Service.SetTerms.Handle(actx, orapp.SetTerms{Company: acme.ID, Customer: talleres.ID, BlockDelivery: true})
	ok(err)

	type page struct {
		Items []host.PairRow `json:"items"`
		Total int            `json:"total"`
	}
	search := func(path string, body map[string]any) page {
		t.Helper()
		var p page
		c.must(c.do("POST", path, root, body, &p), 200, path)
		return p
	}
	all := search("/api/customer-relationships/search", map[string]any{})
	if all.Total != 3 {
		t.Fatalf("three pairs, Talleres twice: %+v", all)
	}
	byNumber := search("/api/customer-relationships/search", map[string]any{
		"filter": map[string]any{"internalOrganizationIds": []string{acme.ID}}, "sort": map[string]any{"by": "customerNumber"}})
	if byNumber.Total != 2 || byNumber.Items[0].CustomerNumber != "C-001" || byNumber.Items[1].Name != "Talleres Pérez" {
		t.Fatalf("Acme's customers by number: %+v", byNumber)
	}
	tp := byNumber.Items[1]
	if tp.CreditLimit == nil || *tp.CreditLimit != "5000.00" || tp.BlockDelivery == nil || !*tp.BlockDelivery || *tp.BlockOrders {
		t.Fatalf("Talleres with Acme: %+v", tp)
	}
	if fs := byNumber.Items[0]; fs.CreditLimit != nil || fs.BlockOrders == nil || !*fs.BlockOrders {
		t.Fatalf("Ferretería, blocked for credit and without a limit: %+v", fs)
	}
	blocked := search("/api/customer-relationships/search", map[string]any{"filter": map[string]any{"blocked": true}})
	if blocked.Total != 2 {
		t.Fatalf("blocked pairs: %+v", blocked)
	}
	c.must(c.do("POST", "/api/customer-relationships/search", root, map[string]any{"sort": map[string]any{"by": "color"}}, nil), 400, "an unknown sort")
	c.must(c.do("POST", "/api/customer-relationships/search", root, map[string]any{"filter": map[string]any{"color": "red"}}, nil), 400, "an unknown filter")
	var one host.PairRow
	c.must(c.do("GET", "/api/customer-relationships/"+tp.ID, root, nil, &one), 200, "one pair")
	if one.ID != tp.ID || one.InternalOrganizationName != "Acme" {
		t.Fatalf("one pair: %+v", one)
	}
	c.must(c.do("GET", "/api/customer-relationships/"+fw.NewUUID().String(), root, nil, nil), 404, "no such pair")

	// Proveedores: la empresa compra al proveedor; Compras dice si sus facturas están bloqueadas.
	supplier, err := h.Parties.Service.RegisterOrganization.Handle(actx, parapp.RegisterOrganization{LegalName: "Papelera Norte",
		Roles: []string{pardomain.RoleSupplier.String()}})
	ok(err)
	_, err = h.Parties.Service.EstablishRelationship.Handle(actx, parapp.EstablishRelationship{Type: pardomain.RelSupplier.String(), From: acme.ID, To: supplier.ID})
	ok(err)
	_, err = h.Purchases.Service.SetSupplier.Handle(actx, puapp.SetSupplier{Company: acme.ID, Supplier: supplier.ID, Blocked: true})
	ok(err)
	sup := search("/api/supplier-relationships/search", map[string]any{})
	if sup.Total != 1 || sup.Items[0].Name != "Papelera Norte" || sup.Items[0].BlockInvoices == nil || !*sup.Items[0].BlockInvoices ||
		sup.Items[0].CustomerNumber != "" || sup.Items[0].BlockOrders != nil {
		t.Fatalf("suppliers: %+v", sup)
	}
}
