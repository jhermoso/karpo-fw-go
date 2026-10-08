package host_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	accapp "github.com/jhermoso/karpo-fw-go/contexts/accounting/application"
	finapp "github.com/jhermoso/karpo-fw-go/contexts/financial/application"
	geoinfra "github.com/jhermoso/karpo-fw-go/contexts/geography/infrastructure"
	hrapp "github.com/jhermoso/karpo-fw-go/contexts/hr/application"
	impapp "github.com/jhermoso/karpo-fw-go/contexts/imports/application"
	parapp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pardomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	treapp "github.com/jhermoso/karpo-fw-go/contexts/treasury/application"
	"github.com/jhermoso/karpo-fw-go/host"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
)

// The files of Sage of a group of two companies.
var sageFiles = []impapp.FileDTO{
	{Role: "companies", Name: "empresas.csv", Content: `Code;Name;BaseCurrencyCode
1;MACCORP EXACT CHANGE EP;EUR
2;AIRPORT CHANGE SA;EUR
`},
	{Role: "offices", Name: "oficinas.csv", Content: `CompanyCode;Code;Name;OwnerCompanyName;Address;PostalCode;City;Province;Phone;Email
1;CT-MAD;Centro Madrid;MACCORP EXACT CHANGE EP;Gran Vía 1;28013;Madrid;Madrid;;
`},
	{Role: "persons", Name: "personas.csv", Content: `Dni;FirstName;LastName;Email;Gender
12345678Z;Ana;Garcia;ana@x.invalid;Mujer
00000000T;Carlos;Martinez;carlos@x.invalid;Hombre
`},
	// Sin Fecha left and Sage does not say when; and works at an office nobody declared.
	{Role: "employees", Name: "empleados.csv", Content: `CompanyCode;CompanyName;EmployeeNumber;Dni;FullName;Email;CollectiveAgreementCode;BaseSalary;Irpf;WorkCenterCode;IsActive;HireDate;TerminationDate
1;MACCORP EXACT CHANGE EP;S-MC-001;12345678Z;Ana Garcia;;CONV-01;1800;15;CT-MAD;true;2021-05-01;
1;MACCORP EXACT CHANGE EP;S-MC-002;00000000T;Carlos Martinez;;;1500;;CT-MAD;false;2018-02-01;2023-12-31
1;MACCORP EXACT CHANGE EP;S-MC-003;;Sin Fecha;;;;;CT-XXX;false;2019-01-01;
`},
	// ACME is a customer of both companies, under a code in each.
	{Role: "customers", Name: "clientes.csv", Content: `CompanyCode;CompanyName;CustomerCode;Name;TaxId;Email;PaymentConditionCode;CommissionistCode
1;MACCORP EXACT CHANGE EP;C-001;ACME SUPPLIES SL;B12345674;compras@acme.invalid;CRED-30;COM-01
2;AIRPORT CHANGE SA;C-A2;ACME SUPPLIES SL;B12345674;;CONTADO;
1;MACCORP EXACT CHANGE EP;C-002;GLOBEX SA;A58818501;info@globex.invalid;CONTADO;
`},
	{Role: "suppliers", Name: "proveedores.csv", Content: `CompanyCode;CompanyName;SupplierCode;Name;TaxId;Email;PaymentConditionCode;CommissionistCode
1;MACCORP EXACT CHANGE EP;S-001;PROVEEDOR NORTE SL;;ventas@norte.invalid;CRED-60;COM-02
`},
	// Sage repeats its one catalog for every company.
	{Role: "tax-rates", Name: "catalogos.csv", Content: `CompanyCode;CompanyName;Code;Description;Rate;SurchargeRate;IsExempt;IsNonSubject
1;MACCORP EXACT CHANGE EP;IVA21;IVA general 21%;21;5.2;false;false
1;MACCORP EXACT CHANGE EP;IVA00;Exento;0;;true;false
2;AIRPORT CHANGE SA;IVA21;IVA general 21%;21;5.2;false;false
2;AIRPORT CHANGE SA;IVA00;Exento;0;;true;false
`},
	{Role: "chart", Name: "plancontable.csv", Content: `CompanyCode;Code;Name
1;57;Tesorería
1;570;Caja
1;57000000;Caja, euros
1;43000000;Clientes
9;43000000;De nadie
`},
	// The second is in pounds: Treasury does not keep it.
	{Role: "bank-accounts", Name: "cuentasbancarias.csv", Content: `CompanyCode;BankCode;Iban;Bic;AccountHolderName;LedgerAccountCode
1;0049;ES15 0049 0000 0000 0000 0011;BSCHESMMXXX;MACCORP;57200011
1;0049;ES0900490000000000000022;;MACCORP;57300018
`},
}

const apiscoreHeader = "IBAN,FK_DIVISA,OPERATIVA_DESCRIPCION,ESTADO_DESCRIPCION,ABANDONADA,DEMO,BLOQUEADA,ID_CLIENTE,N_DOCUMENTO,TIPO_DOCUMENTO,TIPO_PERSONA,TITULAR,NOMBRE_APELLIDOS,EMPRESA,FECHA_ALTA,FECHA_BAJA,NACIONALIDAD_CLIENTE\n"

// The files of Apiscore of a payment institution.
var apiscoreFiles = []impapp.FileDTO{
	{Role: "payment-accounts", Name: "cuentas_pago.csv", Content: apiscoreHeader + `ES4400490001112223334445,EUR,Cuenta de Pago normal,Real,0,0,0,1001,B12345674,CIF,JURÍDICA,EMPRESA UNO SL,,EMPRESA UNO SL,2024-01-15,,
ES5500491111000000000001,USD,Cuenta de Agente,Bloqueada Real,0,0,1,1002,12345678Z,DNI,FÍSICA,JUAN PRUEBA FICTICIO,JUAN PRUEBA FICTICIO,,2023-05-02,,ESPAÑOLA
ES2800491111000000000002,EUR,Cuenta de Pago normal,Real,1,0,0,1001,B12345674,CIF,JURÍDICA,EMPRESA UNO SL,,EMPRESA UNO SL,2022-11-30,,
ES9800610000000000000004,EUR,Cuenta de Pago normal,Real,0,0,0,1003,PA1234567,PASAPORTE,FISICA,MARIA VENEZOLANA,MARIA VENEZOLANA,,2023-01-01,2026-02-01,VENEZOLANA
ES7100610000000000000005,EUR,Cuenta de Pago normal,Real,0,0,0,1004,X99,CARNET ALIEN,FISICA,PEDRO SIN PAIS,PEDRO SIN PAIS,,2023-01-01,,
`},
	{Role: "virtual-accounts", Name: "cuentas_cv.csv", Content: apiscoreHeader + `CV0000000001,MXP,Cuenta de Pago normal,Pruebas,0,1,0,1002,12345678Z,DNI,FÍSICA,JUAN PRUEBA FICTICIO,JUAN PRUEBA FICTICIO,,2025-03-10,,
`},
	// The third is in dollars: Treasury does not keep it.
	{Role: "own-accounts", Name: "cuentas_propias.csv", Content: `ES_SEGREGADA,ACTIVO,DIVISA,IBAN,IBAN_ALL,SWIFT,BANCO
0,1,EUR,ES1500490000000000000011,,BMARES2MXXX,BANCA MARCH
1,1,EUR,0049,ES0900490000000000000022,BSCHESMMXXX,BANCO SANTANDER
1,1,USD,2100,ES9121000418450200051332,CAIXESBBXXX,CAIXA USD
`},
}

const apiscoreEntity = "Pagos Uno EP"

type tradeHost struct {
	t    *testing.T
	h    *host.Host
	actx context.Context
	said []string
}

func newTradeHost(t *testing.T, sw *hotswap.Switch) *tradeHost {
	ctx := context.Background()
	h, err := host.Compose(sw, host.Options{JWTSecret: []byte("loaders-test-secret"), ApiscoreEntity: apiscoreEntity})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.Start(ctx); err != nil {
		t.Fatal(err)
	}
	admin, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "importer", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
	admin.GlobalAdmin = true
	return &tradeHost{t: t, h: h, actx: authz.WithContext(ctx, admin)}
}

func (th *tradeHost) run(source string, files []impapp.FileDTO) impapp.RunDTO {
	th.t.Helper()
	run, err := th.h.Imports.Service.Execute.Handle(th.actx, impapp.RunImport{Source: source, Files: files})
	if err != nil {
		th.t.Fatal(err)
	}
	th.said = th.said[:0]
	for _, m := range run.Messages {
		th.said = append(th.said, m.Kind+":"+m.Key+":"+m.Code+" "+m.Text)
	}
	return run
}

func (th *tradeHost) expect(what string, got []impapp.CountDTO, want map[string]impapp.CountDTO) {
	th.t.Helper()
	g := map[string]impapp.CountDTO{}
	for _, c := range got {
		kind := c.Kind
		c.Kind = ""
		g[kind] = c
	}
	for kind, w := range want {
		if g[kind] != w {
			th.t.Fatalf("%s, %s: %+v, want %+v (%v)", what, kind, g[kind], w, th.said)
		}
	}
}

func (th *tradeHost) codes(run impapp.RunDTO) []string {
	out := []string{}
	for _, m := range run.Messages {
		out = append(out, m.Kind+":"+m.Key+":"+m.Code)
	}
	slices.Sort(out)
	return out
}

func (th *tradeHost) companies() map[string]string {
	all, err := th.h.Parties.Organizations.All(context.Background())
	if err != nil {
		th.t.Fatal(err)
	}
	out := map[string]string{}
	for _, c := range all {
		out[c.Name] = c.ID
	}
	return out
}

func (th *tradeHost) parties(q parapp.SearchParties) map[string]string {
	th.t.Helper()
	q.Size = 50
	p, err := th.h.Parties.Service.Search.Handle(th.actx, q)
	if err != nil {
		th.t.Fatal(err)
	}
	out := map[string]string{}
	for _, x := range p.Items {
		out[x.Name] = x.ID
	}
	return out
}

// sageScenario imports the files of Sage of a group and looks in each context for what they became.
func sageScenario(t *testing.T, sw *hotswap.Switch) {
	th := newTradeHost(t, sw)
	h, actx := th.h, th.actx
	sources, err := h.Imports.Service.Sources.Handle(actx, impapp.ListSources{})
	if err != nil || len(sources) != 3 {
		t.Fatalf("sources: %+v %v", sources, err)
	}
	for _, s := range sources {
		if len(s.Unloaded) != 0 {
			t.Fatalf("every kind of %s has who loads it: %v", s.Key, s.Unloaded)
		}
	}

	run := th.run("sage", sageFiles)
	th.expect("first run", run.Counts, map[string]impapp.CountDTO{"legal-entity": {Read: 2, Created: 2}, "work-center": {Read: 1, Created: 1},
		"person": {Read: 3, Created: 3}, "employment": {Read: 3, Created: 3}, "work-place": {Read: 2, Created: 2},
		"customer": {Read: 3, Created: 2, Updated: 1}, "supplier": {Read: 1, Created: 1}, "tax-rate": {Read: 1, Created: 1},
		"ledger-account": {Read: 4, Created: 4}, "own-account": {Read: 2, Created: 1, Failed: 1}})
	got := th.codes(run)
	if want := []string{"employment:S-MC-003:sage.left_without_date", "ledger-account:43000000:sage.unknown_company",
		"own-account:ES0900490000000000000022:imports.own_account_currency", "tax-rate:IVA00:sage.exemption_is_not_a_rate",
		"work-place:S-MC-003:sage.unknown_office"}; !slices.Equal(got, want) {
		t.Fatalf("what the first run said: %v", th.said)
	}

	ids := th.companies()
	maccorp, airport := ids["MACCORP EXACT CHANGE EP"], ids["AIRPORT CHANGE SA"]
	if len(ids) != 2 || maccorp == "" || airport == "" {
		t.Fatalf("companies: %v", ids)
	}
	// People, with their document; their employments, with their dates.
	if byDoc := th.parties(parapp.SearchParties{Document: "12345678Z", Kind: "person"}); len(byDoc) != 1 || byDoc["Ana Garcia"] == "" {
		t.Fatalf("Ana by her document: %v", byDoc)
	}
	jobs, err := h.HR.Service.SearchEmployments.Handle(actx, hrapp.SearchEmployments{Employer: maccorp, Size: 50})
	if err != nil || jobs.Total != 3 {
		t.Fatalf("employments: %+v %v", jobs, err)
	}
	left := map[string]string{}
	for _, e := range jobs.Items {
		left[e.Number] = e.Hired + "/" + e.Terminated
	}
	if left["S-MC-001"] != "2021-05-01/" || left["S-MC-002"] != "2018-02-01/2023-12-31" || left["S-MC-003"] != "2019-01-01/2019-01-01" {
		t.Fatalf("employments: %v", left)
	}
	// Customers and suppliers: ACME is one party, a customer of both companies.
	customer, supplier := pardomain.RoleCustomer.String(), pardomain.RoleSupplier.String()
	mc, ac := th.parties(parapp.SearchParties{Role: customer, Organization: maccorp}), th.parties(parapp.SearchParties{Role: customer, Organization: airport})
	if len(mc) != 2 || len(ac) != 1 || mc["ACME SUPPLIES SL"] == "" || mc["GLOBEX SA"] == "" || ac["ACME SUPPLIES SL"] != mc["ACME SUPPLIES SL"] {
		t.Fatalf("customers: %v %v", mc, ac)
	}
	if byDoc := th.parties(parapp.SearchParties{Document: "A58818501"}); byDoc["GLOBEX SA"] != mc["GLOBEX SA"] {
		t.Fatalf("GLOBEX by its tax number: %v", byDoc)
	}
	if ms := th.parties(parapp.SearchParties{Role: supplier, Organization: maccorp}); len(ms) != 1 || ms["PROVEEDOR NORTE SL"] == "" {
		t.Fatalf("suppliers: %v", ms)
	}
	// The chart: what other accounts hang from is not posted to.
	chart, err := h.Accounting.Service.SearchAccounts.Handle(actx, accapp.SearchAccounts{Company: maccorp})
	if err != nil || len(chart) != 4 {
		t.Fatalf("chart: %+v %v", chart, err)
	}
	for _, a := range chart {
		if a.Postable != (len(a.Code) == 8) {
			t.Fatalf("postable: %+v", a)
		}
	}
	if other, _ := h.Accounting.Service.SearchAccounts.Handle(actx, accapp.SearchAccounts{Company: airport}); len(other) != 0 {
		t.Fatalf("the chart of the other company: %+v", other)
	}
	// The bank account, with what Sage knows of it in its alias.
	banks, err := h.Treasury.Service.SearchAccounts.Handle(actx, treapp.SearchAccounts{Owner: maccorp})
	if err != nil || len(banks) != 1 || banks[0].IBAN != "ES1500490000000000000011" || banks[0].Alias != "0049 0011 (segregada, abandono)" || banks[0].BIC != "BSCHESMMXXX" {
		t.Fatalf("bank accounts: %+v %v", banks, err)
	}

	// The same files again: nothing new, nothing twice.
	again := th.run("sage", sageFiles)
	th.expect("second run", again.Counts, map[string]impapp.CountDTO{"legal-entity": {Read: 2, Unchanged: 2}, "work-center": {Read: 1, Unchanged: 1},
		"person": {Read: 3, Unchanged: 3}, "employment": {Read: 3, Unchanged: 3}, "work-place": {Read: 2, Unchanged: 2},
		"customer": {Read: 3, Unchanged: 3}, "supplier": {Read: 1, Unchanged: 1}, "tax-rate": {Read: 1, Unchanged: 1},
		"ledger-account": {Read: 4, Unchanged: 4}, "own-account": {Read: 2, Unchanged: 1, Failed: 1}})
	if len(th.parties(parapp.SearchParties{Role: customer, Organization: maccorp})) != 2 {
		t.Fatal("no customer twice")
	}
}

// apiscoreScenario imports the files of Apiscore of a payment institution and looks in Parties,
// the finance sector and Treasury for what they became.
func apiscoreScenario(t *testing.T, sw *hotswap.Switch) {
	th := newTradeHost(t, sw)
	h, actx := th.h, th.actx

	run := th.run("apiscore", apiscoreFiles)
	th.expect("first run", run.Counts, map[string]impapp.CountDTO{"legal-entity": {Read: 1, Created: 1}, "customer": {Read: 4, Created: 4},
		"customer-account": {Read: 6, Created: 6}, "own-account": {Read: 3, Created: 2, Failed: 1}})
	if got, want := th.codes(run), []string{"customer:doc:X99:apiscore.document_without_country",
		"own-account:ES9121000418450200051332:imports.own_account_currency"}; !slices.Equal(got, want) {
		t.Fatalf("what the first run said: %v", th.said)
	}
	entity := th.companies()[apiscoreEntity]
	if entity == "" {
		t.Fatal("the institution was not registered")
	}
	// The customers: the company and three persons, those whose document could be kept with it.
	customers := th.parties(parapp.SearchParties{Role: pardomain.RoleCustomer.String(), Organization: entity})
	if len(customers) != 4 || customers["EMPRESA UNO SL"] == "" || customers["JUAN PRUEBA FICTICIO"] == "" || customers["MARIA VENEZOLANA"] == "" {
		t.Fatalf("customers: %v", customers)
	}
	for doc, name := range map[string]string{"B12345674": "EMPRESA UNO SL", "12345678Z": "JUAN PRUEBA FICTICIO", "PA1234567": "MARIA VENEZOLANA"} {
		if byDoc := th.parties(parapp.SearchParties{Document: doc}); len(byDoc) != 1 || byDoc[name] == "" {
			t.Fatalf("%s by its document %s: %v", name, doc, byDoc)
		}
	}
	if byDoc := th.parties(parapp.SearchParties{Document: "X99"}); len(byDoc) != 0 {
		t.Fatalf("a document without a country is not kept: %v", byDoc)
	}
	// The accounts, each as Apiscore says it is.
	account := func(number string) finapp.AccountDTO {
		t.Helper()
		a, err := h.Financial.Service.Get.Handle(actx, finapp.GetAccount{Company: entity, Number: number})
		if err != nil {
			t.Fatalf("%s: %v", number, err)
		}
		return a
	}
	uses := func(a finapp.AccountDTO) string {
		out := []string{}
		for _, u := range a.Uses {
			out = append(out, u.Use)
		}
		return strings.Join(out, ",")
	}
	normal, agent, abandoned := account("ES4400490001112223334445"), account("ES5500491111000000000001"), account("ES2800491111000000000002")
	closed, virtual := account("ES9800610000000000000004"), account("CV0000000001")
	if normal.Status != "active" || normal.Currency != "EUR" || normal.Opened != "2024-01-15" || normal.Holder != customers["EMPRESA UNO SL"] ||
		uses(normal) != "customer-payment" || normal.Virtual || normal.Demo {
		t.Fatalf("the payment account: %+v", normal)
	}
	if agent.Status != "blocked" || agent.Currency != "USD" || uses(agent) != "agent" || agent.Holder != customers["JUAN PRUEBA FICTICIO"] {
		t.Fatalf("the account of the agent: %+v", agent)
	}
	if abandoned.Status != "abandoned" || abandoned.Holder != normal.Holder {
		t.Fatalf("the abandoned account: %+v", abandoned)
	}
	if closed.Status != "closed" || closed.Closed != "2026-02-01" {
		t.Fatalf("the closed account: %+v", closed)
	}
	if !virtual.Virtual || !virtual.Demo || virtual.Currency != "MXN" || uses(virtual) != "virtual-multicurrency" || virtual.Holder != agent.Holder {
		t.Fatalf("the virtual account: %+v", virtual)
	}
	// Its own accounts in banks.
	banks, err := h.Treasury.Service.SearchAccounts.Handle(actx, treapp.SearchAccounts{Owner: entity})
	if err != nil || len(banks) != 2 {
		t.Fatalf("bank accounts: %+v %v", banks, err)
	}
	alias := map[string]string{}
	for _, b := range banks {
		alias[b.IBAN] = b.Alias
	}
	if alias["ES1500490000000000000011"] != "BANCA MARCH 0011" || alias["ES0900490000000000000022"] != "BANCO SANTANDER 0022 (segregada)" {
		t.Fatalf("bank accounts: %v", alias)
	}

	// The same files again: nothing new, nothing twice.
	again := th.run("apiscore", apiscoreFiles)
	th.expect("second run", again.Counts, map[string]impapp.CountDTO{"legal-entity": {Read: 1, Unchanged: 1}, "customer": {Read: 4, Unchanged: 4},
		"customer-account": {Read: 6, Unchanged: 6}, "own-account": {Read: 3, Unchanged: 2, Failed: 1}})
	stats, err := h.Financial.Service.Stats.Handle(actx, finapp.GetStats{Company: entity})
	if err != nil || stats.Total+stats.Demo != 6 && stats.Total != 6 {
		t.Fatalf("no account twice: %+v %v", stats, err)
	}
}

func memorySwitch(t *testing.T) *hotswap.Switch {
	store := memory.NewStore("memory")
	if err := geoinfra.LoadMemory(context.Background(), store); err != nil {
		t.Fatal(err)
	}
	sw := hotswap.New(store)
	t.Cleanup(func() { _ = sw.Close(context.Background()) })
	return sw
}

func sqliteSwitch(t *testing.T) *hotswap.Switch {
	ctx := context.Background()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "karpo.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
	m, err := sqlrepo.NewMigrator(db, host.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return hotswap.New(db)
}

func TestSage_OnMemory(t *testing.T)     { sageScenario(t, memorySwitch(t)) }
func TestSage_OnSQLite(t *testing.T)     { sageScenario(t, sqliteSwitch(t)) }
func TestApiscore_OnMemory(t *testing.T) { apiscoreScenario(t, memorySwitch(t)) }
func TestApiscore_OnSQLite(t *testing.T) { apiscoreScenario(t, sqliteSwitch(t)) }
