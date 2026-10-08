package domain_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/imports/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

func TestDocument(t *testing.T) {
	for _, c := range []struct{ number, written, nationality, want string }{
		{"12345678Z", "DNI", "", "NIDN ES"},
		{"12345678-z", "PASAPORTE", "VENEZOLANA", "NIDN ES"}, // a Spanish number is Spanish whatever it is called
		{"B12345674", "CIF", "URUGUAY", "TXID ES"},
		{"X1234567L", "TARJETA RESIDENTE (NIE)", "VENEZOLANA", "OTHR ES"},
		{"12345678A", "DNI", "", "OTHR ES"}, // wrong check digit: Spanish by its type, not a DNI Parties would take
		{"22222222X", "NIF", "ESPAÑOLA", "OTHR ES"},
		{"P0000001", "PASAPORTE", "PERUNA", "OTHR PE"},
		{"P0000004", "Pasaporte", "EEUU DE AMÉRICA", "OTHR US"},
		{"F0000002", "DOCUMENTO FISCAL EXTRANJERO", "UNITED STATES OF AME", "TXID US"},
		{"A0000003", "ACREDITACIÓN", "DEUTSCH", "OTHR DE"},
		{"P0000005", "PASAPORTE", "país inventado", ""},
		{"P0000006", "PASAPORTE", "", ""},
		{"C0000007", "CARNET ALIEN", "VENEZOLANA", ""},
		{"", "DNI", "ESPAÑOLA", ""},
	} {
		typ, country, ok := domain.Document(c.number, c.written, c.nationality)
		got := ""
		if ok {
			got = typ + " " + country
		}
		if got != c.want {
			t.Errorf("%s %s %s: %q, want %q", c.number, c.written, c.nationality, got, c.want)
		}
	}
	if got := domain.CleanName("  JUAN\t\"EL\"  PÉREZ-O'NEIL (hijo); "); got != "JUAN EL PÉREZ-O'NEIL (hijo)" {
		t.Errorf("clean name: %q", got)
	}
}

// index returns the records of a kind by scope and key.
func index(records []domain.Record, kind string) map[string]map[string]string {
	out := map[string]map[string]string{}
	for _, r := range records {
		if r.Kind == kind {
			out[r.Scope+"|"+r.Key] = r.Fields
		}
	}
	return out
}

func said(messages []domain.Message) []string {
	out := []string{}
	for _, m := range messages {
		out = append(out, m.Kind+":"+m.Key+":"+m.Code)
	}
	slices.Sort(out)
	return out
}

func TestSage(t *testing.T) {
	files := []domain.File{
		{Role: domain.RoleBankAccounts, Name: "cuentasbancarias.csv", Content: "CompanyCode;BankCode;Iban;Bic;AccountHolderName;LedgerAccountCode\n" +
			"1;0049;ES11 0049 1111;bschesmmxxx;M;57200011\n1;0049;ES22;;M;57300018\n1;0049;ES33;;M;99999999\n1;0049;ES33;;M;\n7;0049;ES44;;M;\n"},
		{Role: domain.RoleChart, Name: "plancontable.csv", Content: "CompanyCode;Code;Name\n1;57;Tesorería\n1;570;Caja\n1;57000000;Caja, euros\n2;57;Tesorería\n1;57;Otra vez\n"},
		{Role: domain.RoleTaxRates, Name: "catalogos.csv", Content: "CompanyCode;CompanyName;Code;Description;Rate;SurchargeRate;IsExempt;IsNonSubject\n" +
			"1;A;iva21;;21.00;5.2;false;false\n2;B;IVA21;;21;5.2;false;false\n1;A;IVA00;Exento;0;;true;false\n1;A;RARO;;mucho;;false;false\n"},
		{Role: domain.RoleCustomers, Name: "clientes.csv", Content: "CompanyCode;CompanyName;CustomerCode;Name;TaxId;Email;PaymentConditionCode;CommissionistCode\n" +
			"1;;C-1;ACME  SL;b-12345674;Compras@Acme.test;CRED-30;COM-01\n;airport change sa;C-1;ACME SL;;;;\n1;;C-1;Repetido;;;;\n3;Nadie SL;C-9;Perdido;;;;\n"},
		{Role: domain.RoleEmployees, Name: "empleados.csv", Content: "CompanyCode;CompanyName;EmployeeNumber;Dni;FullName;Email;CollectiveAgreementCode;BaseSalary;Irpf;WorkCenterCode;IsActive;HireDate;TerminationDate\n" +
			"1;;E-1;12345678z;;;;;;CT-MAD;true;2021-05-01;\n1;;E-2;;Carlos Martinez Ruiz;c@x.test;;;;CT-XXX;false;2018-02-01;\n1;;E-3;99;;;;;;;true;;\n2;;e-1;;Otro;;;;;;true;;\n"},
		{Role: domain.RolePersons, Name: "personas.csv", Content: "Dni;FirstName;LastName;Email;Gender\n12345678Z;Ana;Garcia;Ana@x.test;Mujer\n12345678Z;Otra;Persona;;\n"},
		{Role: domain.RoleOffices, Name: "oficinas.csv", Content: "CompanyCode;Code;Name;OwnerCompanyName;Address;PostalCode;City;Province;Phone;Email\n" +
			"1;CT-MAD;Centro Madrid;;Gran Vía 1;28013;Madrid;Madrid;;\n1;CT-MA2;Centro Madrid;;;;;;;\n"},
		{Role: domain.RoleCompanies, Name: "empresas.csv", Content: "Code;Name;BaseCurrencyCode\n1;Maccorp  Exact Change EP;EUR\n2;Airport Change SA;eur\n2;Otra;EUR\n"},
	}
	records, messages, err := domain.Sage{}.Read(files)
	if err != nil {
		t.Fatal(err)
	}
	const m, a = "Maccorp Exact Change EP", "Airport Change SA"
	count := map[string]int{}
	for _, r := range records {
		count[r.Kind]++
	}
	if want := map[string]int{"legal-entity": 2, "work-center": 1, "person": 2, "employment": 2, "work-place": 1, "customer": 2, "tax-rate": 1,
		"ledger-account": 4, "own-account": 3}; len(count) != len(want) {
		t.Fatalf("kinds: %v", count)
	} else {
		for k, n := range want {
			if count[k] != n {
				t.Fatalf("%s: %d, want %d (%v)", k, count[k], n, count)
			}
		}
	}
	if got, want := said(messages), []string{"customer:C-1:sage.duplicate", "customer:C-9:sage.unknown_company", "employment:E-2:sage.left_without_date",
		"legal-entity:Otra:sage.duplicate", "own-account:ES44:sage.unknown_company",
		"person:E-3:sage.employee_without_name", "person:e-1:sage.duplicate", "tax-rate:IVA00:sage.exemption_is_not_a_rate", "tax-rate:RARO:sage.rate",
		"work-place:E-2:sage.unknown_office"}; !slices.Equal(got, want) {
		t.Fatalf("messages:\n%v\nwant\n%v", got, want)
	}
	people, jobs, places := index(records, "person"), index(records, "employment"), index(records, "work-place")
	if p := people["global|E-1"]; p["firstName"] != "Ana" || p["lastName"] != "Garcia" || p["email"] != "ana@x.test" || p["document"] != "12345678Z" ||
		p["documentType"] != "NIDN" || p["country"] != "ES" || p["legalEntity"] != m {
		t.Fatalf("E-1: %v", p)
	}
	if p := people["global|E-2"]; p["fullName"] != "Carlos Martinez Ruiz" || p["email"] != "c@x.test" || p["document"] != "" {
		t.Fatalf("E-2: %v", p)
	}
	if j := jobs[m+"|E-2"]; j["hireDate"] != "2018-02-01" || j["terminationDate"] != "2018-02-01" || jobs[m+"|E-1"]["terminationDate"] != "" {
		t.Fatalf("employments: %v", jobs)
	}
	if places[m+"|E-1"]["workCenter"] != "Centro Madrid" || index(records, "work-center")[m+"|Centro Madrid"]["notes"] != "Gran Vía 1, 28013 Madrid, Madrid" {
		t.Fatalf("work places: %v", places)
	}
	customers := index(records, "customer")
	if c := customers[m+"|C-1"]; c["name"] != "ACME SL" || c["document"] != "B-12345674" || c["documentType"] != "TXID" || c["email"] != "compras@acme.test" ||
		customers[a+"|C-1"]["document"] != "" {
		t.Fatalf("customers: %v", customers)
	}
	if r := index(records, "tax-rate")["global|IVA21"]; r["rate"] != "21" || r["surcharge"] != "5.2" || r["description"] != "IVA21" {
		t.Fatalf("rate: %v", r)
	}
	chart := index(records, "ledger-account")
	if chart[m+"|57"]["postable"] != "false" || chart[m+"|570"]["postable"] != "false" || chart[m+"|57000000"]["postable"] != "true" || chart[a+"|57"]["postable"] != "true" {
		t.Fatalf("chart: %v", chart)
	}
	banks := index(records, "own-account")
	if b := banks[m+"|ES1100491111"]; b["currency"] != "EUR" || b["segregated"] != "true" || b["abandonment"] != "true" || b["bic"] != "BSCHESMMXXX" {
		t.Fatalf("bank accounts: %v", banks)
	}
	if banks[m+"|ES22"]["currency"] != "GBP" || banks[m+"|ES22"]["abandonment"] != "false" || banks[m+"|ES33"]["segregated"] != "false" || banks[m+"|ES33"]["currency"] != "EUR" {
		t.Fatalf("bank accounts: %v", banks)
	}
}

func TestApiscore(t *testing.T) {
	const header = "IBAN,FK_DIVISA,OPERATIVA_DESCRIPCION,ESTADO_DESCRIPCION,ABANDONADA,DEMO,BLOQUEADA,ID_CLIENTE,N_DOCUMENTO,TIPO_DOCUMENTO,TIPO_PERSONA,TITULAR,NOMBRE_APELLIDOS,EMPRESA,FECHA_ALTA,FECHA_BAJA,NACIONALIDAD_CLIENTE\n"
	files := []domain.File{
		{Role: domain.RoleOwnAccounts, Name: "cuentas_propias.csv", Content: "BANCO,IBAN_ALL,IBAN,SWIFT,DIVISA\nBANCO SANTANDER,ES1100491111000000000009,0049,bschesmmxxx,\n" +
			",ES22,ES22,,EUR\nBANCA MARCH,,ES4400000000000000000004,,EUR\nBANCA MARCH,,ES4400000000000000000004,,EUR\n"},
		{Role: domain.RoleVirtualAccounts, Name: "cuentas_cv.csv", Content: header + "cv0000000001,mxp,Cuenta de Agente,Pruebas,0,0,0,1002,00000001a,DNI,FÍSICA,JUAN PRUEBA,JUAN PRUEBA,,2025-03-10,,\n"},
		{Role: domain.RolePaymentAccounts, Name: "cuentas_pago.csv", Content: header +
			"ES11 0000,EUR,Cuenta de Pago normal,Real,0,0,0,1001,b00000001,CIF,JURÍDICA,TITULAR SL,,EMPRESA UNO SL,2024-01-15,,\n" +
			"ES22,usd,Cuenta de Agente,Bloqueada Real,1,0,1.0,1002,00000001A,DNI,FÍSICA,JUAN TITULAR,JUAN PRUEBA,,02/05/2023,2026-02-01,\n" +
			"ES33,,Cuenta Operativa de Jugador,Real,true,0,0,1001,B00000001,CIF,JURÍDICA,OTRO NOMBRE,,OTRO NOMBRE,,,\n" +
			"ES33,,Repetida,Real,0,0,0,1001,B00000001,CIF,JURÍDICA,X,,X,,,\n" +
			",EUR,Sin número,Real,0,0,0,1009,Z1,DNI,FISICA,A,A,,,,\n" +
			"ES55,EUR,Sin titular,Real,0,0,0,1010,,,FISICA,,,,,,\n" +
			"ES66,EUR,Fecha rara,Real,0,0,0,1011,,,FISICA,A B,A B,,ayer,,\n" +
			"ES77,EUR,Cuenta de Pago normal,Real,0,0,0,,,,FISICA,,ana  lópez,,,,\n" +
			"ES88,EUR,Cuenta de Pago normal,Real,0,0,0,,P1,PASAPORTE,FISICA,,EVA SIN PAIS,,,,\n"},
	}
	records, messages, err := domain.Apiscore{Entity: " Pagos  Uno EP "}.Read(files)
	if err != nil {
		t.Fatal(err)
	}
	const e = "Pagos Uno EP"
	if le := index(records, "legal-entity")["global|"+e]; le["name"] != e || le["financialInstitution"] != "true" {
		t.Fatalf("the institution: %v", records[0])
	}
	if got, want := said(messages), []string{"customer-account:ES33:apiscore.duplicate", "customer-account:ES55:apiscore.account_without_holder",
		"customer-account:ES66:apiscore.dates", "customer:doc:P1:apiscore.document_without_country", "own-account::apiscore.no_segregation_column"}; !slices.Equal(got, want) {
		t.Fatalf("messages:\n%v\nwant\n%v", got, want)
	}
	customers, accounts, own := index(records, "customer"), index(records, "customer-account"), index(records, "own-account")
	if len(customers) != 4 || len(accounts) != 6 || len(own) != 2 {
		t.Fatalf("%d customers, %d accounts, %d own accounts", len(customers), len(accounts), len(own))
	}
	if c := customers[e+"|doc:B00000001"]; c["name"] != "EMPRESA UNO SL" || c["partyKind"] != "organization" || c["code"] != "1001" || c["documentType"] != "OTHR" || c["country"] != "ES" {
		t.Fatalf("the company: %v", c)
	}
	if c := customers[e+"|doc:00000001A"]; c["name"] != "JUAN PRUEBA" || c["partyKind"] != "person" || c["fullName"] != "JUAN PRUEBA" {
		t.Fatalf("the person: %v", c)
	}
	if customers[e+"|name:ANA LÓPEZ"]["name"] != "ana lópez" || customers[e+"|doc:P1"]["document"] != "" {
		t.Fatalf("customers: %v", customers)
	}
	if a := accounts[e+"|ES110000"]; a["holder"] != "doc:B00000001" || a["use"] != "customer-payment" || a["status"] != "active" || a["currency"] != "EUR" ||
		a["opened"] != "2024-01-15" || a["virtual"] != "false" || a["demo"] != "false" {
		t.Fatalf("ES11: %v", a)
	}
	if a := accounts[e+"|ES22"]; a["use"] != "agent" || a["status"] != "blocked" || a["currency"] != "USD" || a["opened"] != "2023-05-02" || a["closed"] != "2026-02-01" {
		t.Fatalf("ES22: %v", a)
	}
	if a := accounts[e+"|ES33"]; a["use"] != "player-operating" || a["status"] != "abandoned" || a["currency"] != "EUR" || a["name"] != "Cuenta Operativa de Jugador" {
		t.Fatalf("ES33: %v", a)
	}
	if a := accounts[e+"|CV0000000001"]; a["use"] != "virtual-multicurrency" || a["virtual"] != "true" || a["demo"] != "true" || a["currency"] != "MXN" || a["holder"] != "doc:00000001A" {
		t.Fatalf("the virtual account: %v", a)
	}
	if o := own[e+"|ES1100491111000000000009"]; o["bank"] != "BANCO SANTANDER" || o["bic"] != "BSCHESMMXXX" || o["currency"] != "EUR" || o["segregated"] != "false" ||
		own[e+"|ES4400000000000000000004"]["bank"] != "BANCA MARCH" {
		t.Fatalf("own accounts: %v", own)
	}

	segregated, _, err := domain.Apiscore{Entity: e}.Read([]domain.File{{Role: domain.RoleOwnAccounts, Name: "p.csv",
		Content: "ES_SEGREGADA,IBAN_ALL,BANCO\n1,ES1100491111000000000001,B\n0,ES1100491111000000000002,B\n,ES1100491111000000000003,B\n"}})
	if o := index(segregated, "own-account"); err != nil || len(o) != 3 || o[e+"|ES1100491111000000000001"]["segregated"] != "true" ||
		o[e+"|ES1100491111000000000002"]["segregated"] != "false" || o[e+"|ES1100491111000000000003"]["segregated"] != "false" {
		t.Fatalf("segregated: %v %v", o, err)
	}
	var rule *fw.RuleViolationError
	if _, _, err := (domain.Apiscore{}).Read(files); !errors.As(err, &rule) || rule.Code != "apiscore.no_entity" {
		t.Fatalf("without an institution: %v", err)
	}
	if _, _, err := (domain.Apiscore{Entity: e}).Read(nil); !errors.As(err, &rule) || rule.Code != "apiscore.no_files" {
		t.Fatalf("without files: %v", err)
	}
}
