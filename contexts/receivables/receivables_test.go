package receivables_test

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
	bdomain "github.com/jhermoso/karpo-fw-go/contexts/billing/domain"
	binfra "github.com/jhermoso/karpo-fw-go/contexts/billing/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/fiscal"
	fapp "github.com/jhermoso/karpo-fw-go/contexts/fiscal/application"
	finfra "github.com/jhermoso/karpo-fw-go/contexts/fiscal/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/parties"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	pinfra "github.com/jhermoso/karpo-fw-go/contexts/parties/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/receivables"
	rapp "github.com/jhermoso/karpo-fw-go/contexts/receivables/application"
	"github.com/jhermoso/karpo-fw-go/contexts/receivables/contracts"
	rinfra "github.com/jhermoso/karpo-fw-go/contexts/receivables/infrastructure"
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

// host composes Parties, Fiscal, Billing and Receivables on one hot-swappable backend.
type host struct {
	t        *testing.T
	srv      *httptest.Server
	sw       *hotswap.Switch
	dir      *authorization.MemoryDirectory
	ids      map[string]fw.UUID
	tokens   map[string]string
	rec      *receivables.Module
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
	rm := receivables.Compose(sw, nil)

	jwt, _ := jwtauth.New(jwtauth.Config{Secret: []byte("receivables-test")})
	dir := authorization.NewMemoryDirectory()
	h := &host{t: t, sw: sw, dir: dir, ids: map[string]fw.UUID{}, tokens: map[string]string{}, rec: rm, billing: bm, fiscal: fm, parties: pm}
	perms := []authz.Permission{rapp.PermTermsRead, rapp.PermTermsUpdate, rapp.PermCreditRead, rapp.PermCreditUpdate, rapp.PermReceivableRead,
		rapp.PermCollectionRead, rapp.PermCollectionWrite}
	for _, u := range []string{"clerk", "outsider"} {
		h.ids[u] = fw.NewUUID()
		dir.Put(h.ids[u], authz.Subject{Active: true, Permissions: perms})
		tok, _ := jwt.Issue(jwtauth.Claims{Subject: h.ids[u].String(), Username: u, PartyID: fw.NewUUID().String(), ExpiresAt: fw.Now().Add(time.Hour).Unix()})
		h.tokens[u] = "Bearer " + tok
	}
	ac, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "setup", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
	ac.GlobalAdmin = true
	h.adminCtx = authz.WithContext(ctx, ac)

	mux := http.NewServeMux()
	rm.RegisterRoutes(mux)
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

func relay(t *testing.T, r interface {
	RelayOnce(context.Context) (int, error)
}) {
	t.Helper()
	for {
		n, err := r.RelayOnce(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			return
		}
	}
}

// invoice drafts and issues an invoice of one G21 line.
func (h *host) invoice(seller, customer, series, amount string, qty string, due vocab.Date, corrects string) bapp.InvoiceDTO {
	svc := h.billing.Service
	d := bapp.DraftInvoice{Seller: seller, Customer: customer, DetailsInput: bapp.DetailsInput{DueDate: due}}
	if corrects != "" {
		d = bapp.DraftInvoice{Seller: seller, Corrects: corrects, Reason: "R1"}
	}
	inv, err := svc.DraftInvoice.Handle(h.adminCtx, d)
	h.ok(err)
	id, _ := bdomain.ParseInvoiceID(inv.ID)
	_, err = svc.AddLine.Handle(h.adminCtx, bapp.AddLine{ID: id, Description: "Servicio", Quantity: qty, UnitPrice: amount, TaxCode: "G21"})
	h.ok(err)
	inv, err = svc.Issue.Handle(h.adminCtx, bapp.IssueInvoice{ID: id, Series: series, Date: vocab.MustDate(2026, 9, 28)})
	h.ok(err)
	return inv
}

func (h *host) scenario(tag string) {
	t := h.t
	ctx := h.adminCtx
	ps := h.parties.Service
	org := func(name string) papp.PartyDTO {
		o, err := ps.RegisterOrganization.Handle(ctx, papp.RegisterOrganization{LegalName: name + " " + tag, Roles: []string{pdomain.RoleInternalOrganization.String()}})
		h.ok(err)
		return o
	}
	acme, globex := org("Acme"), org("Globex")
	h.grant("clerk", acme.ID)
	h.grant("outsider", globex.ID)
	doc := func(party, docType, number string) {
		id, _ := pdomain.ParsePartyID(party)
		_, err := ps.AddIdentification.Handle(ctx, papp.AddIdentification{PartyID: id, DocumentType: docType, Country: "ES", Number: number, Primary: true})
		h.ok(err)
	}
	doc(acme.ID, "c0000000-0004-0000-0000-000000000003", "A58818501")
	ana, err := ps.RegisterPerson.Handle(ctx, papp.RegisterPerson{GivenName: "Ana", FirstSurname: tag})
	h.ok(err)
	doc(ana.ID, "c0000000-0004-0000-0000-000000000002", "12345678Z")
	bea, err := ps.RegisterPerson.Handle(ctx, papp.RegisterPerson{GivenName: "Bea", FirstSurname: tag})
	h.ok(err)
	_, err = h.fiscal.Service.CreateRate.Handle(ctx, fapp.CreateRate{Type: "vat", Territory: "common", Code: "G21", Description: "General", Rate: "21",
		From: vocab.MustDate(2012, 9, 1)})
	h.ok(err)
	_, err = h.fiscal.Service.RegisterTaxpayer.Handle(ctx, fapp.RegisterTaxpayer{Organization: acme.ID, TermsInput: fapp.TermsInput{Territory: "common", FiscalYearStartMonth: 1}})
	h.ok(err)
	fa, err := h.billing.Service.OpenSeries.Handle(ctx, bapp.OpenSeries{Seller: acme.ID, Code: "FA", Year: 2026})
	h.ok(err)
	rs, err := h.billing.Service.OpenSeries.Handle(ctx, bapp.OpenSeries{Seller: acme.ID, Code: "R", Year: 2026, Corrective: true})
	h.ok(err)

	// Payment terms: two installments at 30 and 60 days, paid on the 10th.
	var terms rapp.TermsDTO
	h.must(h.do("POST", "/api/receivables/terms", "clerk", map[string]any{"seller": acme.ID, "code": "30-60", "description": "30 y 60 días, día 10",
		"installments": 2, "daysToFirst": 30, "daysBetween": 30, "fixedDays": []int{10}}, &terms), 201, "terms")
	h.must(h.do("POST", "/api/receivables/terms", "clerk", map[string]any{"seller": acme.ID, "code": "X", "description": "X", "installments": 2,
		"daysToFirst": 30}, nil), 400, "several installments need days between them")
	h.must(h.do("POST", "/api/receivables/terms", "outsider", map[string]any{"seller": acme.ID, "code": "Y", "description": "Y", "installments": 1}, nil),
		404, "outsider")
	var dues []rapp.DueDTO
	h.must(h.do("GET", "/api/receivables/terms/"+terms.ID+"/schedule?issued=2026-09-28&amount=1000", "clerk", nil, &dues), 200, "preview")
	if len(dues) != 2 || dues[0].Date != "2026-11-10" || dues[1].Date != "2026-12-10" || dues[1].Amount != "500.00" {
		t.Fatalf("schedule: %+v", dues)
	}
	limit := "200"
	h.must(h.do("PUT", "/api/receivables/credit", "clerk", map[string]any{"seller": acme.ID, "customer": ana.ID, "terms": terms.ID, "limit": limit}, nil), 200, "credit")

	// Billing issues; Receivables opens the receivables from the Published Language.
	inv1 := h.invoice(acme.ID, ana.ID, fa.ID, "100", "1", vocab.Date{}, "")                // 121.00 by the customer's terms
	inv2 := h.invoice(acme.ID, ana.ID, fa.ID, "50", "1", vocab.MustDate(2026, 10, 15), "") // 60.50 on its due date
	credit := h.invoice(acme.ID, "", rs.ID, "30", "-1", vocab.Date{}, inv1.ID)             // -36.30 at once
	broker := inprocess.NewBroker()
	broker.Subscribe("receivables", h.rec.Consumer)
	relay(t, h.billing.Relay(broker))
	var r1 rapp.ReceivableDTO
	h.must(h.do("GET", "/api/receivables/invoices/"+inv1.ID, "clerk", nil, &r1), 200, "receivable")
	if len(r1.Installments) != 2 || r1.Installments[0].Due != "2026-11-10" || r1.Installments[0].Amount != "60.50" || r1.Total != "121.00" {
		t.Fatalf("receivable 1: %+v", r1)
	}
	h.must(h.do("GET", "/api/receivables/invoices/"+inv1.ID, "outsider", nil, nil), 404, "outsider")
	var exp contracts.Exposure
	h.must(h.do("GET", "/api/receivables/credit/exposure?seller="+acme.ID+"&customer="+ana.ID+"&on=2026-10-20", "clerk", nil, &exp), 200, "exposure")
	// 121.00 + 60.50 − 36.30 = 145.20 open; overdue: 60.50 (due 15 Oct) − 36.30 (the credit) = 24.20.
	if exp.Open != "145.20" || exp.Overdue != "24.20" || !exp.Limited || exp.Available != "54.80" || exp.Blocked {
		t.Fatalf("exposure: %+v", exp)
	}

	// A collection allocated to both invoices: capped on both sides.
	var col rapp.CollectionDTO
	h.must(h.do("POST", "/api/receivables/collections", "clerk", map[string]any{"seller": acme.ID, "payer": ana.ID, "date": "2026-10-16",
		"amount": "100", "method": "transfer", "reference": "TRF-1", "allocations": []map[string]any{
			{"invoice": inv2.ID, "installment": 1, "amount": "60.50"}, {"invoice": inv1.ID, "installment": 1, "amount": "39.50"}}}, &col), 201, "collection")
	if col.Unallocated != "0.00" || len(col.Allocations) != 2 {
		t.Fatalf("collection: %+v", col)
	}
	h.must(h.do("POST", "/api/receivables/collections/"+col.ID+"/allocations", "clerk", map[string]any{"invoice": inv1.ID, "installment": 2, "amount": "1"}, nil),
		422, "the collection is fully allocated")
	h.must(h.do("POST", "/api/receivables/collections", "clerk", map[string]any{"seller": acme.ID, "payer": ana.ID, "date": "2026-10-16", "amount": "500",
		"method": "cash", "allocations": []map[string]any{{"invoice": inv2.ID, "installment": 1, "amount": "1"}}}, nil), 422, "invoice 2 is settled")
	h.must(h.do("POST", "/api/receivables/collections", "clerk", map[string]any{"seller": acme.ID, "payer": bea.ID, "date": "2026-10-16", "amount": "10",
		"method": "cash", "allocations": []map[string]any{{"invoice": inv1.ID, "installment": 2, "amount": "10"}}}, nil), 422, "another customer's invoice")
	var r2 rapp.ReceivableDTO
	h.must(h.do("GET", "/api/receivables/invoices/"+inv2.ID, "clerk", nil, &r2), 200, "receivable 2")
	if !r2.Settled {
		t.Fatalf("receivable 2 settled: %+v", r2)
	}

	// The credit nets against the second installment of invoice 1.
	h.must(h.do("POST", "/api/receivables/offsets", "clerk", map[string]any{"seller": acme.ID, "credit": credit.ID, "invoice": inv1.ID, "installment": 1,
		"amount": "36.30", "date": "2026-10-20"}, nil), 422, "only 21.00 open in the first installment")
	var off rapp.CollectionDTO
	h.must(h.do("POST", "/api/receivables/offsets", "clerk", map[string]any{"seller": acme.ID, "credit": credit.ID, "invoice": inv1.ID, "installment": 2,
		"amount": "36.30", "date": "2026-10-20"}, &off), 201, "offset")
	h.must(h.do("GET", "/api/receivables/credit/exposure?seller="+acme.ID+"&customer="+ana.ID+"&on=2026-10-20", "clerk", nil, &exp), 200, "exposure")
	if exp.Open != "45.20" || exp.Overdue != "0.00" {
		t.Fatalf("exposure after collection and offset: %+v", exp)
	}

	// Reversals: an allocation, then the whole collection.
	h.must(h.do("POST", "/api/receivables/collections/"+col.ID+"/allocations/reverse", "clerk", map[string]any{"allocation": col.Allocations[1].ID}, &col),
		200, "reverse")
	h.must(h.do("POST", "/api/receivables/collections/"+col.ID+"/cancel", "clerk", nil, &col), 200, "cancel")
	if !col.Cancelled || col.Allocated != "0.00" {
		t.Fatalf("cancelled: %+v", col)
	}
	h.must(h.do("GET", "/api/receivables/credit/exposure?seller="+acme.ID+"&customer="+ana.ID+"&on=2026-10-20", "clerk", nil, &exp), 200, "exposure")
	if exp.Open != "145.20" {
		t.Fatalf("exposure after the cancellation: %+v", exp)
	}
	var open fw.Page[rapp.ReceivableDTO]
	h.must(h.do("GET", "/api/receivables/invoices?customer="+ana.ID+"&open=true", "clerk", nil, &open), 200, "open receivables")
	if open.Total != 2 {
		t.Fatalf("open receivables: %d", open.Total)
	}

	// Published Language.
	store := memory.NewStore("accounting")
	acc := messaging.NewConsumer("accounting", memory.NewInbox(store), store)
	allocated, reversed, settled := 0, 0, 0
	messaging.Handle(acc, func(_ context.Context, e contracts.CollectionAllocatedV1, _ application.Envelope) error {
		allocated++
		return nil
	})
	messaging.Handle(acc, func(_ context.Context, e contracts.AllocationReversedV1, _ application.Envelope) error {
		reversed++
		return nil
	})
	messaging.Handle(acc, func(_ context.Context, e contracts.ReceivableSettledV1, _ application.Envelope) error {
		settled++
		return nil
	})
	out := inprocess.NewBroker()
	out.Subscribe("accounting", acc)
	relay(t, h.rec.Relay(out))
	// 2 allocations + 2 of the offset; 2 reversals; invoice 2 and the credit settled.
	if allocated != 4 || reversed != 2 || settled != 2 {
		t.Fatalf("published: %d allocated, %d reversed, %d settled", allocated, reversed, settled)
	}
}

func TestReceivables_FedByBilling_MemoryThenSQLite(t *testing.T) {
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
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{pinfra.Migrations(), finfra.Migrations(), binfra.Migrations(), rinfra.Migrations()})
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

func TestReceivables_LayersRespectTheArchitecture(t *testing.T) {
	const m = "github.com/jhermoso/karpo-fw-go"
	archtest.AssertOnlyImports(t, "domain", false, archtest.Std, m+"/pkg/domain/...", m+"/contexts/receivables/domain")
	archtest.AssertOnlyImports(t, "contracts", false, archtest.Std)
	archtest.AssertTreeDoesNotImport(t, "application", []string{"/pkg/persistence", "/pkg/distribution", "database/sql", "net/http"})
}
