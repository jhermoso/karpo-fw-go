package documents_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jhermoso/karpo-fw-go/contexts/documents"
	dapp "github.com/jhermoso/karpo-fw-go/contexts/documents/application"
	"github.com/jhermoso/karpo-fw-go/contexts/documents/domain"
	dinfra "github.com/jhermoso/karpo-fw-go/contexts/documents/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
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

func TestDocument_Rules(t *testing.T) {
	company := domain.OrganizationID{UUID: fw.NewUUID()}
	ok := func() domain.DocumentState {
		return domain.DocumentState{Company: company, Fact: domain.Ref{Type: domain.Invoice, ID: "i-1"}, Number: " A-2026-000001 ",
			Date: vocab.MustDate(2026, 5, 8), Origin: domain.Ref{Type: domain.DeliveryNote, ID: "d-1"}, Relation: domain.OriginatesFrom}
	}
	bad := []func(*domain.DocumentState){
		func(s *domain.DocumentState) { s.Company = domain.OrganizationID{} },
		func(s *domain.DocumentState) { s.Fact.Type = "letter" },
		func(s *domain.DocumentState) { s.Fact.ID = "" },
		func(s *domain.DocumentState) { s.Number = " " },
		func(s *domain.DocumentState) { s.Date = vocab.Date{} },
		func(s *domain.DocumentState) { s.Relation = "" },
		func(s *domain.DocumentState) { s.Origin = domain.Ref{} },
		func(s *domain.DocumentState) { s.Relation = "likes" },
		func(s *domain.DocumentState) { s.Origin = s.Fact },
	}
	for i, f := range bad {
		s := ok()
		f(&s)
		if _, err := domain.RecordDocument(domain.NewDocumentID(), s); !errors.Is(err, fw.ErrValidation) {
			t.Fatalf("case %d: %v", i, err)
		}
	}
	s := ok()
	s.Cancelled, s.CancelReason = true, "x"
	d, err := domain.RecordDocument(domain.NewDocumentID(), s)
	if err != nil || d.State().Number != "A-2026-000001" || d.State().Cancelled || d.State().HasTotal {
		t.Fatalf("recorded: %+v %v", d, err)
	}
	if !d.Cancel(" duplicada ") || d.Cancel("otra vez") || d.State().CancelReason != "duplicada" {
		t.Fatalf("cancelled once: %+v", d.State())
	}
}

// host composes Documents on a hot-swappable backend; the test plays the issuing contexts by
// sending their facts to the broker.
type host struct {
	t      *testing.T
	srv    *httptest.Server
	sw     *hotswap.Switch
	dir    *authorization.MemoryDirectory
	ids    map[string]fw.UUID
	tokens map[string]string
	docs   *documents.Module
	broker *inprocess.Broker
}

func compose(t *testing.T) *host {
	ctx := context.Background()
	sw := hotswap.New(memory.NewStore("memory"))
	dm := documents.Compose(sw)
	broker := inprocess.NewBroker()
	broker.Subscribe("documents", dm.Consumer)

	jwt, _ := jwtauth.New(jwtauth.Config{Secret: []byte("documents-test")})
	dir := authorization.NewMemoryDirectory()
	h := &host{t: t, sw: sw, dir: dir, ids: map[string]fw.UUID{}, tokens: map[string]string{}, docs: dm, broker: broker}
	users := map[string][]authz.Permission{"viewer": dapp.Permissions(), "stranger": nil, "outsider": dapp.Permissions()}
	for u, perms := range users {
		h.ids[u] = fw.NewUUID()
		dir.Put(h.ids[u], authz.Subject{Active: true, Permissions: perms})
		tok, _ := jwt.Issue(jwtauth.Claims{Subject: h.ids[u].String(), Username: u, PartyID: fw.NewUUID().String(), ExpiresAt: fw.Now().Add(time.Hour).Unix()})
		h.tokens[u] = "Bearer " + tok
	}
	mux := http.NewServeMux()
	dm.RegisterRoutes(mux)
	h.srv = httptest.NewServer(distribution.Chain(mux, distribution.Authorize(jwt, authorization.NewResolver(dir, authorization.Options{}))))
	t.Cleanup(h.srv.Close)
	t.Cleanup(func() { _ = sw.Close(ctx) })
	return h
}

func (h *host) grant(user, org string) {
	s, _, _ := h.dir.Subject(context.Background(), h.ids[user])
	s.Grants = append(s.Grants, authz.Grant{OrganizationID: fw.MustParseUUID(org), Level: authz.Full})
	h.dir.Put(h.ids[user], s)
}

func (h *host) get(path, user string, out any) int {
	h.t.Helper()
	req, _ := http.NewRequest("GET", h.srv.URL+path, nil)
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

// send plays an issuing context: one of its facts reaches the broker.
func (h *host) send(id, typ string, at time.Time, data map[string]any) error {
	raw, _ := json.Marshal(data)
	return h.broker.Send(context.Background(), application.Envelope{ID: id, Type: typ, Source: "test", OccurredAt: at, Data: raw})
}

func (h *host) scenario(tag string) {
	t := h.t
	id := func() string { return fw.NewUUID().String() }
	acme, globex, customer, supplier, employee := id(), id(), id(), id(), id()
	order, delivery, invoice, credit, received, payslip, filing := id(), id(), id(), id(), id(), id(), id()
	h.grant("viewer", acme)
	h.grant("stranger", acme)
	h.grant("outsider", globex)
	day := func(m time.Month, d int) time.Time { return time.Date(2026, m, d, 10, 0, 0, 0, time.UTC) }
	ok := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	// The sale: order, delivery note, invoice from the delivery note, corrective invoice.
	ok(h.send(tag+"-1", "orders.order-confirmed.v1", day(5, 4), map[string]any{"orderId": order, "company": acme, "customer": customer,
		"number": "PED-2026-000001", "total": "242.00"}))
	ok(h.send(tag+"-2", "orders.delivery-issued.v1", day(5, 6), map[string]any{"deliveryId": delivery, "orderId": order, "company": acme,
		"customer": customer, "number": "ALB-2026-000001", "date": "2026-05-06", "total": "242.00"}))
	issued := map[string]any{"invoiceId": invoice, "number": "A-2026-000001", "kind": "ordinary", "sourceType": "orders.delivery", "sourceId": delivery,
		"seller": acme, "customer": customer, "issueDate": "2026-05-08", "total": "242.00"}
	ok(h.send(tag+"-3", "billing.invoice-issued.v1", day(5, 8), issued))
	ok(h.send(tag+"-3", "billing.invoice-issued.v1", day(5, 8), issued))  // the same message again
	ok(h.send(tag+"-3b", "billing.invoice-issued.v1", day(5, 8), issued)) // the same fact in another message
	ok(h.send(tag+"-4", "billing.invoice-issued.v1", day(5, 20), map[string]any{"invoiceId": credit, "number": "R-2026-000001", "kind": "corrective",
		"corrects": invoice, "seller": acme, "customer": customer, "issueDate": "2026-05-20", "total": "-24.20"}))
	// A purchase booked by mistake, a payslip, and a tax filing reverted.
	ok(h.send(tag+"-5", "purchases.invoice-registered.v1", day(5, 10), map[string]any{"invoiceId": received, "company": acme, "supplier": supplier,
		"supplierNumber": "2026/0042", "register": "FR-2026-000001", "received": "2026-05-10", "total": "1210.00"}))
	ok(h.send(tag+"-6", "purchases.invoice-cancelled.v1", day(5, 11), map[string]any{"invoiceId": received, "reason": "duplicada"}))
	ok(h.send(tag+"-6b", "purchases.invoice-cancelled.v1", day(5, 11), map[string]any{"invoiceId": received, "reason": "otra vez"}))
	ok(h.send(tag+"-7", "payroll.payslip-approved.v1", day(5, 28), map[string]any{"payslipId": payslip, "person": employee, "employer": acme,
		"kind": "ordinary", "periodStart": "2026-05-01", "paymentDate": "2026-05-29", "net": "1500.00"}))
	ok(h.send(tag+"-8", "fiscal.filing-submitted.v1", day(7, 15), map[string]any{"filingId": filing, "declarant": acme, "form": "111", "year": 2026,
		"period": "2T", "withheld": "300.00"}))
	ok(h.send(tag+"-9", "fiscal.filing-reverted.v1", day(7, 16), map[string]any{"filingId": filing, "reason": "error en las bases"}))
	// A cancellation of something the register never heard of changes nothing; a broken fact is refused.
	ok(h.send(tag+"-10", "payroll.payslip-cancelled.v1", day(7, 16), map[string]any{"payslipId": id(), "reason": "x"}))
	if err := h.send(tag+"-11", "orders.delivery-issued.v1", day(5, 6), map[string]any{"deliveryId": id(), "orderId": order, "company": acme,
		"customer": customer, "number": "ALB-2026-000002", "date": "6 de mayo", "total": "1.00"}); err == nil {
		t.Fatal("a fact without a date is refused")
	}
	// Another company's order.
	ok(h.send(tag+"-12", "orders.order-confirmed.v1", day(5, 4), map[string]any{"orderId": id(), "company": globex, "customer": customer,
		"number": "PED-2026-000001", "total": "10.00"}))

	var page fw.Page[dapp.DocumentDTO]
	h.must(h.get("/api/documents?company="+acme, "stranger", nil), 403, "no permission")
	h.must(h.get("/api/documents?company="+acme+"&type=letter", "viewer", nil), 400, "type")
	h.must(h.get("/api/documents?company="+acme, "viewer", &page), 200, "register")
	var types []string
	for _, d := range page.Items {
		types = append(types, d.Type)
	}
	want := []string{"order", "delivery-note", "invoice", "received-invoice", "credit-note", "payslip", "tax-filing"}
	if len(types) != len(want) {
		t.Fatalf("register: %v", types)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("register by date: %v", types)
		}
	}
	rec, pay, fil, crd := page.Items[3], page.Items[5], page.Items[6], page.Items[4]
	if rec.Number != "FR-2026-000001" || rec.Reference != "2026/0042" || rec.Party != supplier || !rec.Cancelled || rec.CancelReason != "duplicada" ||
		rec.Version != 2 || pay.Number != "ordinary 2026-05" || pay.Date != "2026-05-29" || pay.Total != "1500.00" || fil.Number != "111-2026-2T" ||
		fil.Date != "2026-07-15" || fil.Party != "" || !fil.Cancelled || crd.Relation != "rectifies" || crd.OriginID != invoice || crd.Total != "-24.20" ||
		page.Items[0].Date != "2026-05-04" || page.Items[2].OriginType != "delivery-note" || page.Items[2].Relation != "originates-from" {
		t.Fatalf("entries: %+v", page.Items)
	}
	count := func(query string, n int) {
		t.Helper()
		var p fw.Page[dapp.DocumentDTO]
		h.must(h.get("/api/documents?company="+acme+"&"+query, "viewer", &p), 200, query)
		if len(p.Items) != n {
			t.Fatalf("%s: %d documents, want %d", query, len(p.Items), n)
		}
	}
	count("type=invoice", 1)
	count("party="+customer, 4)
	count("number=alb", 1)
	count("from=2026-05-10&to=2026-05-31", 3)
	count("live=true", 5)
	h.must(h.get("/api/documents", "outsider", &page), 200, "outsider's register")
	if len(page.Items) != 1 || page.Items[0].Company != globex {
		t.Fatalf("outsider: %+v", page.Items)
	}

	var doc dapp.DocumentDTO
	h.must(h.get("/api/documents/by-fact/letter/"+delivery, "viewer", nil), 400, "type")
	h.must(h.get("/api/documents/by-fact/delivery-note/"+id(), "viewer", nil), 404, "unknown fact")
	h.must(h.get("/api/documents/by-fact/delivery-note/"+delivery, "outsider", nil), 404, "outsider")
	h.must(h.get("/api/documents/by-fact/delivery-note/"+delivery, "viewer", &doc), 200, "by fact")
	if doc.Number != "ALB-2026-000001" || doc.OriginID != order {
		t.Fatalf("by fact: %+v", doc)
	}
	h.must(h.get("/api/documents/"+crd.ID, "outsider", nil), 404, "outsider")
	h.must(h.get("/api/documents/"+crd.ID, "viewer", &doc), 200, "get")

	// The trail is the same from either end.
	for _, from := range []string{crd.ID, page0(t, h, acme)} {
		var trail []dapp.DocumentDTO
		h.must(h.get("/api/documents/"+from+"/trail", "viewer", &trail), 200, "trail")
		if len(trail) != 4 || trail[0].Number != "PED-2026-000001" || trail[1].Number != "ALB-2026-000001" || trail[2].Number != "A-2026-000001" ||
			trail[3].Number != "R-2026-000001" {
			t.Fatalf("trail: %+v", trail)
		}
	}
	h.must(h.get("/api/documents/"+crd.ID+"/trail", "outsider", nil), 404, "outsider's trail")

	// The port other contexts use.
	got, found, err := h.docs.Register.ByFact(context.Background(), "invoice", invoice)
	if err != nil || !found || got.Number != "A-2026-000001" || got.Date != "2026-05-08" || got.Cancelled {
		t.Fatalf("port: %+v %v %v", got, found, err)
	}
	if _, found, err := h.docs.Register.ByFact(context.Background(), "invoice", id()); err != nil || found {
		t.Fatalf("port, unknown fact: %v %v", found, err)
	}
}

// page0 returns the id of the first document of a company: its order.
func page0(t *testing.T, h *host, company string) string {
	t.Helper()
	var p fw.Page[dapp.DocumentDTO]
	h.must(h.get("/api/documents?company="+company+"&type=order", "viewer", &p), 200, "order")
	if len(p.Items) != 1 {
		t.Fatalf("order: %+v", p.Items)
	}
	return p.Items[0].ID
}

func TestDocuments_RecordsWhatIsIssued_MemoryThenSQLite(t *testing.T) {
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
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{dinfra.Migrations()})
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

func TestDocuments_LayersRespectTheArchitecture(t *testing.T) {
	const m = "github.com/jhermoso/karpo-fw-go"
	archtest.AssertOnlyImports(t, "domain", false, archtest.Std, m+"/pkg/domain/...", m+"/contexts/documents/domain")
	archtest.AssertOnlyImports(t, "contracts", false, archtest.Std)
	archtest.AssertTreeDoesNotImport(t, "application", []string{"/pkg/persistence", "/pkg/distribution", "database/sql", "net/http"})
}
