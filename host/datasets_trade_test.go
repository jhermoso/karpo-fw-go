package host_test

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	bilapp "github.com/jhermoso/karpo-fw-go/contexts/billing/application"
	bildomain "github.com/jhermoso/karpo-fw-go/contexts/billing/domain"
	expapp "github.com/jhermoso/karpo-fw-go/contexts/exports/application"
	expdomain "github.com/jhermoso/karpo-fw-go/contexts/exports/domain"
	fisapp "github.com/jhermoso/karpo-fw-go/contexts/fiscal/application"
	ordapp "github.com/jhermoso/karpo-fw-go/contexts/orders/application"
	parapp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pardomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	purapp "github.com/jhermoso/karpo-fw-go/contexts/purchases/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
)

// tradeDatasetsScenario makes a company sell and buy through the host, and exports its invoices,
// its orders, what its customers owe and the invoices of its suppliers.
func tradeDatasetsScenario(t *testing.T, sw *hotswap.Switch) {
	th := newTradeHost(t, sw)
	h, actx, ctx := th.h, th.actx, context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	const taxID = "c0000000-0004-0000-0000-000000000003"
	organization := func(name, nif string, roles []string, affiliation *parapp.NewAffiliation) string {
		t.Helper()
		p, err := h.Parties.Service.RegisterOrganization.Handle(actx, parapp.RegisterOrganization{LegalName: name, Roles: roles, Affiliation: affiliation})
		must(err)
		pid, _ := pardomain.ParsePartyID(p.ID)
		_, err = h.Parties.Service.AddIdentification.Handle(actx, parapp.AddIdentification{PartyID: pid, DocumentType: taxID, Country: "ES", Number: nif, Primary: true})
		must(err)
		return p.ID
	}
	acme := organization("Acme SL", "B12345674", []string{pardomain.RoleInternalOrganization.String()}, nil)
	other := organization("Otra SL", "A58818501", []string{pardomain.RoleInternalOrganization.String()}, nil)
	customer := organization("Cliente, Uno SL", "A08015497", []string{pardomain.RoleCustomer.String()},
		&parapp.NewAffiliation{Organization: acme, RelationshipType: pardomain.RelCustomer.String()})
	supplier := organization("Proveedor Dos SL", "B58378431", []string{pardomain.RoleSupplier.String()},
		&parapp.NewAffiliation{Organization: acme, RelationshipType: pardomain.RelSupplier.String()})

	_, err := h.Fiscal.Service.CreateRate.Handle(actx, fisapp.CreateRate{Type: "vat", Territory: "common", Code: "G21", Description: "General", Rate: "21",
		From: vocab.MustDate(2012, 9, 1)})
	must(err)
	_, err = h.Fiscal.Service.RegisterTaxpayer.Handle(actx, fisapp.RegisterTaxpayer{Organization: acme, TermsInput: fisapp.TermsInput{Territory: "common", FiscalYearStartMonth: 1}})
	must(err)
	series, err := h.Billing.Service.OpenSeries.Handle(actx, bilapp.OpenSeries{Seller: acme, Code: "FA", Year: 2026})
	must(err)
	draft := func(price string) bildomain.InvoiceID {
		t.Helper()
		inv, err := h.Billing.Service.DraftInvoice.Handle(actx, bilapp.DraftInvoice{Seller: acme, Customer: customer,
			DetailsInput: bilapp.DetailsInput{DueDate: vocab.MustDate(2026, 10, 15)}})
		must(err)
		id, _ := bildomain.ParseInvoiceID(inv.ID)
		_, err = h.Billing.Service.AddLine.Handle(actx, bilapp.AddLine{ID: id, Description: "Cuota", Quantity: "1", UnitPrice: price, TaxCode: "G21"})
		must(err)
		return id
	}
	issued, err := h.Billing.Service.Issue.Handle(actx, bilapp.IssueInvoice{ID: draft("100"), Series: series.ID, Date: vocab.MustDate(2026, 9, 28)})
	must(err)
	draft("50")
	_, err = h.Orders.Service.DraftOrder.Handle(actx, ordapp.DraftOrder{Company: acme, Customer: customer, Date: vocab.MustDate(2026, 9, 20), Reference: "PO-7"})
	must(err)
	_, err = h.Purchases.Service.RegisterInvoice.Handle(actx, purapp.RegisterInvoice{Company: acme, Supplier: supplier, SupplierNumber: "V-1",
		Issued: vocab.MustDate(2026, 9, 1), Received: vocab.MustDate(2026, 9, 2), Due: vocab.MustDate(2026, 10, 1), Total: "121.00",
		Lines: []purapp.LineInput{{Description: "Papel", Category: "goods", Base: "100", TaxCode: "G21"}}})
	must(err)
	if _, err := h.Deliver(ctx); err != nil { // Receivables learns of the invoice
		t.Fatal(err)
	}

	export := func(c context.Context, dataset string, filter map[string]string) []string {
		t.Helper()
		job, err := h.Exports.Service.Start.Handle(c, expapp.StartExport{Dataset: dataset, Filter: filter})
		if err != nil {
			t.Fatalf("%s: %v", dataset, err)
		}
		if chores, err := h.RunChores(ctx); err != nil || chores.ExportsWritten != 1 {
			t.Fatalf("%s: chores %+v %v", dataset, chores, err)
		}
		jid, _ := expdomain.ParseJobID(job.ID)
		file, err := h.Exports.Service.Download.Handle(c, expapp.DownloadJob{ID: jid})
		if err != nil {
			t.Fatalf("%s: %v", dataset, err)
		}
		defer file.Content.Close()
		body, _ := io.ReadAll(file.Content)
		lines := strings.Split(strings.TrimSuffix(strings.TrimPrefix(string(body), bom), "\r\n"), "\r\n")
		slices.Sort(lines[1:])
		return lines
	}
	same := func(what string, got []string, want ...string) {
		t.Helper()
		if !slices.Equal(got, want) {
			t.Fatalf("%s:\n%s\nwant\n%s", what, strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	}

	const invoices = "Número,Tipo,Estado,Fecha,Vencimiento,Cliente,CIF/NIF,Base,Cuota,Total,Divisa"
	same("invoices", export(actx, "invoices", map[string]string{"seller": acme}), invoices,
		","+issued.Kind+",Borrador,,2026-10-15,\"Cliente, Uno SL\",,50.00,,,EUR",
		issued.Number+","+issued.Kind+",Emitida,2026-09-28,2026-10-15,\"Cliente, Uno SL\",A08015497,100.00,21.00,121.00,EUR")
	same("issued invoices", export(actx, "invoices", map[string]string{"status": "issued", "from": "2026-09-01", "to": "2026-09-30"}), invoices,
		issued.Number+","+issued.Kind+",Emitida,2026-09-28,2026-10-15,\"Cliente, Uno SL\",A08015497,100.00,21.00,121.00,EUR")
	same("invoices of another company", export(actx, "invoices", map[string]string{"seller": other}), invoices)
	same("orders", export(actx, "orders", map[string]string{"company": acme}), "Número,Fecha,Cliente,Referencia,Estado,Total,Pendiente",
		",2026-09-20,\"Cliente, Uno SL\",PO-7,draft,0.00,0.00")
	same("receivables", export(actx, "receivables", map[string]string{"open": "true"}),
		"Factura,Cliente,Fecha factura,Plazo,Vencimiento,Importe,Cobrado,Pendiente",
		issued.Number+",\"Cliente, Uno SL\",2026-09-28,1,2026-10-15,121.00,0.00,121.00")
	bought := export(actx, "purchase-invoices", map[string]string{"company": acme})
	if len(bought) != 2 || bought[0] != "Registro,Nº proveedor,Proveedor,Fecha,Recepción,Vencimiento,Base,Cuota,Total,Retención,A pagar,Anulada" ||
		!strings.HasSuffix(bought[1], ",V-1,Proveedor Dos SL,2026-09-01,2026-09-02,2026-10-01,100.00,21.00,121.00,0.00,121.00,No") {
		t.Fatalf("purchase invoices: %v", bought)
	}

	// Who sees another company gets an empty file; who lacks the permission of a list, none.
	scoped := func(perms ...authz.Permission) context.Context {
		ac, err := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "clerk", Kind: authz.Service,
			Permissions: append([]authz.Permission{expapp.PermJobRead, expapp.PermJobCreate}, perms...),
			Grants:      []authz.Grant{{OrganizationID: fw.MustParseUUID(other), Level: authz.ReadOnly}}})
		must(err)
		return authz.WithContext(ctx, ac)
	}
	same("what the clerk of another company sees", export(scoped(bilapp.PermInvoiceRead), "invoices", nil), invoices)
	for _, dataset := range []string{"invoices", "orders", "receivables", "purchase-invoices"} {
		if _, err := h.Exports.Service.Start.Handle(scoped(parapp.PermPartyRead), expapp.StartExport{Dataset: dataset}); !errors.Is(err, fw.ErrForbidden) {
			t.Fatalf("%s without its permission: %v", dataset, err)
		}
	}
}

func TestTradeDatasets_OnMemory(t *testing.T) { tradeDatasetsScenario(t, memorySwitch(t)) }
func TestTradeDatasets_OnSQLite(t *testing.T) { tradeDatasetsScenario(t, sqliteSwitch(t)) }
