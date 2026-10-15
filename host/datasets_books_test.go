package host_test

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	accapp "github.com/jhermoso/karpo-fw-go/contexts/accounting/application"
	astapp "github.com/jhermoso/karpo-fw-go/contexts/assets/application"
	expapp "github.com/jhermoso/karpo-fw-go/contexts/exports/application"
	expdomain "github.com/jhermoso/karpo-fw-go/contexts/exports/domain"
	fisapp "github.com/jhermoso/karpo-fw-go/contexts/fiscal/application"
	parapp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pardomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	purapp "github.com/jhermoso/karpo-fw-go/contexts/purchases/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
)

// bookDatasetsScenario gives a company a ledger, an entry, a debt and an asset through the host,
// and exports its journal, what it owes and its fixed assets.
func bookDatasetsScenario(t *testing.T, sw *hotswap.Switch) {
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
	supplier := organization("Proveedor Dos SL", "B58378431", []string{pardomain.RoleSupplier.String()},
		&parapp.NewAffiliation{Organization: acme, RelationshipType: pardomain.RelSupplier.String()})

	// A ledger and an entry by hand.
	for code, name := range map[string]string{"4300": "Clientes", "7000": "Ventas", "5700": "Caja", "5720": "Bancos", "6400": "Sueldos", "6420": "SS empresa",
		"4760": "SS acreedora", "4751": "Retenciones", "4650": "Remuneraciones", "4000": "Proveedores"} {
		_, err := h.Accounting.Service.CreateAccount.Handle(actx, accapp.CreateAccount{Company: acme, Code: code, Name: name, Postable: true})
		must(err)
	}
	_, err := h.Accounting.Service.OpenLedger.Handle(actx, accapp.OpenLedger{Company: acme, StartMonth: 1, Accounts: map[string]string{"revenue": "7000",
		"customers": "4300", "cash": "5700", "bank": "5720", "wages": "6400", "employer-social-security": "6420", "social-security-payable": "4760",
		"withholding-payable": "4751", "net-pay-payable": "4650"}})
	must(err)
	entry, err := h.Accounting.Service.PostEntry.Handle(actx, accapp.PostEntry{Company: acme, Date: vocab.MustDate(2026, 9, 30), Description: "Pago a cuenta",
		Lines: []accapp.LineInput{{Account: "4000", Party: supplier, Debit: "50", Description: "Anticipo, proveedor"}, {Account: "5720", Credit: "50"}}})
	must(err)
	// A debt: the invoice of a supplier, which Payments learns of.
	_, err = h.Fiscal.Service.CreateRate.Handle(actx, fisapp.CreateRate{Type: "vat", Territory: "common", Code: "G21", Description: "General", Rate: "21",
		From: vocab.MustDate(2012, 9, 1)})
	must(err)
	_, err = h.Fiscal.Service.RegisterTaxpayer.Handle(actx, fisapp.RegisterTaxpayer{Organization: acme, TermsInput: fisapp.TermsInput{Territory: "common", FiscalYearStartMonth: 1}})
	must(err)
	_, err = h.Purchases.Service.RegisterInvoice.Handle(actx, purapp.RegisterInvoice{Company: acme, Supplier: supplier, SupplierNumber: "V-1",
		Issued: vocab.MustDate(2026, 9, 1), Received: vocab.MustDate(2026, 9, 2), Due: vocab.MustDate(2026, 10, 1), Total: "121.00",
		Lines: []purapp.LineInput{{Description: "Papel", Category: "goods", Base: "100", TaxCode: "G21"}}})
	must(err)
	// An asset.
	_, err = h.Assets.Service.Register.Handle(actx, astapp.RegisterAsset{Company: acme, Code: "fur-01", Name: "Furgoneta", Class: "vehicles", Serial: "1234-ABC",
		Acquired: vocab.MustDate(2026, 1, 10), InService: vocab.MustDate(2026, 1, 16), Cost: "12000", Residual: "2000", LifeMonths: 48})
	must(err)
	if _, err := h.Deliver(ctx); err != nil {
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

	const journal = "Ejercicio,Asiento,Fecha,Cuenta,Tercero,Concepto,Debe,Haber,Origen"
	if entry.Number != 1 {
		t.Fatalf("the entry: %+v", entry)
	}
	same("journal", export(actx, "journal", map[string]string{"company": acme, "from": "2026-09-30", "to": "2026-09-30"}), journal,
		"2026,1,2026-09-30,4000,Proveedor Dos SL,\"Anticipo, proveedor\",50.00,,manual", "2026,1,2026-09-30,5720,,Pago a cuenta,,50.00,manual")
	same("journal of another company", export(actx, "journal", map[string]string{"company": other}), journal)
	same("payables", export(actx, "payables", map[string]string{"company": acme, "open": "true"}),
		"Documento,Tipo,Acreedor,Concepto,Fecha,Vencimiento,Importe,Pagado,Pendiente,Anulado",
		"V-1,supplier-invoice,Proveedor Dos SL,,2026-09-01,2026-10-01,121.00,0.00,121.00,No")
	same("assets", export(actx, "assets", map[string]string{"company": acme}),
		"Código,Nombre,Clase,Nº serie,Adquisición,Puesta en servicio,Coste,Valor residual,Vida (meses),Amortización acumulada,Valor neto,Estado,Baja",
		"FUR-01,Furgoneta,vehicles,1234-ABC,2026-01-10,2026-01-16,12000.00,2000.00,48,0.00,12000.00,in-service,")

	scoped := func(perms ...authz.Permission) context.Context {
		ac, err := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "clerk", Kind: authz.Service,
			Permissions: append([]authz.Permission{expapp.PermJobRead, expapp.PermJobCreate}, perms...),
			Grants:      []authz.Grant{{OrganizationID: fw.MustParseUUID(other), Level: authz.ReadOnly}}})
		must(err)
		return authz.WithContext(ctx, ac)
	}
	if clerk := export(scoped(astapp.PermAssetRead), "assets", map[string]string{"company": other}); len(clerk) != 1 {
		t.Fatalf("what the clerk of another company sees: %v", clerk)
	}
	for _, dataset := range []string{"journal", "payables", "assets"} {
		if _, err := h.Exports.Service.Start.Handle(scoped(parapp.PermPartyRead), expapp.StartExport{Dataset: dataset}); !errors.Is(err, fw.ErrForbidden) {
			t.Fatalf("%s without its permission: %v", dataset, err)
		}
	}
}

func TestBookDatasets_OnMemory(t *testing.T) { bookDatasetsScenario(t, memorySwitch(t)) }
func TestBookDatasets_OnSQLite(t *testing.T) { bookDatasetsScenario(t, sqliteSwitch(t)) }
