package integration

import (
	"context"
	"io"
	"strings"
	"testing"

	accapp "github.com/jhermoso/karpo-fw-go/contexts/accounting/application"
	bilapp "github.com/jhermoso/karpo-fw-go/contexts/billing/application"
	expapp "github.com/jhermoso/karpo-fw-go/contexts/exports/application"
	expdomain "github.com/jhermoso/karpo-fw-go/contexts/exports/domain"
	finapp "github.com/jhermoso/karpo-fw-go/contexts/financial/application"
	geoapp "github.com/jhermoso/karpo-fw-go/contexts/geography/application"
	geodomain "github.com/jhermoso/karpo-fw-go/contexts/geography/domain"
	impapp "github.com/jhermoso/karpo-fw-go/contexts/imports/application"
	ordapp "github.com/jhermoso/karpo-fw-go/contexts/orders/application"
	parapp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pardomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	recapp "github.com/jhermoso/karpo-fw-go/contexts/receivables/application"
	recdomain "github.com/jhermoso/karpo-fw-go/contexts/receivables/domain"
	treapp "github.com/jhermoso/karpo-fw-go/contexts/treasury/application"
	"github.com/jhermoso/karpo-fw-go/host"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// The institution the files of Apiscore of these tests belong to.
const apiscoreEntity = "Pagos Uno EP"

var sageFiles = []impapp.FileDTO{
	{Role: "companies", Name: "empresas.csv", Content: "Code;Name;BaseCurrencyCode\n1;MACCORP EXACT CHANGE EP;EUR\n2;AIRPORT CHANGE SA;EUR\n"},
	{Role: "offices", Name: "oficinas.csv", Content: "CompanyCode;Code;Name;OwnerCompanyName;Address;PostalCode;City;Province;Phone;Email\n" +
		"1;CT-MAD;Centro Madrid;MACCORP EXACT CHANGE EP;Gran Vía 1;28013;Madrid;Madrid;;\n"},
	{Role: "persons", Name: "personas.csv", Content: "Dni;FirstName;LastName;Email;Gender\n12345678Z;Ana;Garcia;ana@x.invalid;Mujer\n"},
	{Role: "employees", Name: "empleados.csv", Content: "CompanyCode;CompanyName;EmployeeNumber;Dni;FullName;Email;CollectiveAgreementCode;BaseSalary;Irpf;WorkCenterCode;IsActive;HireDate;TerminationDate\n" +
		"1;MACCORP EXACT CHANGE EP;S-MC-001;12345678Z;Ana Garcia;;CONV-01;1800;15;CT-MAD;true;2021-05-01;\n"},
	{Role: "customers", Name: "clientes.csv", Content: "CompanyCode;CompanyName;CustomerCode;Name;TaxId;Email;PaymentConditionCode;CommissionistCode\n" +
		"1;MACCORP EXACT CHANGE EP;C-001;ACME SUPPLIES SL;B12345674;compras@acme.invalid;CRED-30;COM-01\n" +
		"2;AIRPORT CHANGE SA;C-A2;ACME SUPPLIES SL;B12345674;;CONTADO;\n"},
	{Role: "suppliers", Name: "proveedores.csv", Content: "CompanyCode;CompanyName;SupplierCode;Name;TaxId;Email;PaymentConditionCode;CommissionistCode\n" +
		"1;MACCORP EXACT CHANGE EP;S-001;PROVEEDOR ÑANDÚ SL;;ventas@norte.invalid;CRED-60;COM-02\n"},
	{Role: "tax-rates", Name: "catalogos.csv", Content: "CompanyCode;CompanyName;Code;Description;Rate;SurchargeRate;IsExempt;IsNonSubject\n" +
		"1;MACCORP EXACT CHANGE EP;IVA21;IVA general 21%;21;5.2;false;false\n"},
	{Role: "chart", Name: "plancontable.csv", Content: "CompanyCode;Code;Name\n1;570;Caja\n1;57000000;Caja, euros\n"},
	{Role: "bank-accounts", Name: "cuentasbancarias.csv", Content: "CompanyCode;BankCode;Iban;Bic;AccountHolderName;LedgerAccountCode\n" +
		"1;0049;ES1500490000000000000011;BSCHESMMXXX;MACCORP;57200011\n"},
}

const apiscoreHeader = "IBAN,FK_DIVISA,OPERATIVA_DESCRIPCION,ESTADO_DESCRIPCION,ABANDONADA,DEMO,BLOQUEADA,ID_CLIENTE,N_DOCUMENTO,TIPO_DOCUMENTO,TIPO_PERSONA,TITULAR,NOMBRE_APELLIDOS,EMPRESA,FECHA_ALTA,FECHA_BAJA,NACIONALIDAD_CLIENTE\n"

// ACME and Ana are already in Parties, from Sage: here they are customers of the institution.
var apiscoreFiles = []impapp.FileDTO{
	{Role: "payment-accounts", Name: "cuentas_pago.csv", Content: apiscoreHeader +
		"ES4400490001112223334445,EUR,Cuenta de Pago normal,Real,0,0,0,1001,B12345674,CIF,JURÍDICA,ACME SUPPLIES SL,,ACME SUPPLIES SL,2024-01-15,,\n" +
		"ES5500491111000000000001,USD,Cuenta de Agente,Bloqueada Real,0,0,1,1002,12345678Z,DNI,FÍSICA,ANA GARCIA,ANA GARCIA,,2023-05-02,,ESPAÑOLA\n" +
		"ES9800610000000000000004,EUR,Cuenta de Pago normal,Real,0,0,0,1003,PA1234567,PASAPORTE,FISICA,MARÍA NÚÑEZ,MARÍA NÚÑEZ,,2023-01-01,2026-02-01,VENEZOLANA\n"},
	{Role: "virtual-accounts", Name: "cuentas_cv.csv", Content: apiscoreHeader +
		"CV0000000001,MXP,Cuenta de Pago normal,Pruebas,0,1,0,1002,12345678Z,DNI,FÍSICA,ANA GARCIA,ANA GARCIA,,2025-03-10,,\n"},
	// The first is the account Sage loaded for another company: an IBAN is of one owner.
	{Role: "own-accounts", Name: "cuentas_propias.csv", Content: "ES_SEGREGADA,ACTIVO,DIVISA,IBAN,IBAN_ALL,SWIFT,BANCO\n" +
		"0,1,EUR,ES1500490000000000000011,,BMARES2MXXX,BANCA MARCH\n1,1,EUR,0049,ES0900490000000000000022,BSCHESMMXXX,BANCO SANTANDER\n"},
}

// tradeImports loads the files of Sage and then those of Apiscore through the host, twice each,
// and looks in the contexts for what they became.
func tradeImports(t *testing.T, h *host.Host, actx context.Context) {
	t.Helper()
	run := func(source string, files []impapp.FileDTO, want map[string]impapp.CountDTO) {
		t.Helper()
		r, err := h.Imports.Service.Execute.Handle(actx, impapp.RunImport{Source: source, Files: files})
		if err != nil {
			t.Fatalf("%s: %v", source, err)
		}
		for _, c := range r.Counts {
			kind := c.Kind
			c.Kind = ""
			if c != want[kind] {
				t.Fatalf("%s, %s: %+v, want %+v (%+v)", source, kind, c, want[kind], r.Messages)
			}
		}
	}
	run("sage", sageFiles, map[string]impapp.CountDTO{"legal-entity": {Read: 2, Created: 2}, "work-center": {Read: 1, Created: 1},
		"person": {Read: 1, Created: 1}, "employment": {Read: 1, Created: 1}, "work-place": {Read: 1, Created: 1},
		"customer": {Read: 2, Created: 1, Updated: 1}, "supplier": {Read: 1, Created: 1}, "tax-rate": {Read: 1, Created: 1},
		"ledger-account": {Read: 2, Created: 2}, "own-account": {Read: 1, Created: 1}})
	run("sage", sageFiles, map[string]impapp.CountDTO{"legal-entity": {Read: 2, Unchanged: 2}, "work-center": {Read: 1, Unchanged: 1},
		"person": {Read: 1, Unchanged: 1}, "employment": {Read: 1, Unchanged: 1}, "work-place": {Read: 1, Unchanged: 1},
		"customer": {Read: 2, Unchanged: 2}, "supplier": {Read: 1, Unchanged: 1}, "tax-rate": {Read: 1, Unchanged: 1},
		"ledger-account": {Read: 2, Unchanged: 2}, "own-account": {Read: 1, Unchanged: 1}})
	run("apiscore", apiscoreFiles, map[string]impapp.CountDTO{"legal-entity": {Read: 1, Created: 1}, "customer": {Read: 3, Created: 1, Updated: 2},
		"customer-account": {Read: 4, Created: 4}, "own-account": {Read: 2, Created: 1, Failed: 1}})
	run("apiscore", apiscoreFiles, map[string]impapp.CountDTO{"legal-entity": {Read: 1, Unchanged: 1}, "customer": {Read: 3, Unchanged: 3},
		"customer-account": {Read: 4, Unchanged: 4}, "own-account": {Read: 2, Unchanged: 1, Failed: 1}})

	all, err := h.Parties.Organizations.All(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	id := map[string]string{}
	for _, c := range all {
		id[c.Name] = c.ID
	}
	maccorp, entity := id["MACCORP EXACT CHANGE EP"], id[apiscoreEntity]
	if maccorp == "" || entity == "" {
		t.Fatalf("companies: %v", id)
	}
	named := func(q parapp.SearchParties) map[string]string {
		t.Helper()
		q.Size = 50
		p, err := h.Parties.Service.Search.Handle(actx, q)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for _, x := range p.Items {
			out[x.Name] = x.ID
		}
		return out
	}
	customers := named(parapp.SearchParties{Role: pardomain.RoleCustomer.String(), Organization: entity})
	if len(customers) != 3 || customers["ACME SUPPLIES SL"] == "" || customers["Ana Garcia"] == "" || customers["MARÍA NÚÑEZ"] == "" {
		t.Fatalf("the customers of the institution: %v", customers)
	}
	if acme := named(parapp.SearchParties{Document: "B12345674"}); len(acme) != 1 || acme["ACME SUPPLIES SL"] != customers["ACME SUPPLIES SL"] {
		t.Fatalf("ACME is one party: %v", acme)
	}
	if s := named(parapp.SearchParties{Role: pardomain.RoleSupplier.String(), Organization: maccorp}); len(s) != 1 || s["PROVEEDOR ÑANDÚ SL"] == "" {
		t.Fatalf("suppliers: %v", s)
	}
	chart, err := h.Accounting.Service.SearchAccounts.Handle(actx, accapp.SearchAccounts{Company: maccorp})
	if err != nil || len(chart) != 2 || chart[0].Postable == chart[1].Postable {
		t.Fatalf("chart: %+v %v", chart, err)
	}
	banks, err := h.Treasury.Service.SearchAccounts.Handle(actx, treapp.SearchAccounts{Owner: entity})
	if err != nil || len(banks) != 1 || banks[0].Alias != "BANCO SANTANDER 0022 (segregada)" {
		t.Fatalf("bank accounts: %+v %v", banks, err)
	}
	blocked, err := h.Financial.Service.Get.Handle(actx, finapp.GetAccount{Company: entity, Number: "ES5500491111000000000001"})
	if err != nil || blocked.Status != "blocked" || blocked.Currency != "USD" || blocked.Holder != customers["Ana Garcia"] || blocked.Opened != "2023-05-02" {
		t.Fatalf("the blocked account: %+v %v", blocked, err)
	}
	closed, err := h.Financial.Service.Get.Handle(actx, finapp.GetAccount{Company: entity, Number: "ES9800610000000000000004"})
	if err != nil || closed.Status != "closed" || closed.Closed != "2026-02-01" {
		t.Fatalf("the closed account: %+v %v", closed, err)
	}
	virtual, err := h.Financial.Service.Get.Handle(actx, finapp.GetAccount{Company: entity, Number: "CV0000000001"})
	if err != nil || !virtual.Virtual || !virtual.Demo || virtual.Currency != "MXN" || len(virtual.Uses) != 1 || virtual.Uses[0].Use != "virtual-multicurrency" {
		t.Fatalf("the virtual account: %+v %v", virtual, err)
	}

	// What the company sells, as files: a draft order and a draft invoice for the customer Sage brought.
	acme := named(parapp.SearchParties{Document: "B12345674"})["ACME SUPPLIES SL"]
	if _, err := h.Orders.Service.DraftOrder.Handle(actx, ordapp.DraftOrder{Company: maccorp, Customer: acme, Reference: "PO-Ñ7"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Billing.Service.DraftInvoice.Handle(actx, bilapp.DraftInvoice{Seller: maccorp, Customer: acme}); err != nil {
		t.Fatal(err)
	}
	listed := func(dataset string) string {
		t.Helper()
		j, err := h.Exports.Service.Start.Handle(actx, expapp.StartExport{Dataset: dataset, Filter: map[string]string{"customer": acme}})
		if err != nil {
			t.Fatal(err)
		}
		if done, err := h.RunChores(context.Background()); err != nil || done.ExportsWritten != 1 {
			t.Fatalf("%s: %+v %v", dataset, done, err)
		}
		id, _ := expdomain.ParseJobID(j.ID)
		f, err := h.Exports.Service.Download.Handle(actx, expapp.DownloadJob{ID: id})
		if err != nil {
			t.Fatal(err)
		}
		defer f.Content.Close()
		b, _ := io.ReadAll(f.Content)
		return string(b)
	}
	if orders := listed("orders"); !strings.Contains(orders, ",ACME SUPPLIES SL,PO-Ñ7,draft,0.00,0.00\r\n") || strings.Count(orders, "\r\n") != 2 {
		t.Fatalf("orders: %q", orders)
	}
	if invoices := listed("invoices"); !strings.Contains(invoices, ",Borrador,,,ACME SUPPLIES SL,,0.00,,,EUR\r\n") || strings.Count(invoices, "\r\n") != 2 {
		t.Fatalf("invoices: %q", invoices)
	}
	if owed := listed("receivables"); strings.Count(owed, "\r\n") != 1 {
		t.Fatalf("receivables: %q", owed)
	}

	// The calendar: a company in Madrid does not collect on a weekend, on a holiday of the town or
	// on one of the country.
	towns, err := h.Geography.Service.SearchBoundaries.Handle(actx, geoapp.SearchBoundaries{Text: "madrid", Type: geodomain.TypeMunicipality.String(), Size: 50})
	if err != nil {
		t.Fatal(err)
	}
	madrid := ""
	for _, b := range towns.Items {
		if b.Name == "Madrid" {
			madrid = b.ID
		}
	}
	es, err := h.Geography.Ports.Country(actx, "ES")
	if err != nil || madrid == "" {
		t.Fatalf("Madrid and Spain: %q %v", madrid, err)
	}
	mid, _ := pardomain.ParsePartyID(maccorp)
	if _, err := h.Parties.Service.AddContact.Handle(actx, parapp.AddContact{PartyID: mid, Kind: "postal", Purposes: []string{"default"},
		Address: &parapp.AddressDTO{Line1: "Mayor 1", PostalCode: "28013", Locality: "Madrid", Country: "ES", GeoBoundary: madrid}}); err != nil {
		t.Fatal(err)
	}
	for boundary, d := range map[string]geoapp.DayInput{madrid: {Date: vocab.MustDate(2026, 12, 7), Name: "Fiesta local"},
		es.Boundary: {Date: vocab.MustDate(2026, 12, 8), Name: "Inmaculada Concepción"}} {
		for range 2 { // twice: the second time nothing is new
			if _, err := h.Geography.Holidays.Declare(actx, geoapp.DeclareHolidays{Boundary: boundary, Days: []geoapp.DayInput{d}}); err != nil {
				t.Fatal(err)
			}
		}
	}
	year, err := h.Geography.Holidays.Search(actx, geoapp.SearchHolidays{Boundary: madrid, Year: 2026, Inherited: true})
	if err != nil || len(year) != 2 || year[0].Date != "2026-12-07" || year[1].Name != "Inmaculada Concepción" || year[1].BoundaryName == "" {
		t.Fatalf("the calendar of Madrid: %+v %v", year, err)
	}
	terms, err := h.Receivables.Service.CreateTerms.Handle(actx, recapp.CreateTerms{Seller: maccorp, Code: "30D", Description: "30 días", Installments: 1,
		DaysToFirst: 30, ControlHolidays: true})
	if err != nil {
		t.Fatal(err)
	}
	tid, _ := recdomain.ParseTermsID(terms.ID)
	dues, err := h.Receivables.Service.Preview.Handle(actx, recapp.PreviewSchedule{ID: tid, Issued: "2026-11-06", Amount: "121.00"})
	if err != nil || len(dues) != 1 || dues[0].Date != "2026-12-09" {
		t.Fatalf("thirty days from 6 November, past Sunday 6, Monday 7 and Tuesday 8 December: %+v %v", dues, err)
	}
}
