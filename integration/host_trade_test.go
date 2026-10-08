package integration

import (
	"context"
	"testing"

	accapp "github.com/jhermoso/karpo-fw-go/contexts/accounting/application"
	finapp "github.com/jhermoso/karpo-fw-go/contexts/financial/application"
	impapp "github.com/jhermoso/karpo-fw-go/contexts/imports/application"
	parapp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pardomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	treapp "github.com/jhermoso/karpo-fw-go/contexts/treasury/application"
	"github.com/jhermoso/karpo-fw-go/host"
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
}
