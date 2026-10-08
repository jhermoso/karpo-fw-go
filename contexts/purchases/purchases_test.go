package purchases_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jhermoso/karpo-fw-go/contexts/accounting"
	aapp "github.com/jhermoso/karpo-fw-go/contexts/accounting/application"
	ainfra "github.com/jhermoso/karpo-fw-go/contexts/accounting/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/fiscal"
	fapp "github.com/jhermoso/karpo-fw-go/contexts/fiscal/application"
	fdomain "github.com/jhermoso/karpo-fw-go/contexts/fiscal/domain"
	finfra "github.com/jhermoso/karpo-fw-go/contexts/fiscal/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/parties"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	pinfra "github.com/jhermoso/karpo-fw-go/contexts/parties/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/payments"
	yapp "github.com/jhermoso/karpo-fw-go/contexts/payments/application"
	yinfra "github.com/jhermoso/karpo-fw-go/contexts/payments/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/purchases"
	uapp "github.com/jhermoso/karpo-fw-go/contexts/purchases/application"
	uinfra "github.com/jhermoso/karpo-fw-go/contexts/purchases/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authorization"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution/jwtauth"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/messaging/inprocess"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
	"github.com/jhermoso/karpo-fw-go/pkg/testing/archtest"
)

// host composes Parties, Fiscal, Purchases, Payments and Accounting on one hot-swappable backend,
// with one in-process broker carrying the Published Language between them.
type host struct {
	t        *testing.T
	srv      *httptest.Server
	sw       *hotswap.Switch
	dir      *authorization.MemoryDirectory
	ids      map[string]fw.UUID
	tokens   map[string]string
	pur      *purchases.Module
	pay      *payments.Module
	acc      *accounting.Module
	fiscal   *fiscal.Module
	parties  *parties.Module
	broker   *inprocess.Broker
	adminCtx context.Context
}

func compose(t *testing.T) *host {
	ctx := context.Background()
	sw := hotswap.New(memory.NewStore("memory"))
	pm := parties.Compose(sw, nil)
	fm := fiscal.Compose(sw, finfra.PartiesIdentities{TaxIdentities: pm.TaxIdentities})
	um := purchases.Compose(sw, uinfra.FiscalTaxes{Engine: fm.TaxEngine})
	ym := payments.Compose(sw, nil)
	am := accounting.Compose(sw)
	broker := inprocess.NewBroker()
	broker.Subscribe("payments", ym.Consumer)
	broker.Subscribe("accounting", am.Consumer)
	broker.Subscribe("fiscal", fm.Consumer)

	jwt, _ := jwtauth.New(jwtauth.Config{Secret: []byte("purchases-test")})
	dir := authorization.NewMemoryDirectory()
	h := &host{t: t, sw: sw, dir: dir, ids: map[string]fw.UUID{}, tokens: map[string]string{}, pur: um, pay: ym, acc: am, fiscal: fm, parties: pm,
		broker: broker}
	all := []authz.Permission{uapp.PermInvoiceRead, uapp.PermInvoiceRegister, uapp.PermInvoiceCancel, uapp.PermSupplierRead, uapp.PermSupplierUpdate}
	users := map[string][]authz.Permission{
		"clerk":     {uapp.PermInvoiceRead, uapp.PermInvoiceRegister, uapp.PermSupplierRead, uapp.PermSupplierUpdate},
		"canceller": {uapp.PermInvoiceRead, uapp.PermInvoiceCancel},
		"viewer":    {uapp.PermInvoiceRead, uapp.PermSupplierRead},
		"outsider":  all,
	}
	for u, perms := range users {
		h.ids[u] = fw.NewUUID()
		dir.Put(h.ids[u], authz.Subject{Active: true, Permissions: perms})
		tok, _ := jwt.Issue(jwtauth.Claims{Subject: h.ids[u].String(), Username: u, PartyID: fw.NewUUID().String(), ExpiresAt: fw.Now().Add(time.Hour).Unix()})
		h.tokens[u] = "Bearer " + tok
	}
	ac, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "setup", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
	ac.GlobalAdmin = true
	h.adminCtx = authz.WithContext(ctx, ac)

	mux := http.NewServeMux()
	um.RegisterRoutes(mux)
	h.srv = httptest.NewServer(distribution.Chain(mux, distribution.Authorize(jwt, authorization.NewResolver(dir, authorization.Options{}))))
	t.Cleanup(h.srv.Close)
	t.Cleanup(func() { _ = sw.Close(ctx) })
	return h
}

func (h *host) grant(user string, orgs ...string) {
	s, _, _ := h.dir.Subject(context.Background(), h.ids[user])
	for _, o := range orgs {
		s.Grants = append(s.Grants, authz.Grant{OrganizationID: fw.MustParseUUID(o), Level: authz.Full})
	}
	h.dir.Put(h.ids[user], s)
}

func (h *host) do(method, path, user string, body, out any) int {
	h.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest(method, h.srv.URL+path, &buf)
	req.Header.Set("Authorization", h.tokens[user])
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer res.Body.Close()
	if out != nil && res.StatusCode < 300 {
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			h.t.Fatal(err)
		}
	}
	return res.StatusCode
}

func (h *host) must(got, want int, what string) {
	h.t.Helper()
	if got != want {
		h.t.Fatalf("%s: status %d, want %d", what, got, want)
	}
}

func (h *host) ok(err error) {
	h.t.Helper()
	if err != nil {
		h.t.Fatal(err)
	}
}

// deliver relays the Published Language of Purchases and Payments until quiet.
func (h *host) deliver() {
	for moved := true; moved; {
		moved = false
		for _, r := range []interface {
			RelayOnce(context.Context) (int, error)
		}{h.pur.Relay(h.broker), h.pay.Relay(h.broker)} {
			n, err := r.RelayOnce(context.Background())
			h.ok(err)
			moved = moved || n > 0
		}
	}
}

func (h *host) expect(company string, want map[string]string) {
	h.t.Helper()
	rows, err := h.acc.Service.TrialBalance.Handle(h.adminCtx, aapp.TrialBalance{Company: company})
	h.ok(err)
	got := map[string]string{}
	sum := vocab.DecimalFromInt(0)
	for _, r := range rows {
		got[r.Account] = r.Balance
		sum = sum.Add(vocab.MustDecimal(r.Balance))
	}
	for a, b := range want {
		if got[a] != b {
			h.t.Fatalf("balance of %s = %q, want %s (all: %v)", a, got[a], b, got)
		}
	}
	if !sum.IsZero() {
		h.t.Fatalf("the trial balance does not balance: %v", got)
	}
}

func (h *host) scenario(tag string) {
	t := h.t
	ctx := h.adminCtx
	ps := h.parties.Service
	org := func(name string, roles ...string) papp.PartyDTO {
		o, err := ps.RegisterOrganization.Handle(ctx, papp.RegisterOrganization{LegalName: name + " " + tag, Roles: roles})
		h.ok(err)
		return o
	}
	doc := func(party, docType, number string) {
		id, _ := pdomain.ParsePartyID(party)
		_, err := ps.AddIdentification.Handle(ctx, papp.AddIdentification{PartyID: id, DocumentType: docType, Country: "ES", Number: number, Primary: true})
		h.ok(err)
	}
	acme, globex := org("Acme", pdomain.RoleInternalOrganization.String()), org("Globex", pdomain.RoleInternalOrganization.String())
	doc(acme.ID, "c0000000-0004-0000-0000-000000000003", "A58818501")
	papeleria := org("Papelería Núñez")
	asesor, err := ps.RegisterPerson.Handle(ctx, papp.RegisterPerson{GivenName: "Luis", FirstSurname: "Asesor " + tag})
	h.ok(err)
	doc(asesor.ID, "c0000000-0004-0000-0000-000000000002", "12345678Z")
	for _, u := range []string{"clerk", "canceller", "viewer"} {
		h.grant(u, acme.ID)
	}
	h.grant("outsider", globex.ID)

	// Fiscal: rates, taxpayer and its Modelo 111. Accounting: the chart and the ledger.
	fs := h.fiscal.Service
	for code, rate := range map[string]string{"G21": "21", "R10": "10"} {
		_, err := fs.CreateRate.Handle(ctx, fapp.CreateRate{Type: "vat", Territory: "common", Code: code, Description: code, Rate: rate, From: vocab.MustDate(2012, 9, 1)})
		h.ok(err)
	}
	tp, err := fs.RegisterTaxpayer.Handle(ctx, fapp.RegisterTaxpayer{Organization: acme.ID, TermsInput: fapp.TermsInput{Territory: "common", FiscalYearStartMonth: 1}})
	h.ok(err)
	tpid, _ := fdomain.ParseTaxpayerID(tp.ID)
	_, err = fs.AddObligation.Handle(ctx, fapp.AddObligation{ID: tpid, Form: "111", Periodicity: "quarterly", FromYear: 2020})
	h.ok(err)
	for code, name := range map[string]string{"4000": "Proveedores", "4720": "HP IVA soportado", "4751": "HP acreedora por retenciones", "6000": "Compras",
		"6230": "Servicios de profesionales", "6280": "Suministros"} {
		_, err := h.acc.Service.CreateAccount.Handle(ctx, aapp.CreateAccount{Company: acme.ID, Code: code, Name: name, Postable: true})
		h.ok(err)
	}
	_, err = h.acc.Service.OpenLedger.Handle(ctx, aapp.OpenLedger{Company: acme.ID, StartMonth: 1, Accounts: map[string]string{"suppliers": "4000",
		"input-tax": "4720", "withholding-payable": "4751", "purchases": "6000", "professional-services": "6230", "supplies": "6280"}})
	h.ok(err)

	// The adviser's profile: professional services, 15% withheld, 30 days, paid to his account.
	h.must(h.do("PUT", "/api/purchases/suppliers", "viewer", map[string]any{"company": acme.ID, "supplier": asesor.ID}, nil), 403, "viewer")
	h.must(h.do("PUT", "/api/purchases/suppliers", "clerk", map[string]any{"company": acme.ID, "supplier": asesor.ID, "category": "food"}, nil), 400, "category")
	var prof uapp.SupplierDTO
	h.must(h.do("PUT", "/api/purchases/suppliers", "clerk", map[string]any{"company": acme.ID, "supplier": asesor.ID, "category": "professional-services",
		"withholdingRate": "15", "paymentDays": 30, "iban": "ES7921000813610123456789"}, &prof), 200, "supplier profile")

	// The adviser's invoice, booked with the defaults of his profile.
	adviser := map[string]any{"company": acme.ID, "supplier": asesor.ID, "supplierNumber": "2026/0042", "issued": "2026-09-30", "received": "2026-10-02",
		"lines": []map[string]any{{"description": "Asesoría fiscal septiembre", "base": "1000", "taxCode": "G21"}}, "total": "1210.00"}
	var inv1 uapp.InvoiceDTO
	h.must(h.do("POST", "/api/purchases/invoices", "outsider", adviser, nil), 404, "outsider")
	h.must(h.do("POST", "/api/purchases/invoices", "viewer", adviser, nil), 403, "viewer")
	wrong := map[string]any{}
	for k, v := range adviser {
		wrong[k] = v
	}
	wrong["total"] = "1209.99"
	h.must(h.do("POST", "/api/purchases/invoices", "clerk", wrong, nil), 422, "total printed on the invoice")
	h.must(h.do("POST", "/api/purchases/invoices", "clerk", adviser, &inv1), 201, "adviser's invoice")
	h.must(h.do("POST", "/api/purchases/invoices", "clerk", adviser, nil), 422, "booked once")
	if inv1.Register != "FR-2026-000001" || inv1.Due != "2026-10-30" || inv1.Withholding != "150.00" || inv1.Payable != "1060.00" ||
		len(inv1.PayTo) != 1 || inv1.PayTo[0].Amount != "1060.00" || inv1.Lines[0].Category != "professional-services" || inv1.Taxes[0].Amount != "210.00" {
		t.Fatalf("adviser's invoice: %+v", inv1)
	}

	// The stationer's invoice (two rates), a non-deductible one and a corrective one.
	var inv2, inv3, inv4 uapp.InvoiceDTO
	h.must(h.do("POST", "/api/purchases/invoices", "clerk", map[string]any{"company": acme.ID, "supplier": papeleria.ID, "supplierNumber": "A-77",
		"issued": "2026-10-01", "received": "2026-10-03", "due": "2026-10-31", "withholdingRate": "15", "total": "176.00",
		"lines": []map[string]any{{"category": "goods", "base": "100", "taxCode": "G21"}, {"category": "supplies", "base": "50", "taxCode": "R10"}}},
		nil), 422, "withholding without professional services")
	h.must(h.do("POST", "/api/purchases/invoices", "clerk", map[string]any{"company": acme.ID, "supplier": papeleria.ID, "supplierNumber": "A-77",
		"issued": "2026-10-01", "received": "2026-10-03", "due": "2026-10-31", "total": "176.00",
		"lines": []map[string]any{{"category": "goods", "base": "100", "taxCode": "G21"}, {"category": "supplies", "base": "50", "taxCode": "R10"}}},
		&inv2), 201, "stationer's invoice")
	h.must(h.do("POST", "/api/purchases/invoices", "clerk", map[string]any{"company": acme.ID, "supplier": papeleria.ID, "supplierNumber": "A-78",
		"issued": "2026-10-01", "received": "2026-10-03", "due": "2026-10-31", "total": "242.00", "nonDeductible": true,
		"lines": []map[string]any{{"category": "supplies", "base": "200", "taxCode": "G21"}}}, &inv3), 201, "non-deductible invoice")
	h.must(h.do("POST", "/api/purchases/invoices", "clerk", map[string]any{"company": acme.ID, "supplier": papeleria.ID, "supplierNumber": "AR-1",
		"issued": "2026-10-04", "received": "2026-10-05", "due": "2026-10-05", "total": "-24.20", "corrects": inv2.ID,
		"lines": []map[string]any{{"category": "goods", "base": "-20", "taxCode": "G21"}}}, &inv4), 201, "corrective invoice")
	if inv2.Tax != "26.00" || inv2.Register != "FR-2026-000002" || inv4.Payable != "-24.20" || inv4.Corrects != inv2.ID {
		t.Fatalf("stationer: %+v / %+v", inv2, inv4)
	}
	h.deliver()

	// Payments owes what is paid to each supplier (nothing for the corrective one).
	pays, err := h.pay.Service.SearchPayables.Handle(ctx, yapp.SearchPayables{Company: acme.ID, Kind: "supplier-invoice"})
	h.ok(err)
	byDoc := map[string]yapp.PayableDTO{}
	for _, p := range pays.Items {
		byDoc[p.Document] = p
	}
	if len(pays.Items) != 3 || byDoc["2026/0042"].Amount != "1060.00" || byDoc["2026/0042"].Due != "2026-10-30" || len(byDoc["2026/0042"].PayTo) != 1 ||
		byDoc["A-77"].Amount != "176.00" || byDoc["A-78"].Amount != "242.00" {
		t.Fatalf("payables: %+v", pays.Items)
	}

	// Accounting: expenses and input tax against suppliers and the withholding.
	h.expect(acme.ID, map[string]string{"6230": "1000.00", "6000": "80.00", "6280": "292.00", "4720": "231.80", "4751": "-150.00", "4000": "-1453.80"})

	// Fiscal: the withholding enters the Modelo 111 of the third quarter (invoice date).
	q3, err := fs.GenerateFiling.Handle(ctx, fapp.GenerateFiling{Organization: acme.ID, Form: "111", Year: 2026, Period: "3T"})
	if err != nil || q3.Withheld != "150.00" || q3.Recipients != 1 || len(q3.Lines) != 1 || q3.Lines[0].Key != "G" || q3.Lines[0].Perceptions != "1000.00" {
		t.Fatalf("Modelo 111: %+v %v", q3, err)
	}

	// The adviser's invoice was booked by mistake: cancelling it withdraws the payable, reverses
	// the entry and the withholding.
	h.must(h.do("POST", "/api/purchases/invoices/"+inv1.ID+"/cancel", "clerk", map[string]any{"reason": "duplicada"}, nil), 403, "registering is not cancelling")
	h.must(h.do("POST", "/api/purchases/invoices/"+inv1.ID+"/cancel", "canceller", map[string]any{"reason": "duplicada"}, &inv1), 200, "cancel")
	h.must(h.do("POST", "/api/purchases/invoices/"+inv1.ID+"/cancel", "canceller", map[string]any{"reason": "duplicada"}, nil), 422, "cancelled once")
	h.deliver()
	if p, err := h.pay.Service.SearchPayables.Handle(ctx, yapp.SearchPayables{Company: acme.ID, Kind: "supplier-invoice", OpenOnly: true}); err != nil || len(p.Items) != 2 {
		t.Fatalf("payable withdrawn: %+v %v", p.Items, err)
	}
	h.expect(acme.ID, map[string]string{"6230": "0.00", "4751": "0.00", "4000": "-393.80"})
	q3, err = fs.GenerateFiling.Handle(ctx, fapp.GenerateFiling{Organization: acme.ID, Form: "111", Year: 2026, Period: "3T"})
	if err != nil || q3.Withheld != "0.00" {
		t.Fatalf("Modelo 111 after the cancellation: %+v %v", q3, err)
	}
	// Booked again, now with the next number of the register.
	h.must(h.do("POST", "/api/purchases/invoices", "clerk", adviser, &inv1), 201, "booked again")
	if inv1.Register != "FR-2026-000005" {
		t.Fatalf("register: %s", inv1.Register)
	}

	var register fw.Page[uapp.InvoiceDTO]
	h.must(h.do("GET", "/api/purchases/invoices?company="+acme.ID+"&from=2026-10-03", "viewer", nil, &register), 200, "register")
	if len(register.Items) != 3 || register.Items[0].Register != "FR-2026-000002" {
		t.Fatalf("register from 3 October: %+v", register.Items)
	}
	h.must(h.do("GET", "/api/purchases/invoices/"+inv2.ID, "outsider", nil, nil), 404, "outsider")
	var sups []uapp.SupplierDTO
	h.must(h.do("GET", "/api/purchases/suppliers?company="+acme.ID, "viewer", nil, &sups), 200, "suppliers")
	if len(sups) != 1 || sups[0].WithholdingRate != "15.00" || sups[0].IBAN != "ES7921000813610123456789" {
		t.Fatalf("suppliers: %+v", sups)
	}
}

func TestPurchases_BooksReceivedInvoices_MemoryThenSQLite(t *testing.T) {
	h := compose(t)
	h.scenario("m")

	ctx := context.Background()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "host.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{pinfra.Migrations(), finfra.Migrations(), uinfra.Migrations(), yinfra.Migrations(),
		ainfra.Migrations()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.sw.Swap(ctx, db); err != nil {
		t.Fatal(err)
	}
	h.scenario("s")
}

func TestPurchases_LayersRespectTheArchitecture(t *testing.T) {
	const m = "github.com/jhermoso/karpo-fw-go"
	archtest.AssertOnlyImports(t, "domain", false, archtest.Std, m+"/pkg/domain/...", m+"/contexts/purchases/domain")
	archtest.AssertOnlyImports(t, "contracts", false, archtest.Std)
	archtest.AssertTreeDoesNotImport(t, "application", []string{"/pkg/persistence", "/pkg/distribution", "database/sql", "net/http"})
}
