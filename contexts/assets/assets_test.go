package assets_test

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
	"github.com/jhermoso/karpo-fw-go/contexts/assets"
	sapp "github.com/jhermoso/karpo-fw-go/contexts/assets/application"
	sinfra "github.com/jhermoso/karpo-fw-go/contexts/assets/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/fiscal"
	fapp "github.com/jhermoso/karpo-fw-go/contexts/fiscal/application"
	finfra "github.com/jhermoso/karpo-fw-go/contexts/fiscal/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/parties"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	pinfra "github.com/jhermoso/karpo-fw-go/contexts/parties/infrastructure"
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

// host composes Parties, Fiscal, Purchases, Assets and Accounting on one hot-swappable backend,
// with one in-process broker carrying the Published Language to Accounting.
type host struct {
	t        *testing.T
	srv      *httptest.Server
	sw       *hotswap.Switch
	dir      *authorization.MemoryDirectory
	ids      map[string]fw.UUID
	tokens   map[string]string
	ast      *assets.Module
	pur      *purchases.Module
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
	sm := assets.Compose(sw)
	am := accounting.Compose(sw)
	broker := inprocess.NewBroker()
	broker.Subscribe("accounting", am.Consumer)

	jwt, _ := jwtauth.New(jwtauth.Config{Secret: []byte("assets-test")})
	dir := authorization.NewMemoryDirectory()
	h := &host{t: t, sw: sw, dir: dir, ids: map[string]fw.UUID{}, tokens: map[string]string{}, ast: sm, pur: um, acc: am, fiscal: fm, parties: pm,
		broker: broker}
	users := map[string][]authz.Permission{
		"keeper":     {sapp.PermAssetRead, sapp.PermAssetUpdate},
		"accountant": {sapp.PermAssetRead, sapp.PermDepreciate, sapp.PermAssetDispose},
		"viewer":     {sapp.PermAssetRead},
		"outsider":   sapp.Permissions(),
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
	sm.RegisterRoutes(mux)
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

// deliver relays the Published Language of Purchases and Assets until quiet.
func (h *host) deliver() {
	for moved := true; moved; {
		moved = false
		for _, r := range []interface {
			RelayOnce(context.Context) (int, error)
		}{h.pur.Relay(h.broker), h.ast.Relay(h.broker)} {
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
	acme, globex := org("Acme", pdomain.RoleInternalOrganization.String()), org("Globex", pdomain.RoleInternalOrganization.String())
	id, _ := pdomain.ParsePartyID(acme.ID)
	_, err := ps.AddIdentification.Handle(ctx, papp.AddIdentification{PartyID: id, DocumentType: "c0000000-0004-0000-0000-000000000003", Country: "ES",
		Number: "A58818501", Primary: true})
	h.ok(err)
	dealer := org("Concesionario Ruiz")
	for _, u := range []string{"keeper", "accountant", "viewer"} {
		h.grant(u, acme.ID)
	}
	h.grant("outsider", globex.ID)

	// Fiscal: the rate and the taxpayer. Accounting: the chart and the posting profile.
	fs := h.fiscal.Service
	_, err = fs.CreateRate.Handle(ctx, fapp.CreateRate{Type: "vat", Territory: "common", Code: "G21", Description: "G21", Rate: "21", From: vocab.MustDate(2012, 9, 1)})
	h.ok(err)
	_, err = fs.RegisterTaxpayer.Handle(ctx, fapp.RegisterTaxpayer{Organization: acme.ID, TermsInput: fapp.TermsInput{Territory: "common", FiscalYearStartMonth: 1}})
	h.ok(err)
	for code, name := range map[string]string{"4000": "Proveedores", "4720": "HP IVA soportado", "2180": "Inmovilizado material",
		"2810": "Amortización acumulada", "6810": "Amortización del inmovilizado", "5430": "Créditos por enajenación de inmovilizado",
		"6710": "Pérdidas del inmovilizado", "7710": "Beneficios del inmovilizado"} {
		_, err := h.acc.Service.CreateAccount.Handle(ctx, aapp.CreateAccount{Company: acme.ID, Code: code, Name: name, Postable: true})
		h.ok(err)
	}
	_, err = h.acc.Service.OpenLedger.Handle(ctx, aapp.OpenLedger{Company: acme.ID, StartMonth: 1, Accounts: map[string]string{"suppliers": "4000",
		"input-tax": "4720", "fixed-assets": "2180", "accumulated-depreciation": "2810", "depreciation-expense": "6810",
		"asset-sale-receivable": "5430", "asset-disposal-loss": "6710", "asset-disposal-gain": "7710"}})
	h.ok(err)

	// The invoice of a van and a computer is an investment, not an expense.
	_, err = h.pur.Service.RegisterInvoice.Handle(ctx, uapp.RegisterInvoice{Company: acme.ID, Supplier: dealer.ID, SupplierNumber: "V-2026-17",
		Issued: vocab.MustDate(2026, 1, 9), Received: vocab.MustDate(2026, 1, 10), Due: vocab.MustDate(2026, 2, 9), Total: "15972.00",
		Lines: []uapp.LineInput{{Description: "Furgoneta", Category: "fixed-asset", Base: "12000", TaxCode: "G21"},
			{Description: "Ordenador", Category: "fixed-asset", Base: "1200", TaxCode: "G21"}}})
	h.ok(err)
	h.deliver()
	h.expect(acme.ID, map[string]string{"2180": "13200.00", "4720": "2772.00", "4000": "-15972.00"})

	// The register: the van (residual value, 48 months), the computer (24 months) and a plot of land.
	van := map[string]any{"company": acme.ID, "code": "fur-01", "name": "Furgoneta", "class": "vehicles", "serial": "1234-ABC", "supplier": dealer.ID,
		"document": "V-2026-17", "acquired": "2026-01-10", "inService": "2026-01-16", "cost": "12000", "residual": "2000", "lifeMonths": 48}
	var a1, a2, land sapp.AssetDTO
	h.must(h.do("POST", "/api/assets", "viewer", van, nil), 403, "viewer")
	h.must(h.do("POST", "/api/assets", "outsider", van, nil), 404, "outsider")
	h.must(h.do("POST", "/api/assets", "keeper", map[string]any{"company": acme.ID, "code": "X", "name": "Barco", "class": "boats", "acquired": "2026-01-10",
		"cost": "1", "lifeMonths": 12}, nil), 400, "class")
	h.must(h.do("POST", "/api/assets", "keeper", van, &a1), 201, "van")
	h.must(h.do("POST", "/api/assets", "keeper", van, nil), 422, "code used once")
	h.must(h.do("POST", "/api/assets", "keeper", map[string]any{"company": acme.ID, "code": "ORD-01", "name": "Ordenador", "class": "computers",
		"acquired": "2026-02-01", "cost": "1200", "lifeMonths": 24}, &a2), 201, "computer")
	h.must(h.do("POST", "/api/assets", "keeper", map[string]any{"company": acme.ID, "code": "TER-01", "name": "Solar", "class": "land",
		"acquired": "2020-05-01", "cost": "50000"}, &land), 201, "land")
	if a1.Code != "FUR-01" || a1.Monthly != "208.33" || a1.NetBookValue != "12000.00" || a1.Status != "in-service" || a2.InService != "2026-02-01" ||
		a2.Monthly != "50.00" || land.Monthly != "0.00" {
		t.Fatalf("register: %+v / %+v / %+v", a1, a2, land)
	}

	// The depreciation of the first quarter: the van from 16 January, the computer from February.
	march := map[string]any{"company": acme.ID, "year": 2026, "month": 3}
	var run sapp.RunDTO
	h.must(h.do("POST", "/api/assets/depreciation", "keeper", march, nil), 403, "keeping the register is not depreciating")
	h.must(h.do("POST", "/api/assets/depreciation", "accountant", map[string]any{"company": acme.ID, "year": 2026, "month": 13}, nil), 400, "month")
	h.must(h.do("POST", "/api/assets/depreciation", "accountant", march, &run), 200, "first quarter")
	if run.Period != "2026-03" || run.Assets != 2 || run.Charges != 5 || run.Total != "624.19" {
		t.Fatalf("first quarter: %+v", run)
	}
	h.must(h.do("POST", "/api/assets/depreciation", "accountant", march, &run), 200, "first quarter again")
	if run.Assets != 0 || run.Charges != 0 || run.Total != "0.00" {
		t.Fatalf("a month is charged once: %+v", run)
	}
	h.deliver()
	h.expect(acme.ID, map[string]string{"6810": "624.19", "2810": "-624.19", "2180": "13200.00"})
	var book sapp.BookDTO
	h.must(h.do("GET", "/api/assets/book?company="+acme.ID, "viewer", nil, &book), 200, "book")
	if len(book.Assets) != 3 || book.Cost != "63200.00" || book.Accumulated != "624.19" || book.NetBookValue != "62575.81" {
		t.Fatalf("book: %+v", book)
	}

	// The computer breaks in April and is scrapped: its net book value is a loss.
	scrap := map[string]any{"date": "2026-04-15", "kind": "scrap"}
	h.must(h.do("POST", "/api/assets/"+a2.ID+"/dispose", "keeper", scrap, nil), 403, "keeping the register is not disposing")
	h.must(h.do("POST", "/api/assets/"+a2.ID+"/dispose", "accountant", map[string]any{"date": "2026-03-15", "kind": "scrap"}, nil), 422, "month already depreciated")
	h.must(h.do("POST", "/api/assets/"+a2.ID+"/dispose", "accountant", scrap, &a2), 200, "scrap")
	if a2.Status != "disposed" || a2.Accumulated != "100.00" || a2.Result != "-1100.00" {
		t.Fatalf("scrapped: %+v", a2)
	}
	// The van is sold in July for 11,000: depreciated through June first, a gain of 149.18.
	h.must(h.do("POST", "/api/assets/"+a1.ID+"/dispose", "accountant", map[string]any{"date": "2026-07-10", "kind": "sale", "proceeds": "11000"}, &a1), 200, "sale")
	if a1.Accumulated != "1149.18" || a1.Result != "149.18" || len(a1.Charges) != 6 || a1.Disposed != "2026-07-10" || a1.Proceeds != "11000.00" {
		t.Fatalf("sold: %+v", a1)
	}
	h.must(h.do("POST", "/api/assets/"+a1.ID+"/dispose", "accountant", scrap, nil), 422, "disposed once")
	h.deliver()
	h.expect(acme.ID, map[string]string{"2180": "0.00", "2810": "0.00", "6810": "1249.18", "5430": "11000.00", "6710": "1100.00", "7710": "-149.18"})

	// Nothing is left to depreciate: land does not, and the others are gone.
	h.must(h.do("POST", "/api/assets/depreciation", "accountant", map[string]any{"company": acme.ID, "year": 2026, "month": 12}, &run), 200, "year end")
	if run.Assets != 0 || run.Total != "0.00" {
		t.Fatalf("year end: %+v", run)
	}

	h.must(h.do("PUT", "/api/assets/"+land.ID, "accountant", map[string]any{"name": "Solar norte"}, nil), 403, "depreciating is not keeping the register")
	h.must(h.do("PUT", "/api/assets/"+land.ID, "keeper", map[string]any{"name": "Solar norte"}, &land), 200, "change")
	h.must(h.do("GET", "/api/assets/"+land.ID, "outsider", nil, nil), 404, "outsider")
	var got sapp.AssetDTO
	h.must(h.do("GET", "/api/assets/"+a1.ID, "viewer", nil, &got), 200, "get")
	if got.Status != "disposed" || len(got.Charges) != 6 || got.Charges[0].Period != "2026-01" || got.Charges[0].Amount != "107.53" || got.Supplier != dealer.ID {
		t.Fatalf("van: %+v", got)
	}
	var page fw.Page[sapp.AssetDTO]
	h.must(h.do("GET", "/api/assets?company="+acme.ID+"&inService=true", "viewer", nil, &page), 200, "in service")
	if len(page.Items) != 1 || page.Items[0].Name != "Solar norte" {
		t.Fatalf("in service: %+v", page.Items)
	}
	h.must(h.do("GET", "/api/assets?class=vehicles", "viewer", nil, &page), 200, "vehicles")
	if len(page.Items) != 1 || page.Items[0].Code != "FUR-01" {
		t.Fatalf("vehicles: %+v", page.Items)
	}
	h.must(h.do("GET", "/api/assets", "outsider", nil, &page), 200, "outsider's register")
	if len(page.Items) != 0 {
		t.Fatalf("outsider: %+v", page.Items)
	}
}

func TestAssets_DepreciatesAndDisposes_MemoryThenSQLite(t *testing.T) {
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
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{pinfra.Migrations(), finfra.Migrations(), uinfra.Migrations(), sinfra.Migrations(),
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

func TestAssets_LayersRespectTheArchitecture(t *testing.T) {
	const m = "github.com/jhermoso/karpo-fw-go"
	archtest.AssertOnlyImports(t, "domain", false, archtest.Std, m+"/pkg/domain/...", m+"/contexts/assets/domain")
	archtest.AssertOnlyImports(t, "contracts", false, archtest.Std)
	archtest.AssertTreeDoesNotImport(t, "application", []string{"/pkg/persistence", "/pkg/distribution", "database/sql", "net/http"})
}
