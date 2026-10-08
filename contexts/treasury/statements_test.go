package treasury_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/treasury"
	tapp "github.com/jhermoso/karpo-fw-go/contexts/treasury/application"
	"github.com/jhermoso/karpo-fw-go/contexts/treasury/domain"
	tinfra "github.com/jhermoso/karpo-fw-go/contexts/treasury/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authorization"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution/jwtauth"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
	"github.com/jhermoso/karpo-fw-go/pkg/time/fake"
)

// n43 builds a Cuaderno 43 file of one account (bank 2100, office 0813, number 0123456789) for
// May 2026 from an opening balance in cents and movements (day, cents, concept).
type n43Movement struct {
	day     int
	cents   int64
	concept string
}

func n43(number string, opening int64, moves []n43Movement, closingDelta int64) string {
	amount := func(c int64) string {
		key := "2"
		if c < 0 {
			key, c = "1", -c
		}
		return key + fmt.Sprintf("%014d", c)
	}
	pad := func(s string, n int) string { return s + strings.Repeat(" ", n-len([]rune(s))) }
	var b strings.Builder
	b.WriteString("11" + "2100" + "0813" + number + "260501" + "260531" + amount(opening) + "978" + "3" + pad("ACME SA", 26) + "   \n")
	closing, debits, credits := opening, int64(0), int64(0)
	nd, nc := 0, 0
	for _, m := range moves {
		day := fmt.Sprintf("2605%02d", m.day)
		b.WriteString("22" + "    " + "0813" + day + day + "02" + "011" + amount(m.cents) + "0000000000" + pad("REF", 12) + pad("", 16) + "\n")
		b.WriteString("23" + "01" + pad(m.concept, 38) + pad("", 38) + "\n")
		closing += m.cents
		if m.cents < 0 {
			nd, debits = nd+1, debits-m.cents
		} else {
			nc, credits = nc+1, credits+m.cents
		}
	}
	b.WriteString("33" + "2100" + "0813" + number + fmt.Sprintf("%05d%014d%05d%014d", nd, debits, nc, credits) + amount(closing+closingDelta) + "978" + "    \n")
	b.WriteString("88" + strings.Repeat("9", 18) + fmt.Sprintf("%06d", 2+2*len(moves)) + "\n")
	return b.String()
}

var may = []n43Movement{{5, 36300, "ADEUDOS SEPA REMESA"}, {12, -106000, "ORDEN DE TRANSFERENCIAS"}, {29, -250, "COMISIÓN DE MANTENIMIENTO"}}

func TestNorma43_Parses(t *testing.T) {
	accounts, err := domain.ParseNorma43(strings.ReplaceAll(n43("0123456789", 1000000, may, 0), "\n", "\r\n"))
	if err != nil || len(accounts) != 1 {
		t.Fatalf("parse: %+v %v", accounts, err)
	}
	a := accounts[0]
	if a.Bank != "2100" || a.Office != "0813" || a.Number != "0123456789" || a.From != vocab.MustDate(2026, 5, 1) || a.To != vocab.MustDate(2026, 5, 31) ||
		!a.Opening.Equal(vocab.MustDecimal("10000")) || !a.Closing.Equal(vocab.MustDecimal("9300.50")) || len(a.Lines) != 3 {
		t.Fatalf("account: %+v", a)
	}
	if l := a.Lines[1]; l.No != 2 || l.Date != vocab.MustDate(2026, 5, 12) || !l.Amount.Equal(vocab.MustDecimal("-1060")) || l.Concept != "ORDEN DE TRANSFERENCIAS" {
		t.Fatalf("movement: %+v", l)
	}
	if a.Lines[2].Concept != "COMISIÓN DE MANTENIMIENTO" {
		t.Fatalf("concept: %q", a.Lines[2].Concept)
	}
	iban, _ := vocab.NewIBAN("ES7921000813610123456789")
	other, _ := vocab.NewIBAN("ES9121000418450200051332")
	if !a.Matches(iban) || a.Matches(other) {
		t.Fatal("the account of the file is matched by bank, office and number")
	}
	// ISO 8859-1, as banks still send it.
	latin := strings.ReplaceAll(n43("0123456789", 1000000, may, 0), "Ó", "\xd3")
	if accounts, err := domain.ParseNorma43(latin); err != nil || accounts[0].Lines[2].Concept != "COMISIÓN DE MANTENIMIENTO" {
		t.Fatalf("latin-1: %v", err)
	}
	for name, content := range map[string]string{
		"closing balance": n43("0123456789", 1000000, may, 1),
		"no totals":       strings.Join(strings.Split(n43("0123456789", 1000000, may, 0), "\n")[:5], "\n"),
		"unknown record":  "99" + strings.Repeat(" ", 78),
		"empty":           "\n\n",
		"other currency":  strings.Replace(n43("0123456789", 1000000, may, 0), "978", "840", 1),
		"movement first":  strings.Join(strings.Split(n43("0123456789", 1000000, may, 0), "\n")[1:], "\n"),
	} {
		var rv *fw.RuleViolationError
		if _, err := domain.ParseNorma43(content); !errors.As(err, &rv) || rv.Code != "treasury.norma43" {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestStatement_Rules(t *testing.T) {
	owner, account := domain.OrganizationID{UUID: fw.NewUUID()}, domain.AccountID{UUID: fw.NewUUID()}
	day := func(d int) vocab.Date { return vocab.MustDate(2026, 5, d) }
	dec := vocab.MustDecimal
	ok := func() domain.StatementState {
		return domain.StatementState{Owner: owner, Account: account, From: day(1), To: day(31), Opening: dec("100"), Closing: dec("160.50"),
			Lines: []domain.StatementLine{{Date: day(5), Amount: dec("63")}, {Date: day(9), Amount: dec("-2.50"), Concept: "  Comisión   de  mantenimiento "}}}
	}
	bad := []func(*domain.StatementState){
		func(s *domain.StatementState) { s.Closing = dec("160") },
		func(s *domain.StatementState) { s.To = vocab.MustDate(2026, 4, 30) },
		func(s *domain.StatementState) { s.Lines[0].Date = vocab.MustDate(2026, 6, 1) },
		func(s *domain.StatementState) { s.Lines[0].Amount, s.Closing = dec("0"), dec("97.50") },
		func(s *domain.StatementState) { s.Lines[0].Amount, s.Closing = dec("63.001"), dec("160.501") },
		func(s *domain.StatementState) { s.Account = domain.AccountID{} },
	}
	for i, f := range bad {
		s := ok()
		f(&s)
		if _, err := domain.ImportStatement(domain.NewStatementID(), s); !errors.Is(err, fw.ErrValidation) {
			t.Fatalf("case %d: %v", i, err)
		}
	}
	s, err := domain.ImportStatement(domain.NewStatementID(), ok())
	if err != nil || s.Pending() != 2 || s.State().Lines[1].No != 2 || s.State().Lines[1].Concept != "Comisión de mantenimiento" ||
		s.State().Lines[0].ValueDate != day(5) {
		t.Fatalf("imported: %+v %v", s, err)
	}
	violates := func(err error, code string) {
		t.Helper()
		var rv *fw.RuleViolationError
		if !errors.As(err, &rv) || rv.Code != code {
			t.Fatalf("want %s: %v", code, err)
		}
	}
	violates(s.Reconcile(3, domain.Match{Kind: domain.MatchOther, Note: "x"}), "treasury.statement_line")
	violates(s.Reconcile(1, domain.Match{Kind: domain.MatchOther}), "treasury.match")
	violates(s.Reconcile(1, domain.Match{Kind: domain.MatchRemittance}), "treasury.match")
	violates(s.Reconcile(1, domain.Match{Kind: "gift", ID: "x"}), "treasury.match")
	violates(s.Release(1), "treasury.line_pending")
	if err := s.Reconcile(1, domain.Match{Kind: domain.MatchRemittance, ID: "r-1"}); err != nil || s.Pending() != 1 {
		t.Fatal(err)
	}
	violates(s.Reconcile(1, domain.Match{Kind: domain.MatchOther, Note: "x"}), "treasury.line_reconciled")
	before := len(s.PendingEvents())
	if err := s.Reconcile(2, domain.Match{Kind: domain.MatchOther, Note: "Comisión"}); err != nil || s.Pending() != 0 || len(s.PendingEvents()) != before+1 {
		t.Fatalf("the last movement reconciles the statement: %v", err)
	}
	if err := s.Release(1); err != nil || s.Pending() != 1 {
		t.Fatal(err)
	}
}

// others plays the contexts Treasury asks: what Receivables has to collect, what Payments has to
// pay, and who the parties are.
type others struct {
	due []domain.DueItem
	pay []domain.PayableDue
}

func (o *others) DueItems(context.Context, domain.OrganizationID, vocab.Date) ([]domain.DueItem, error) {
	return o.due, nil
}

func (o *others) DueForTransfer(context.Context, domain.OrganizationID, vocab.Date) ([]domain.PayableDue, int, error) {
	return o.pay, 0, nil
}

func (o *others) Identities(_ context.Context, parties []domain.PartyID) (map[domain.PartyID]domain.Identity, error) {
	out := map[domain.PartyID]domain.Identity{}
	for _, p := range parties {
		out[p] = domain.Identity{Name: "Acme, S.A.", NIF: "A58818501"}
	}
	return out, nil
}

// bank composes Treasury alone, with the other contexts played by the test.
func bank(t *testing.T) (*host, *others) {
	ctx := context.Background()
	sw := hotswap.New(memory.NewStore("memory"))
	world := &others{}
	tm := treasury.Compose(sw, world, world, world)
	jwt, _ := jwtauth.New(jwtauth.Config{Secret: []byte("statements-test")})
	dir := authorization.NewMemoryDirectory()
	h := &host{t: t, sw: sw, dir: dir, ids: map[string]fw.UUID{}, tokens: map[string]string{}, treasury: tm}
	all := []authz.Permission{tapp.PermStatementRead, tapp.PermStatementImport, tapp.PermStatementReconcile}
	users := map[string][]authz.Permission{"importer": {tapp.PermStatementRead, tapp.PermStatementImport},
		"reconciler": {tapp.PermStatementRead, tapp.PermStatementReconcile}, "viewer": {tapp.PermStatementRead}, "outsider": all}
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
	return h, world
}

func (h *host) statements(tag string, world *others) {
	t := h.t
	ctx := h.adminCtx
	svc := h.treasury.Service
	acme, globex := fw.NewUUID().String(), fw.NewUUID().String()
	for _, u := range []string{"importer", "reconciler", "viewer"} {
		h.grant(u, acme)
	}
	h.grant("outsider", globex)
	day := func(m time.Month, d int) vocab.Date { return vocab.MustDate(2026, m, d) }

	main, err := svc.OpenAccount.Handle(ctx, tapp.OpenAccount{Owner: acme, IBAN: "ES7921000813610123456789", Alias: "Principal", Collections: true,
		Payments: true, Opened: vocab.MustDate(2020, 1, 1)})
	h.ok(err)
	second, err := svc.OpenAccount.Handle(ctx, tapp.OpenAccount{Owner: acme, IBAN: "ES9121000418450200051332", Alias: "Otra", Collections: true,
		Opened: vocab.MustDate(2020, 1, 1)})
	h.ok(err)
	debtor := fw.NewUUID()
	_, err = svc.RegisterMandate.Handle(ctx, tapp.RegisterMandate{Creditor: acme, Debtor: debtor.String(), IBAN: "ES6621000418401234567891",
		Reference: "M-" + tag, Scheme: "CORE", Signed: day(1, 1)})
	h.ok(err)

	// What was sent to the bank at the beginning of May: remittances of direct debits and an order
	// of transfers, through the use cases (on a fixed clock: files are generated before they are due).
	debtorIBAN, _ := vocab.NewIBAN("ES6621000418401234567891")
	restore := fw.SetClock(fake.New(time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)))
	remittance := func(account string, on vocab.Date, amount string) string {
		t.Helper()
		world.due = []domain.DueItem{{Invoice: domain.InvoiceID{UUID: fw.NewUUID()}, Number: "A" + fw.NewUUID().String()[24:], Customer: domain.PartyID{UUID: debtor},
			Installment: 1, Due: on, Open: vocab.MustDecimal(amount)}}
		r, err := svc.Propose.Handle(ctx, tapp.ProposeRemittance{Creditor: acme, Account: account, Scheme: "CORE", CollectionDate: on, DueTo: on})
		h.ok(err)
		rid, _ := domain.ParseRemittanceID(r.ID)
		_, err = svc.Generate.Handle(ctx, tapp.GenerateRemittance{ID: rid})
		h.ok(err)
		return r.ID
	}
	rem := remittance(main.ID, day(5, 5), "363.00")
	foreign := remittance(second.ID, day(5, 5), "363.00")
	late := remittance(main.ID, day(6, 20), "500.00")
	world.pay = []domain.PayableDue{{Payable: domain.PayableID{UUID: fw.NewUUID()}, Kind: "supplier-invoice", Document: "FR-2026-000001",
		Payee: fw.NewUUID().String(), Due: day(5, 12), PayTo: []domain.PayableAccount{{IBAN: debtorIBAN, Amount: vocab.MustDecimal("1060.00")}}}}
	order, err := svc.ProposeTransfers.Handle(ctx, tapp.ProposeTransfers{Debtor: acme, Account: main.ID, ExecutionDate: day(5, 12), DueTo: day(5, 12)})
	h.ok(err)
	oid, _ := domain.ParseTransferOrderID(order.ID)
	_, err = svc.GenerateTransfers.Handle(ctx, tapp.GenerateTransfers{ID: oid})
	h.ok(err)
	trf := order.ID
	restore()

	// The bank's file for May.
	file := func(content string) map[string]any { return map[string]any{"owner": acme, "content": content} }
	good := n43("0123456789", 1000000, may, 0)
	var imported []tapp.StatementDTO
	h.must(h.do("POST", "/api/treasury/statements/norma43", "reconciler", file(good), nil), 403, "reconciling is not importing")
	h.must(h.do("POST", "/api/treasury/statements/norma43", "outsider", file(good), nil), 404, "outsider")
	h.must(h.do("POST", "/api/treasury/statements/norma43", "importer", file(""), nil), 400, "empty file")
	h.must(h.do("POST", "/api/treasury/statements/norma43", "importer", file(n43("0123456789", 1000000, may, 1)), nil), 422, "file that does not add up")
	h.must(h.do("POST", "/api/treasury/statements/norma43", "importer", file(n43("9999999999", 1000000, may, 0)), nil), 422, "account of someone else")
	h.must(h.do("POST", "/api/treasury/statements/norma43", "importer", file(good), &imported), 201, "May")
	h.must(h.do("POST", "/api/treasury/statements/norma43", "importer", file(good), nil), 422, "the period is recorded once")
	if len(imported) != 1 || imported[0].Account != main.ID || imported[0].Movements != 3 || imported[0].Pending != 3 || imported[0].Opening != "10000.00" ||
		imported[0].Closing != "9300.50" || imported[0].From != "2026-05-01" {
		t.Fatalf("May: %+v", imported)
	}
	mayID := imported[0].ID

	// June by hand: it must continue May.
	june := func(opening, closing string) map[string]any {
		return map[string]any{"account": main.ID, "from": "2026-06-01", "to": "2026-06-30", "opening": opening, "closing": closing,
			"lines": []map[string]any{{"date": "2026-06-03", "amount": "500.00", "concept": "Adeudos SEPA"}, {"date": "2026-06-10", "amount": "-20.00", "concept": "Seguro"}}}
	}
	var jun, jul tapp.StatementDTO
	h.must(h.do("POST", "/api/treasury/statements", "reconciler", june("9300.50", "9780.50"), nil), 403, "reconciling is not importing")
	h.must(h.do("POST", "/api/treasury/statements", "importer", june("9000.00", "9480.00"), nil), 422, "it does not continue May")
	h.must(h.do("POST", "/api/treasury/statements", "importer", june("9300.50", "9999.99"), nil), 400, "it does not add up")
	h.must(h.do("POST", "/api/treasury/statements", "importer", june("9300.50", "9780.50"), &jun), 201, "June")
	h.must(h.do("POST", "/api/treasury/statements", "importer", map[string]any{"account": main.ID, "from": "2026-07-01", "to": "2026-07-31",
		"opening": "9780.50", "closing": "10143.50", "lines": []map[string]any{{"date": "2026-07-02", "amount": "363.00"}}}, &jul), 201, "July")

	// Automatic: the remittance and the order of the account, by amount and date; the fee is left.
	var stm tapp.StatementDTO
	h.must(h.do("POST", "/api/treasury/statements/"+mayID+"/auto-reconcile", "importer", nil, nil), 403, "importing is not reconciling")
	h.must(h.do("POST", "/api/treasury/statements/"+mayID+"/auto-reconcile", "outsider", nil, nil), 404, "outsider")
	h.must(h.do("POST", "/api/treasury/statements/"+mayID+"/auto-reconcile", "reconciler", nil, &stm), 200, "automatic")
	if stm.Matched != 2 || stm.Pending != 1 || stm.Lines[0].MatchKind != "remittance" || stm.Lines[0].MatchRef != rem || stm.Lines[1].MatchKind != "transfer-order" ||
		stm.Lines[1].MatchRef != trf || stm.Lines[2].MatchKind != "" || stm.Lines[2].Amount != "-2.50" {
		t.Fatalf("automatic: %+v", stm)
	}
	stm = tapp.StatementDTO{}
	h.must(h.do("POST", "/api/treasury/statements/"+mayID+"/auto-reconcile", "reconciler", nil, &stm), 200, "automatic again")
	if stm.Matched != 0 || stm.Pending != 1 {
		t.Fatalf("automatic again: %+v", stm)
	}
	reconcile := func(id string, line int, kind, ref, note string) map[string]any {
		return map[string]any{"line": line, "kind": kind, "ref": ref, "note": note}
	}
	path := func(id, action string) string { return "/api/treasury/statements/" + id + "/" + action }
	h.must(h.do("POST", path(mayID, "reconcile"), "reconciler", reconcile(mayID, 3, "gift", "", ""), nil), 400, "kind")
	h.must(h.do("POST", path(mayID, "reconcile"), "reconciler", reconcile(mayID, 3, "other", "", ""), nil), 422, "a note is needed")
	h.must(h.do("POST", path(mayID, "reconcile"), "reconciler", reconcile(mayID, 9, "other", "", "x"), nil), 422, "no such movement")
	h.must(h.do("POST", path(mayID, "reconcile"), "reconciler", reconcile(mayID, 3, "other", "", "Comisión de mantenimiento"), &stm), 200, "the fee")
	h.must(h.do("POST", path(mayID, "reconcile"), "reconciler", reconcile(mayID, 1, "other", "", "x"), nil), 422, "reconciled once")
	if stm.Pending != 0 || stm.Lines[2].MatchNote != "Comisión de mantenimiento" {
		t.Fatalf("May reconciled: %+v", stm)
	}
	// By hand: released, and reconciled again only against what fits.
	h.must(h.do("POST", path(mayID, "release"), "importer", map[string]any{"line": 1}, nil), 403, "importing is not reconciling")
	h.must(h.do("POST", path(mayID, "release"), "reconciler", map[string]any{"line": 1}, &stm), 200, "release")
	h.must(h.do("POST", path(mayID, "release"), "reconciler", map[string]any{"line": 1}, nil), 422, "released once")
	h.must(h.do("POST", path(mayID, "reconcile"), "reconciler", reconcile(mayID, 1, "remittance", trf, ""), nil), 422, "an order is not a remittance")
	h.must(h.do("POST", path(mayID, "reconcile"), "reconciler", reconcile(mayID, 1, "remittance", foreign, ""), nil), 422, "remittance of another account")
	h.must(h.do("POST", path(mayID, "reconcile"), "reconciler", reconcile(mayID, 1, "remittance", late, ""), nil), 422, "another amount")
	h.must(h.do("POST", path(mayID, "reconcile"), "reconciler", reconcile(mayID, 1, "remittance", rem, ""), &stm), 200, "the remittance")
	h.must(h.do("POST", path(jul.ID, "reconcile"), "reconciler", reconcile(jul.ID, 1, "remittance", rem, ""), nil), 422, "already explains a movement of May")

	// June: the remittance of the 20th is too far from the movement of the 3rd for the automatic
	// run, but a person may decide it is that one.
	stm = tapp.StatementDTO{}
	h.must(h.do("POST", path(jun.ID, "auto-reconcile"), "reconciler", nil, &stm), 200, "June automatic")
	if stm.Matched != 0 || stm.Pending != 2 {
		t.Fatalf("June automatic: %+v", stm)
	}
	h.must(h.do("POST", path(jun.ID, "reconcile"), "reconciler", reconcile(jun.ID, 1, "remittance", late, ""), &stm), 200, "June by hand")

	var page fw.Page[tapp.StatementDTO]
	h.must(h.do("GET", "/api/treasury/statements?owner="+acme, "viewer", nil, &page), 200, "statements")
	if len(page.Items) != 3 || page.Items[0].ID != mayID || page.Items[0].Pending != 0 || page.Items[1].Pending != 1 || page.Items[2].ID != jul.ID {
		t.Fatalf("statements: %+v", page.Items)
	}
	h.must(h.do("GET", "/api/treasury/statements?account="+main.ID+"&pending=true", "viewer", nil, &page), 200, "pending")
	if len(page.Items) != 2 || page.Items[0].ID != jun.ID {
		t.Fatalf("pending: %+v", page.Items)
	}
	h.must(h.do("GET", "/api/treasury/statements?account="+second.ID, "viewer", nil, &page), 200, "the other account")
	if len(page.Items) != 0 {
		t.Fatalf("the other account: %+v", page.Items)
	}
	h.must(h.do("GET", "/api/treasury/statements/"+mayID, "outsider", nil, nil), 404, "outsider")
	h.must(h.do("GET", "/api/treasury/statements/"+mayID, "viewer", nil, &stm), 200, "get")
	if len(stm.Lines) != 3 || stm.Lines[0].Concept != "ADEUDOS SEPA REMESA" || stm.Lines[0].Date != "2026-05-05" || stm.Lines[0].MatchRef != rem ||
		stm.Lines[2].Concept != "COMISIÓN DE MANTENIMIENTO" {
		t.Fatalf("May: %+v", stm)
	}
	h.must(h.do("GET", "/api/treasury/statements", "outsider", nil, &page), 200, "outsider's statements")
	if len(page.Items) != 0 {
		t.Fatalf("outsider: %+v", page.Items)
	}
}

func TestTreasury_ReconcilesBankStatements_MemoryThenSQLite(t *testing.T) {
	h, world := bank(t)
	h.statements("m", world)

	ctx := context.Background()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "host.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{tinfra.Migrations()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.sw.Swap(ctx, db); err != nil {
		t.Fatal(err)
	}
	h.statements("s", world)
}
