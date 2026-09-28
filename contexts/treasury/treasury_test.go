package treasury_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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
	rdomain "github.com/jhermoso/karpo-fw-go/contexts/receivables/domain"
	rinfra "github.com/jhermoso/karpo-fw-go/contexts/receivables/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/treasury"
	tapp "github.com/jhermoso/karpo-fw-go/contexts/treasury/application"
	tinfra "github.com/jhermoso/karpo-fw-go/contexts/treasury/infrastructure"
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

// host composes Parties, Fiscal, Billing, Receivables and Treasury on one hot-swappable backend,
// with one in-process broker carrying the Published Language between them.
type host struct {
	t        *testing.T
	srv      *httptest.Server
	sw       *hotswap.Switch
	dir      *authorization.MemoryDirectory
	ids      map[string]fw.UUID
	tokens   map[string]string
	treasury *treasury.Module
	rec      *receivables.Module
	billing  *billing.Module
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
	bm := billing.Compose(sw, binfra.FiscalTaxes{Engine: fm.TaxEngine}, binfra.PartiesIdentities{TaxIdentities: pm.TaxIdentities})
	rm := receivables.Compose(sw, nil)
	tm := treasury.Compose(sw, tinfra.ReceivablesDueItems{Collectable: rm.Collectable}, tinfra.PartiesIdentities{TaxIdentities: pm.TaxIdentities})
	broker := inprocess.NewBroker()
	broker.Subscribe("receivables", rm.Consumer)

	jwt, _ := jwtauth.New(jwtauth.Config{Secret: []byte("treasury-test")})
	dir := authorization.NewMemoryDirectory()
	h := &host{t: t, sw: sw, dir: dir, ids: map[string]fw.UUID{}, tokens: map[string]string{}, treasury: tm, rec: rm, billing: bm, fiscal: fm,
		parties: pm, broker: broker}
	prepare := []authz.Permission{tapp.PermAccountRead, tapp.PermAccountUpdate, tapp.PermMandateRead, tapp.PermMandateUpdate, tapp.PermRemittanceRead,
		tapp.PermRemittanceEdit}
	users := map[string][]authz.Permission{"clerk": prepare, "sender": {tapp.PermRemittanceRead, tapp.PermRemittanceSend},
		"banker": {tapp.PermRemittanceRead, tapp.PermRemittanceBank}, "outsider": prepare}
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
	tm.RegisterRoutes(mux)
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

func (h *host) request(method, path, user string, body any) *http.Response {
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
	return res
}

func (h *host) do(method, path, user string, body, out any) int {
	h.t.Helper()
	res := h.request(method, path, user, body)
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

// deliver relays the Published Language of Billing and Treasury to Receivables.
func (h *host) deliver() {
	for _, r := range []interface {
		RelayOnce(context.Context) (int, error)
	}{h.billing.Relay(h.broker), h.treasury.Relay(h.broker)} {
		for {
			n, err := r.RelayOnce(context.Background())
			h.ok(err)
			if n == 0 {
				break
			}
		}
	}
}

func (h *host) receivable(invoice string) rapp.ReceivableDTO {
	id, _ := rdomain.ParseReceivableID(invoice)
	r, err := h.rec.Service.GetReceivable.Handle(h.adminCtx, rapp.GetReceivable{ID: id})
	h.ok(err)
	return r
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
	for _, u := range []string{"clerk", "sender", "banker"} {
		h.grant(u, acme.ID)
	}
	h.grant("outsider", globex.ID)
	doc := func(party, docType, number string) {
		id, _ := pdomain.ParsePartyID(party)
		_, err := ps.AddIdentification.Handle(ctx, papp.AddIdentification{PartyID: id, DocumentType: docType, Country: "ES", Number: number, Primary: true})
		h.ok(err)
	}
	doc(acme.ID, "c0000000-0004-0000-0000-000000000003", "A58818501")
	person := func(name, dni string) papp.PartyDTO {
		p, err := ps.RegisterPerson.Handle(ctx, papp.RegisterPerson{GivenName: name, FirstSurname: "Núñez " + tag})
		h.ok(err)
		doc(p.ID, "c0000000-0004-0000-0000-000000000002", dni)
		return p
	}
	ana, bea, carl := person("Ana", "12345678Z"), person("Bea", "00000000T"), person("Carl", "00000001R")
	_, err := h.fiscal.Service.CreateRate.Handle(ctx, fapp.CreateRate{Type: "vat", Territory: "common", Code: "G21", Description: "General", Rate: "21",
		From: vocab.MustDate(2012, 9, 1)})
	h.ok(err)
	_, err = h.fiscal.Service.RegisterTaxpayer.Handle(ctx, fapp.RegisterTaxpayer{Organization: acme.ID, TermsInput: fapp.TermsInput{Territory: "common", FiscalYearStartMonth: 1}})
	h.ok(err)
	fa, err := h.billing.Service.OpenSeries.Handle(ctx, bapp.OpenSeries{Seller: acme.ID, Code: "FA", Year: 2026})
	h.ok(err)
	invoice := func(customer, price string, due vocab.Date) bapp.InvoiceDTO {
		inv, err := h.billing.Service.DraftInvoice.Handle(ctx, bapp.DraftInvoice{Seller: acme.ID, Customer: customer, DetailsInput: bapp.DetailsInput{DueDate: due}})
		h.ok(err)
		id, _ := bdomain.ParseInvoiceID(inv.ID)
		_, err = h.billing.Service.AddLine.Handle(ctx, bapp.AddLine{ID: id, Description: "Cuota", Quantity: "1", UnitPrice: price, TaxCode: "G21"})
		h.ok(err)
		inv, err = h.billing.Service.Issue.Handle(ctx, bapp.IssueInvoice{ID: id, Series: fa.ID, Date: vocab.MustDate(2026, 9, 28)})
		h.ok(err)
		return inv
	}
	invAna := invoice(ana.ID, "100", vocab.MustDate(2026, 10, 15))  // 121.00
	invBea := invoice(bea.ID, "50", vocab.MustDate(2026, 10, 25))   // 60.50
	invCarl := invoice(carl.ID, "10", vocab.MustDate(2026, 10, 10)) // 12.10, no mandate
	h.deliver()

	// Account and mandates.
	var acc tapp.AccountDTO
	account := map[string]any{"owner": acme.ID, "iban": "ES91 2100 0418 4502 0005 1332", "bic": "CAIXESBBXXX", "alias": "Cobros " + tag,
		"collections": true, "opened": "2020-01-01"}
	h.must(h.do("POST", "/api/treasury/accounts", "clerk", account, &acc), 201, "account")
	h.must(h.do("POST", "/api/treasury/accounts", "clerk", account, nil), 422, "the account is registered once")
	h.must(h.do("POST", "/api/treasury/accounts", "clerk", map[string]any{"owner": acme.ID, "iban": "ES9121000418450200051333", "alias": "X",
		"opened": "2020-01-01"}, nil), 400, "IBAN control digits")
	h.must(h.do("POST", "/api/treasury/accounts", "outsider", map[string]any{"owner": acme.ID, "iban": "ES6621000418401234567891", "alias": "X",
		"opened": "2020-01-01"}, nil), 404, "outsider")
	mandate := func(debtor, ref, iban, signed string) {
		h.must(h.do("POST", "/api/treasury/mandates", "clerk", map[string]any{"creditor": acme.ID, "debtor": debtor, "iban": iban, "reference": ref,
			"scheme": "CORE", "signed": signed}, nil), 201, "mandate "+ref)
	}
	mandate(ana.ID, "MAND-ANA", "ES7921000813610123456789", "2025-01-10")
	mandate(bea.ID, "MAND-BEA", "ES6621000418401234567891", "2026-01-10")
	h.must(h.do("POST", "/api/treasury/mandates", "clerk", map[string]any{"creditor": acme.ID, "debtor": bea.ID, "iban": "ES6621000418401234567891",
		"reference": "MAND-BEA", "scheme": "CORE", "signed": "2026-01-10"}, nil), 422, "duplicate reference")

	// A remittance with what is due by 15 October: Ana (Carl has no mandate).
	var r1 tapp.RemittanceDTO
	h.must(h.do("POST", "/api/treasury/remittances", "clerk", map[string]any{"creditor": acme.ID, "account": acc.ID, "scheme": "CORE",
		"collectionDate": "2026-10-20", "dueTo": "2026-10-15"}, &r1), 201, "propose")
	if len(r1.Items) != 1 || r1.Items[0].Invoice != invAna.ID || r1.Items[0].Amount != "121.00" || r1.WithoutMandate != 1 || r1.Total != "121.00" {
		t.Fatalf("proposal: %+v", r1)
	}
	h.must(h.do("POST", "/api/treasury/remittances/"+r1.ID+"/generate", "clerk", nil, nil), 403, "preparing is not sending")
	h.must(h.do("POST", "/api/treasury/remittances/"+r1.ID+"/generate", "sender", nil, &r1), 200, "generate")
	if r1.Status != "generated" || r1.CreditorID != "ES30000A58818501" || r1.Items[0].Sequence != "FRST" {
		t.Fatalf("generated: %+v", r1)
	}
	res := h.request("GET", "/api/treasury/remittances/"+r1.ID+"/pain008", "sender", nil)
	file, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !strings.Contains(res.Header.Get("Content-Type"), "xml") || !strings.Contains(string(file), "<MndtId>MAND-ANA</MndtId>") ||
		!strings.Contains(string(file), "<SeqTp>FRST</SeqTp>") || !strings.Contains(string(file), "<Nm>Ana Nunez "+tag) {
		t.Fatalf("pain.008 (%d):\n%s", res.StatusCode, file)
	}
	h.must(h.do("GET", "/api/treasury/remittances/"+r1.ID, "outsider", nil, nil), 404, "outsider")

	// A second proposal does not repeat Ana: it takes Bea.
	var r2 tapp.RemittanceDTO
	h.must(h.do("POST", "/api/treasury/remittances", "clerk", map[string]any{"creditor": acme.ID, "account": acc.ID, "scheme": "CORE",
		"collectionDate": "2026-10-30", "dueTo": "2026-10-31"}, &r2), 201, "second proposal")
	if len(r2.Items) != 1 || r2.Items[0].Invoice != invBea.ID || r2.WithoutMandate != 1 {
		t.Fatalf("second proposal: %+v", r2)
	}

	// The bank charges: Receivables registers the collection; a return cancels it.
	h.must(h.do("POST", "/api/treasury/remittances/"+r1.ID+"/settle", "sender", map[string]any{"on": "2026-10-20"}, nil), 403, "sending is not settling")
	h.must(h.do("POST", "/api/treasury/remittances/"+r1.ID+"/settle", "banker", map[string]any{"on": "2026-10-20"}, nil), 200, "settle")
	h.deliver()
	if r := h.receivable(invAna.ID); !r.Settled {
		t.Fatalf("collected by direct debit: %+v", r)
	}
	h.must(h.do("POST", "/api/treasury/remittances/"+r1.ID+"/returns", "banker", map[string]any{"endToEnd": r1.Items[0].EndToEnd,
		"on": "2026-10-27", "reason": "AM04"}, nil), 200, "return")
	h.deliver()
	if r := h.receivable(invAna.ID); r.Settled || r.Open != "121.00" {
		t.Fatalf("returned: %+v", r)
	}
	if r := h.receivable(invCarl.ID); r.Open != "12.10" {
		t.Fatalf("carl untouched: %+v", r)
	}

	// Ana goes again, now as a recurrent collection.
	var r3 tapp.RemittanceDTO
	h.must(h.do("POST", "/api/treasury/remittances", "clerk", map[string]any{"creditor": acme.ID, "account": acc.ID, "scheme": "CORE",
		"collectionDate": "2026-11-05", "dueTo": "2026-10-15"}, &r3), 201, "third proposal")
	h.must(h.do("POST", "/api/treasury/remittances/"+r3.ID+"/generate", "sender", nil, &r3), 200, "generate again")
	if len(r3.Items) != 1 || r3.Items[0].Invoice != invAna.ID || r3.Items[0].Sequence != "RCUR" {
		t.Fatalf("recurrent: %+v", r3)
	}
	var mandates []tapp.MandateDTO
	h.must(h.do("GET", "/api/treasury/mandates?creditor="+acme.ID+"&debtor="+ana.ID, "clerk", nil, &mandates), 200, "mandates")
	if len(mandates) != 1 || mandates[0].LastUsed != "2026-11-05" || mandates[0].Next != "RCUR" {
		t.Fatalf("mandate use: %+v", mandates)
	}
}

func TestTreasury_CollectsWhatReceivablesHasOpen_MemoryThenSQLite(t *testing.T) {
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
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{pinfra.Migrations(), finfra.Migrations(), binfra.Migrations(), rinfra.Migrations(), tinfra.Migrations()})
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

func TestTreasury_LayersRespectTheArchitecture(t *testing.T) {
	const m = "github.com/jhermoso/karpo-fw-go"
	archtest.AssertOnlyImports(t, "domain", false, archtest.Std, m+"/pkg/domain/...", m+"/contexts/treasury/domain")
	archtest.AssertOnlyImports(t, "contracts", false, archtest.Std)
	archtest.AssertTreeDoesNotImport(t, "application", []string{"/pkg/persistence", "/pkg/distribution", "database/sql", "net/http"})
}
