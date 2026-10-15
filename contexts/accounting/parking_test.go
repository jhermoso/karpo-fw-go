package accounting_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/accounting"
	aapp "github.com/jhermoso/karpo-fw-go/contexts/accounting/application"
	"github.com/jhermoso/karpo-fw-go/contexts/accounting/domain"
	ainfra "github.com/jhermoso/karpo-fw-go/contexts/accounting/infrastructure"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
)

// parkingScenario walks a company that starts selling before it has books: what Accounting cannot
// post yet waits, in order, and is posted when it can.
func parkingScenario(t *testing.T, sw *hotswap.Switch) {
	ctx := context.Background()
	am := accounting.Compose(sw)
	svc := am.Service
	admin, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "admin", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
	admin.GlobalAdmin = true
	actx := authz.WithContext(ctx, admin)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	clock := time.Date(2026, 3, 10, 9, 0, 0, 0, time.UTC)
	envelope := func(typ, subject string, data any) app.Envelope {
		clock = clock.Add(time.Minute)
		raw, _ := json.Marshal(data)
		return app.Envelope{ID: fw.NewUUID().String(), Type: typ, Source: "test", Subject: subject, OccurredAt: clock, Data: raw}
	}
	invoice := func(company, number, date string) app.Envelope {
		id := fw.NewUUID().String()
		return envelope("billing.invoice-issued.v1", id, map[string]any{"invoiceId": id, "number": number, "seller": company, "customer": fw.NewUUID().String(),
			"issueDate": date, "net": "100.00", "total": "121.00", "taxes": []map[string]any{{"taxCode": "G", "amount": "21.00"}}})
	}
	collection := func(company string, inv app.Envelope) app.Envelope {
		return envelope("receivables.collection-allocated.v1", fw.NewUUID().String(), map[string]any{"collectionId": fw.NewUUID().String(), "seller": company,
			"payer": fw.NewUUID().String(), "method": "cash", "invoiceId": inv.Subject, "installment": 1, "amount": "121.00", "on": "2026-03-12"})
	}
	// books opens the chart and the ledger of a company; roles says which accounts its profile has.
	books := func(company string, roles map[string]string) aapp.LedgerDTO {
		t.Helper()
		for code, name := range map[string]string{"4300": "Clientes", "7000": "Ventas", "4770": "HP IVA repercutido", "5700": "Caja"} {
			_, err := svc.CreateAccount.Handle(actx, aapp.CreateAccount{Company: company, Code: code, Name: name, Postable: true})
			must(err)
		}
		l, err := svc.OpenLedger.Handle(actx, aapp.OpenLedger{Company: company, StartMonth: 1, Accounts: roles})
		must(err)
		return l
	}
	entries := func(company string) int64 {
		t.Helper()
		p, err := svc.SearchEntries.Handle(actx, aapp.SearchEntries{Company: company})
		must(err)
		return p.Total
	}
	parked := func(q aapp.SearchParked) []aapp.ParkedDTO {
		t.Helper()
		q.Size = 50
		p, err := svc.SearchParked.Handle(actx, q)
		must(err)
		return p.Items
	}
	retry := func(company string, posted, waiting int) {
		t.Helper()
		r, err := svc.RetryParked.Handle(actx, aapp.RetryParked{Company: company})
		if err != nil || r.Posted != posted || r.Waiting != waiting {
			t.Fatalf("retry: %+v %v, want %d posted and %d waiting", r, err, posted, waiting)
		}
	}
	violates := func(err error, code string) {
		t.Helper()
		var rv *fw.RuleViolationError
		if !errors.As(err, &rv) || rv.Code != code {
			t.Fatalf("want %s: %v", code, err)
		}
	}
	shop, old := fw.NewUUID().String(), fw.NewUUID().String()
	full := map[string]string{"customers": "4300", "revenue": "7000", "output-tax": "4770", "cash": "5700"}
	books(old, full)

	// The consumer alone refuses the fact of a company without books: its publisher would send it
	// again and again, and with it hold back every other listener.
	first := invoice(shop, "F-1", "2026-03-10")
	violates(am.Consumer.HandleMessage(ctx, first), "accounting.no_ledger")

	// Behind the parking, the fact is taken and kept. Sent again, it is not kept twice.
	must(am.Parking.HandleMessage(ctx, first))
	must(am.Parking.HandleMessage(ctx, first))
	paid := collection(shop, first)
	must(am.Parking.HandleMessage(ctx, paid))
	// A fact that names no company follows the one it is about.
	cancel := envelope("payroll.payslip-cancelled.v1", first.Subject, map[string]any{"payslipId": first.Subject})
	must(am.Parking.HandleMessage(ctx, cancel))
	// What Accounting does not post is none of its business.
	must(am.Parking.HandleMessage(ctx, envelope("parties.party-registered.v1", first.Subject, map[string]any{"partyId": first.Subject})))
	waiting := parked(aapp.SearchParked{Company: shop})
	if len(waiting) != 3 || waiting[0].Code != "accounting.no_ledger" || waiting[0].Attempts != 1 || waiting[0].EventType != "billing.invoice-issued.v1" ||
		waiting[1].Code != domain.WaitingCode || waiting[2].Code != domain.WaitingCode || waiting[2].Company != shop || waiting[0].Status != "parked" ||
		string(waiting[0].Data) != string(first.Data) {
		t.Fatalf("kept: %+v", waiting)
	}
	if entries(shop) != 0 {
		t.Fatal("nothing posted yet")
	}

	// A company with books is not held back by one without.
	must(am.Parking.HandleMessage(ctx, invoice(old, "F-9", "2026-03-11")))
	if entries(old) != 1 || len(parked(aapp.SearchParked{Company: old})) != 0 {
		t.Fatal("the company with books posts at once")
	}

	// Trying again changes nothing while the reason stands, not even the count of attempts.
	retry(shop, 0, 3)
	if w := parked(aapp.SearchParked{Company: shop}); w[0].Attempts != 1 || w[1].Attempts != 1 || w[0].Version != 1 {
		t.Fatalf("attempts: %+v", w)
	}

	// The shop opens its books, without an account for cash: the invoice is posted, the collection
	// stops, and what came after it stays behind.
	ledger := books(shop, map[string]string{"customers": "4300", "revenue": "7000", "output-tax": "4770"})
	retry(shop, 1, 2)
	w := parked(aapp.SearchParked{Company: shop, Status: "parked"})
	if len(w) != 2 || w[0].Code != "accounting.role_undefined" || w[0].EventType != "receivables.collection-allocated.v1" || entries(shop) != 1 {
		t.Fatalf("after the books: %+v, %d entries", w, entries(shop))
	}
	// While something waits, what arrives now waits too: facts are posted in the order they happened.
	late := invoice(shop, "F-2", "2026-03-13")
	must(am.Parking.HandleMessage(ctx, late))
	if entries(shop) != 1 {
		t.Fatal("a new fact does not overtake what waits")
	}
	lid, _ := fw.ParseUUID(ledger.ID)
	_, err := svc.SetProfile.Handle(actx, aapp.SetProfile{ID: domain.LedgerID{UUID: lid}, Accounts: full})
	must(err)
	retry("", 3, 0) // every company at once, as the scheduler does
	if entries(shop) != 3 || len(parked(aapp.SearchParked{Company: shop, Status: "parked"})) != 0 || len(parked(aapp.SearchParked{Company: shop, Status: "posted"})) != 4 {
		t.Fatalf("all posted: %d entries, %+v", entries(shop), parked(aapp.SearchParked{Company: shop}))
	}
	// Nothing waits any more: a fact is posted as it arrives, and one already posted is not posted again.
	must(am.Parking.HandleMessage(ctx, invoice(shop, "F-3", "2026-03-14")))
	must(am.Parking.HandleMessage(ctx, first))
	must(am.Consumer.HandleMessage(ctx, first))
	if entries(shop) != 4 {
		t.Fatalf("entries: %d", entries(shop))
	}

	// A fact that will never be posted (it is wrong) holds its company until somebody gives it up.
	other := fw.NewUUID().String()
	books(other, full)
	wrong := invoice(other, "F-X", "not-a-date")
	good := invoice(other, "F-4", "2026-03-15")
	must(am.Parking.HandleMessage(ctx, wrong))
	must(am.Parking.HandleMessage(ctx, good))
	retry(other, 0, 2)
	stuck := parked(aapp.SearchParked{Company: other})
	if len(stuck) != 2 || stuck[0].Code != "accounting.invalid_event" {
		t.Fatalf("stuck: %+v", stuck)
	}
	wid, _ := domain.ParseParkedID(stuck[0].ID)
	_, err = svc.DiscardParked.Handle(actx, aapp.DiscardParked{ID: wid})
	violates(err, "accounting.discard_note")
	gone, err := svc.DiscardParked.Handle(actx, aapp.DiscardParked{ID: wid, Note: "Factura de prueba con la fecha mal"})
	if err != nil || gone.Status != "discarded" || gone.Note == "" || gone.ResolvedAt == "" {
		t.Fatalf("discarded: %+v %v", gone, err)
	}
	_, err = svc.DiscardParked.Handle(actx, aapp.DiscardParked{ID: wid, Note: "otra vez"})
	violates(err, "accounting.not_parked")
	retry(other, 1, 0)
	if entries(other) != 1 {
		t.Fatalf("entries of the other: %d", entries(other))
	}
	must(am.Parking.HandleMessage(ctx, wrong)) // sent again after it was given up: still given up
	if len(parked(aapp.SearchParked{Company: other})) != 2 {
		t.Fatal("a discarded fact is not kept again")
	}

	// Each one sees what waits in its companies; nobody tries again those of another.
	scoped := func(perms []authz.Permission, orgs ...string) context.Context {
		c := authz.Context{Subject: fw.NewUUID(), SubjectName: "accountant", Kind: authz.Service, Permissions: perms}
		for _, o := range orgs {
			c.Grants = append(c.Grants, authz.Grant{OrganizationID: fw.MustParseUUID(o), Level: authz.Full})
		}
		ac, err := authz.NewContext(c)
		must(err)
		return authz.WithContext(ctx, ac)
	}
	accountant := scoped([]authz.Permission{aapp.PermParkedRead, aapp.PermParkedResolve}, other)
	if p, err := svc.SearchParked.Handle(accountant, aapp.SearchParked{}); err != nil || p.Total != 2 {
		t.Fatalf("the accountant of the other: %+v %v", p, err)
	}
	if _, err := svc.RetryParked.Handle(accountant, aapp.RetryParked{Company: shop}); !errors.Is(err, fw.ErrNotFound) {
		t.Fatalf("another company: %v", err)
	}
	if _, err := svc.SearchParked.Handle(scoped([]authz.Permission{aapp.PermEntryRead}, other), aapp.SearchParked{}); !errors.Is(err, fw.ErrForbidden) {
		t.Fatalf("without the permission: %v", err)
	}
	if _, err := svc.SearchParked.Handle(actx, aapp.SearchParked{Status: "lost"}); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("status: %v", err)
	}
}

func TestAccounting_KeepsWhatItCannotPostYet_MemoryThenSQLite(t *testing.T) {
	ctx := context.Background()
	mem := hotswap.New(memory.NewStore("memory"))
	t.Cleanup(func() { _ = mem.Close(ctx) })
	parkingScenario(t, mem)

	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "host.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{ainfra.Migrations()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	parkingScenario(t, hotswap.New(db))
}

// stubInner is a consumer that answers each message what the test says.
type stubInner struct {
	answer func(app.Envelope) error
}

func (s *stubInner) Name() string    { return accounting.Source }
func (s *stubInner) Types() []string { return []string{"billing.invoice-issued.v1"} }
func (s *stubInner) HandleMessage(_ context.Context, env app.Envelope) error {
	return s.answer(env)
}

// countedParked counts what is written of the parked facts.
type countedParked struct {
	domain.ParkedRepository
	saves int
}

func (c *countedParked) Save(ctx context.Context, f *domain.ParkedFact) error {
	c.saves++
	return c.ParkedRepository.Save(ctx, f)
}

// parkingOver puts the parking before a consumer of the test, on a backend in memory. It gives
// the service, the parking, a context that may do everything, what makes the invoice of a
// company at a minute, and the repository, which counts what is written.
func parkingOver(t *testing.T, inner aapp.Inner) (*aapp.Service, *aapp.Parking, context.Context, func(company string, minute int) app.Envelope, *countedParked) {
	t.Helper()
	sw := hotswap.New(memory.NewStore("memory"))
	t.Cleanup(func() { _ = sw.Close(context.Background()) })
	d := aapp.Deps{
		Accounts: hotswap.Repository(sw, ainfra.AccountRepositoryFactory), Ledgers: hotswap.Repository(sw, ainfra.LedgerRepositoryFactory),
		Entries: hotswap.Repository(sw, ainfra.EntryRepositoryFactory), Counters: hotswap.Repository(sw, ainfra.CounterRepositoryFactory),
		UoW: sw,
	}
	parked := &countedParked{ParkedRepository: hotswap.Repository(sw, ainfra.ParkedRepositoryFactory)}
	d.Parked = parked
	d.Parking = aapp.NewParking(inner, d)
	admin, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "admin", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
	admin.GlobalAdmin = true
	invoice := func(company string, minute int) app.Envelope {
		id := fw.NewUUID().String()
		raw, _ := json.Marshal(map[string]any{"invoiceId": id, "seller": company})
		return app.Envelope{ID: fw.NewUUID().String(), Type: "billing.invoice-issued.v1", Source: "test", Subject: id,
			OccurredAt: time.Date(2026, 3, 10, 9, minute, 0, 0, time.UTC), Data: raw}
	}
	return aapp.NewService(d), d.Parking, authz.WithContext(context.Background(), admin), invoice, parked
}

// A fact that fails with something that is not a rule stops its own company and no other.
func TestParking_AFailureThatIsNoRuleStopsItsCompanyAlone(t *testing.T) {
	inner := &stubInner{answer: func(app.Envelope) error { return fw.Violation("accounting.no_ledger", "the company has no ledger") }}
	svc, parking, actx, invoice, _ := parkingOver(t, inner)
	one, other := fw.NewUUID().String(), fw.NewUUID().String()
	bad, behind, good := invoice(one, 1), invoice(one, 2), invoice(other, 3)
	for _, env := range []app.Envelope{bad, behind, good} {
		if err := parking.HandleMessage(context.Background(), env); err != nil {
			t.Fatal(err)
		}
	}

	// The oldest fact, the one tried first, now fails with a validation; the rest could be posted.
	tried := []string{}
	inner.answer = func(env app.Envelope) error {
		tried = append(tried, env.ID)
		if env.ID == bad.ID {
			var v fw.Validation
			v.Add("issueDate", "required", "the date of the invoice")
			return v.Err()
		}
		return nil
	}
	r, err := svc.RetryParked.Handle(actx, aapp.RetryParked{})
	if r.Posted != 1 || r.Waiting != 2 {
		t.Fatalf("retry: %+v, want 1 posted and 2 waiting", r)
	}
	if !errors.Is(err, fw.ErrValidation) || !strings.Contains(err.Error(), bad.ID) {
		t.Fatalf("what failed is not given at the end: %v", err)
	}
	if len(tried) != 2 || tried[0] != bad.ID || tried[1] != good.ID {
		t.Fatalf("tried: %v, want the bad one and that of the other company", tried)
	}
	kept := func(company string) []aapp.ParkedDTO {
		t.Helper()
		p, err := svc.SearchParked.Handle(actx, aapp.SearchParked{Company: company, Size: 50})
		if err != nil {
			t.Fatal(err)
		}
		return p.Items
	}
	if k := kept(other); len(k) != 1 || k[0].Status != "posted" {
		t.Fatalf("the other company: %+v", k)
	}
	k := kept(one)
	if len(k) != 2 || k[0].Status != "parked" || k[0].Code != domain.FailedCode || !strings.Contains(k[0].Reason, "issueDate") ||
		k[1].Status != "parked" || k[1].Code != domain.WaitingCode {
		t.Fatalf("the company of the fact that failed: %+v", k)
	}
}

// The scheduler retries every round. A fact refused again for the same reason is written once,
// when it is kept, and not once a round; when the reason changes, once more.
func TestParking_TheSameRefusalIsNotWrittenAgain(t *testing.T) {
	refusal := fw.Violation("accounting.no_ledger", "the company has no ledger")
	tries := 0
	inner := &stubInner{answer: func(app.Envelope) error { tries++; return refusal }}
	svc, parking, actx, invoice, parked := parkingOver(t, inner)
	company := fw.NewUUID().String()
	if err := parking.HandleMessage(context.Background(), invoice(company, 1)); err != nil {
		t.Fatal(err)
	}
	rounds := func(n int) aapp.ParkedDTO {
		t.Helper()
		for range n {
			if r, err := svc.RetryParked.Handle(actx, aapp.RetryParked{}); err != nil || r.Posted != 0 || r.Waiting != 1 {
				t.Fatalf("retry: %+v %v", r, err)
			}
		}
		p, err := svc.SearchParked.Handle(actx, aapp.SearchParked{Company: company})
		if err != nil || len(p.Items) != 1 {
			t.Fatalf("kept: %+v %v", p.Items, err)
		}
		return p.Items[0]
	}
	const n = 25
	if f := rounds(n); tries != 1+n || parked.saves != 1 || f.Attempts != 1 || f.Version != 1 || f.Code != "accounting.no_ledger" {
		t.Fatalf("%d rounds for the same reason: %d tries, %d writes, %+v; want one write", n, tries, parked.saves, f)
	}
	// Another reason is news: it is written, once.
	refusal = fw.Violation("accounting.role_undefined", "the profile has no account for cash")
	if f := rounds(n); parked.saves != 2 || f.Attempts != 2 || f.Version != 2 || f.Code != "accounting.role_undefined" {
		t.Fatalf("%d rounds for another reason: %d writes, %+v; want two writes", n, parked.saves, f)
	}
	// And so is a failure that is no rule (it is given back every round, and written once).
	refusal = errors.New("the database is down")
	for range n {
		if _, err := svc.RetryParked.Handle(actx, aapp.RetryParked{}); err == nil {
			t.Fatal("the failure is not given back")
		}
	}
	if parked.saves != 3 {
		t.Fatalf("%d rounds failing the same way: %d writes, want three", n, parked.saves)
	}
}
