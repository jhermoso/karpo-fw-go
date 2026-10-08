package billing_test

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

	"github.com/jhermoso/karpo-fw-go/contexts/billing"
	bapp "github.com/jhermoso/karpo-fw-go/contexts/billing/application"
	"github.com/jhermoso/karpo-fw-go/contexts/billing/contracts"
	binfra "github.com/jhermoso/karpo-fw-go/contexts/billing/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/fiscal"
	fapp "github.com/jhermoso/karpo-fw-go/contexts/fiscal/application"
	finfra "github.com/jhermoso/karpo-fw-go/contexts/fiscal/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/parties"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	pinfra "github.com/jhermoso/karpo-fw-go/contexts/parties/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authorization"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
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

// host composes Parties, Fiscal and Billing on one hot-swappable backend.
type host struct {
	t        *testing.T
	srv      *httptest.Server
	sw       *hotswap.Switch
	dir      *authorization.MemoryDirectory
	ids      map[string]fw.UUID
	tokens   map[string]string
	billing  *billing.Module
	fiscal   *fiscal.Module
	parties  *parties.Module
	adminCtx context.Context
}

func compose(t *testing.T) *host {
	ctx := context.Background()
	sw := hotswap.New(memory.NewStore("memory"))
	pm := parties.Compose(sw, nil)
	fm := fiscal.Compose(sw, finfra.PartiesIdentities{TaxIdentities: pm.TaxIdentities})
	bm := billing.Compose(sw, binfra.FiscalTaxes{Engine: fm.TaxEngine}, binfra.PartiesIdentities{TaxIdentities: pm.TaxIdentities})

	jwt, _ := jwtauth.New(jwtauth.Config{Secret: []byte("billing-test")})
	dir := authorization.NewMemoryDirectory()
	h := &host{t: t, sw: sw, dir: dir, ids: map[string]fw.UUID{}, tokens: map[string]string{}, billing: bm, fiscal: fm, parties: pm}
	prepare := []authz.Permission{bapp.PermInvoiceRead, bapp.PermInvoiceCreate, bapp.PermInvoiceUpdate, bapp.PermSeriesRead, bapp.PermSeriesUpdate}
	users := map[string][]authz.Permission{"clerk": prepare, "issuer": {bapp.PermInvoiceRead, bapp.PermInvoiceIssue}, "outsider": prepare}
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
	bm.RegisterRoutes(mux)
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

func (h *host) scenario(tag string) {
	t := h.t
	ctx := h.adminCtx
	ps := h.parties.Service

	// Parties: the seller with its CIF, two customers with their DNI.
	acme, err := ps.RegisterOrganization.Handle(ctx, papp.RegisterOrganization{LegalName: "Acme " + tag, Roles: []string{pdomain.RoleInternalOrganization.String()}})
	h.ok(err)
	globex, err := ps.RegisterOrganization.Handle(ctx, papp.RegisterOrganization{LegalName: "Globex " + tag, Roles: []string{pdomain.RoleInternalOrganization.String()}})
	h.ok(err)
	h.grant("clerk", acme.ID)
	h.grant("issuer", acme.ID)
	h.grant("outsider", globex.ID)
	doc := func(party, docType, number string) {
		id, _ := pdomain.ParsePartyID(party)
		_, err := ps.AddIdentification.Handle(ctx, papp.AddIdentification{PartyID: id, DocumentType: docType, Country: "ES", Number: number, Primary: true})
		h.ok(err)
	}
	doc(acme.ID, "c0000000-0004-0000-0000-000000000003", "A58818501")
	person := func(name, dni string) papp.PartyDTO {
		p, err := ps.RegisterPerson.Handle(ctx, papp.RegisterPerson{GivenName: name, FirstSurname: tag})
		h.ok(err)
		doc(p.ID, "c0000000-0004-0000-0000-000000000002", dni)
		return p
	}
	ana, bea := person("Ana", "12345678Z"), person("Bea", "00000000T")

	// Fiscal: rates of the common territory and the seller's fiscal profile.
	fs := h.fiscal.Service
	for _, r := range []fapp.CreateRate{
		{Type: "vat", Territory: "common", Code: "G21", Description: "General", Rate: "21", Surcharge: "5.2", From: vocab.MustDate(2012, 9, 1)},
		{Type: "vat", Territory: "common", Code: "R10", Description: "Reducido", Rate: "10", Surcharge: "1.4", From: vocab.MustDate(2012, 9, 1)},
	} {
		_, err := fs.CreateRate.Handle(ctx, r)
		h.ok(err)
	}
	_, err = fs.CreateTreatment.Handle(ctx, fapp.CreateTreatment{Territory: "common", Code: "E1", Description: "Exenta art. 20", Kind: "exempt"})
	h.ok(err)
	_, err = fs.RegisterTaxpayer.Handle(ctx, fapp.RegisterTaxpayer{Organization: acme.ID, TermsInput: fapp.TermsInput{Territory: "common", FiscalYearStartMonth: 1}})
	h.ok(err)

	// Series: ordinary and corrective, one per code and year.
	var fa, rs bapp.SeriesDTO
	h.must(h.do("POST", "/api/billing/series", "clerk", map[string]any{"seller": acme.ID, "code": "fa", "year": 2026}, &fa), 201, "series")
	h.must(h.do("POST", "/api/billing/series", "clerk", map[string]any{"seller": acme.ID, "code": "FA", "year": 2026}, nil), 422, "duplicate series")
	h.must(h.do("POST", "/api/billing/series", "clerk", map[string]any{"seller": acme.ID, "code": "R", "year": 2026, "corrective": true}, &rs), 201, "corrective series")
	h.must(h.do("POST", "/api/billing/series", "outsider", map[string]any{"seller": acme.ID, "code": "X", "year": 2026}, nil), 404, "outsider")

	// An invoice with three rates: taxes on the aggregated base per rate.
	var inv bapp.InvoiceDTO
	h.must(h.do("POST", "/api/billing/invoices", "clerk", map[string]any{"seller": acme.ID, "customer": ana.ID, "dueDate": "2026-10-28"}, &inv), 201, "draft")
	for _, l := range []map[string]any{
		{"description": "Hora de consultoría", "quantity": "1", "unitPrice": "10.03", "taxCode": "G21"},
		{"description": "Hora de consultoría", "quantity": "1", "unitPrice": "10.03", "taxCode": "G21"},
		{"description": "Hora de consultoría", "quantity": "1", "unitPrice": "10.03", "taxCode": "G21"},
		{"description": "Libros", "quantity": "2", "unitPrice": "50", "discount": "10", "taxCode": "R10"},
		{"description": "Formación reglada", "quantity": "1", "unitPrice": "50", "treatment": "E1"},
	} {
		h.must(h.do("POST", "/api/billing/invoices/"+inv.ID+"/lines", "clerk", l, &inv), 200, "line")
	}
	h.must(h.do("POST", "/api/billing/invoices/"+inv.ID+"/lines", "clerk", map[string]any{"description": "X", "quantity": "1", "unitPrice": "1"}, nil), 400, "no tax code")
	var preview bapp.BreakdownDTO
	h.must(h.do("GET", "/api/billing/invoices/"+inv.ID+"/taxes?on=2026-09-28", "clerk", nil, &preview), 200, "preview")
	// 30.09 × 21 % = 6.32 (line by line 6.33); 90 × 10 % = 9.00; 50 exempt.
	if preview.Net != "170.09" || preview.Tax != "15.32" || preview.Total != "185.41" || len(preview.Lines) != 3 {
		t.Fatalf("preview: %+v", preview)
	}
	h.must(h.do("POST", "/api/billing/invoices/"+inv.ID+"/issue", "clerk", map[string]any{"series": fa.ID, "date": "2026-09-28"}, nil), 403, "preparing is not issuing")
	h.must(h.do("POST", "/api/billing/invoices/"+inv.ID+"/issue", "issuer", map[string]any{"series": rs.ID, "date": "2026-09-28"}, nil), 422, "not in a corrective series")
	h.must(h.do("POST", "/api/billing/invoices/"+inv.ID+"/issue", "issuer", map[string]any{"series": fa.ID, "date": "2026-09-28"}, &inv), 200, "issue")
	if inv.Number != "FA-2026-000001" || inv.Status != "issued" || inv.Taxes == nil || inv.Taxes.Total != "185.41" || inv.CustomerNIF != "12345678Z" ||
		inv.SellerNIF != "A58818501" {
		t.Fatalf("issued: %+v", inv)
	}
	h.must(h.do("POST", "/api/billing/invoices/"+inv.ID+"/issue", "issuer", map[string]any{"series": fa.ID, "date": "2026-09-28"}, nil), 422, "issued once")
	h.must(h.do("POST", "/api/billing/invoices/"+inv.ID+"/lines", "clerk", map[string]any{"description": "X", "quantity": "1", "unitPrice": "1", "taxCode": "G21"}, nil),
		422, "an issued invoice is immutable")
	h.must(h.do("DELETE", "/api/billing/invoices/"+inv.ID, "clerk", nil, nil), 422, "an issued invoice is never deleted")
	h.must(h.do("GET", "/api/billing/invoices/"+inv.ID, "outsider", nil, nil), 404, "outsider")

	// A retailer in the equivalence surcharge regime.
	var retail bapp.InvoiceDTO
	h.must(h.do("POST", "/api/billing/invoices", "clerk", map[string]any{"seller": acme.ID, "customer": bea.ID, "equivalenceSurcharge": true}, &retail), 201, "retail")
	h.must(h.do("POST", "/api/billing/invoices/"+retail.ID+"/lines", "clerk", map[string]any{"description": "Mercancía", "quantity": "4", "unitPrice": "25", "taxCode": "G21"}, nil), 200, "line")
	h.must(h.do("POST", "/api/billing/invoices/"+retail.ID+"/issue", "issuer", map[string]any{"series": fa.ID, "date": "2026-09-28"}, &retail), 200, "issue retail")
	if retail.Number != "FA-2026-000002" || retail.Taxes.Surcharge != "5.20" || retail.Taxes.Total != "126.20" {
		t.Fatalf("retail: %+v", retail.Taxes)
	}

	// A refused issue consumes no number (gap-free).
	var late bapp.InvoiceDTO
	h.must(h.do("POST", "/api/billing/invoices", "clerk", map[string]any{"seller": acme.ID, "customer": ana.ID}, &late), 201, "draft")
	h.must(h.do("POST", "/api/billing/invoices/"+late.ID+"/lines", "clerk", map[string]any{"description": "X", "quantity": "1", "unitPrice": "1", "taxCode": "G21"}, nil), 200, "line")
	h.must(h.do("POST", "/api/billing/invoices/"+late.ID+"/issue", "issuer", map[string]any{"series": fa.ID, "date": "2027-01-02"}, nil), 422, "series of another year")
	h.must(h.do("DELETE", "/api/billing/invoices/"+late.ID, "clerk", nil, nil), 204, "discard the draft")
	var series []bapp.SeriesDTO
	h.must(h.do("GET", "/api/billing/series?seller="+acme.ID, "clerk", nil, &series), 200, "series")
	if series[0].Code != "FA" || series[0].Last != 2 {
		t.Fatalf("no gaps: %+v", series)
	}

	// Corrective invoice (by differences) of the first one.
	var fix bapp.InvoiceDTO
	h.must(h.do("POST", "/api/billing/invoices", "clerk", map[string]any{"seller": acme.ID, "corrects": inv.ID, "reason": "r1"}, &fix), 201, "corrective draft")
	h.must(h.do("POST", "/api/billing/invoices", "clerk", map[string]any{"seller": acme.ID, "corrects": fix.ID, "reason": "R1"}, nil), 422, "a draft is not corrected")
	h.must(h.do("POST", "/api/billing/invoices/"+fix.ID+"/lines", "clerk", map[string]any{"description": "Horas no prestadas", "quantity": "-3",
		"unitPrice": "10.03", "taxCode": "G21"}, nil), 200, "negative line")
	h.must(h.do("POST", "/api/billing/invoices/"+fix.ID+"/issue", "issuer", map[string]any{"series": fa.ID, "date": "2026-09-28"}, nil), 422, "corrective series only")
	h.must(h.do("POST", "/api/billing/invoices/"+fix.ID+"/issue", "issuer", map[string]any{"series": rs.ID, "date": "2026-09-28"}, &fix), 200, "issue corrective")
	if fix.Number != "R-2026-000001" || fix.Customer != ana.ID || fix.Corrects != inv.ID || fix.Reason != "R1" || fix.Taxes.Tax != "-6.32" || fix.Taxes.Total != "-36.41" {
		t.Fatalf("corrective: %+v %+v", fix, fix.Taxes)
	}
	var issued fw.Page[bapp.InvoiceDTO]
	h.must(h.do("GET", "/api/billing/invoices?seller="+acme.ID+"&status=issued&from=2026-09-01", "clerk", nil, &issued), 200, "search")
	if issued.Total != 3 {
		t.Fatalf("issued: %d", issued.Total)
	}

	// Published Language.
	store := memory.NewStore("fiscal-books")
	books := messaging.NewConsumer("books", memory.NewInbox(store), store)
	var got []contracts.InvoiceIssuedV1
	messaging.Handle(books, func(_ context.Context, e contracts.InvoiceIssuedV1, _ application.Envelope) error {
		got = append(got, e)
		return nil
	})
	broker := inprocess.NewBroker()
	broker.Subscribe("books", books)
	for {
		n, err := h.billing.Relay(broker).RelayOnce(context.Background())
		h.ok(err)
		if n == 0 {
			break
		}
	}
	if len(got) != 3 || got[0].Number != "FA-2026-000001" || len(got[0].Taxes) != 3 || got[0].Taxes[0].Amount != "6.32" || got[2].Kind != "corrective" ||
		got[2].Corrects != inv.ID || got[1].Surcharge != "5.20" || got[0].Country != "ES" {
		t.Fatalf("published: %+v", got)
	}
}

func TestBilling_WithFiscalAndParties_MemoryThenSQLite(t *testing.T) {
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
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{pinfra.Migrations(), finfra.Migrations(), binfra.Migrations()})
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

func TestBilling_LayersRespectTheArchitecture(t *testing.T) {
	const m = "github.com/jhermoso/karpo-fw-go"
	archtest.AssertOnlyImports(t, "domain", false, archtest.Std, m+"/pkg/domain/...", m+"/contexts/billing/domain")
	archtest.AssertOnlyImports(t, "contracts", false, archtest.Std)
	archtest.AssertTreeDoesNotImport(t, "application", []string{"/pkg/persistence", "/pkg/distribution", "database/sql", "net/http"})
}
