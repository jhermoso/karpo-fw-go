package domain_test

import (
	"errors"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/accounting/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

func isViolation(err error, code string) bool {
	var rv *fw.RuleViolationError
	return errors.As(err, &rv) && rv.Code == code
}

func dec(s string) vocab.Decimal { return vocab.MustDecimal(s) }

func TestAccounts(t *testing.T) {
	for code, ok := range map[string]bool{"43000000": true, "7": true, "0430": false, "43A": false, "": false, "1234567890123": false} {
		if domain.ValidCode(code) != ok {
			t.Fatalf("code %q", code)
		}
	}
	if domain.NatureOf("70000000") != domain.IncomeStatement || domain.NatureOf("43000000") != domain.BalanceSheet || domain.NatureOf("800") != domain.EquityIncome {
		t.Fatal("nature by group")
	}
	if _, err := domain.ReconstituteAccount(domain.NewAccountID(), domain.AccountState{Company: domain.OrganizationID{UUID: fw.NewUUID()},
		Code: "4300 0000", Name: "Clientes"}); err == nil {
		t.Fatal("digits only")
	}
}

func TestLedger(t *testing.T) {
	company := domain.OrganizationID{UUID: fw.NewUUID()}
	if _, err := domain.ReconstituteLedger(domain.NewLedgerID(), domain.LedgerState{Company: company, StartMonth: 1,
		Accounts: map[domain.Role]string{"nope": "430"}}); err == nil {
		t.Fatal("unknown role")
	}
	l, err := domain.ReconstituteLedger(domain.NewLedgerID(), domain.LedgerState{Company: company, StartMonth: 7,
		Accounts: map[domain.Role]string{domain.RoleOutputTax: "47700000"}, TaxCodes: map[string]string{"R10": "47700010"}})
	if err != nil {
		t.Fatal(err)
	}
	if p := l.PeriodOf(vocab.MustDate(2026, 3, 15)); p.Year != 2025 || p.Month != 9 {
		t.Fatalf("March in a year starting in July: %+v", p)
	}
	if p := l.PeriodOf(vocab.MustDate(2026, 7, 1)); p.Year != 2026 || p.Month != 1 {
		t.Fatalf("July: %+v", p)
	}
	if from, to := l.Bounds(2026); from.String() != "2026-07-01" || to.String() != "2027-06-30" {
		t.Fatal("bounds")
	}
	if c, _ := l.TaxAccount("r10"); c != "47700010" {
		t.Fatal("tax override")
	}
	if c, _ := l.TaxAccount("G21"); c != "47700000" {
		t.Fatal("output tax role")
	}
	if _, err := l.AccountFor(domain.RoleBank); !isViolation(err, "accounting.role_undefined") {
		t.Fatal("undefined role")
	}
	_ = l.Close(domain.Period{Year: 2025, Month: 9})
	if !l.IsClosed(l.PeriodOf(vocab.MustDate(2026, 3, 1))) {
		t.Fatal("closed")
	}
	if _, err := domain.Post(domain.NewEntryID(), domain.Draft{Company: company, Date: vocab.MustDate(2026, 3, 1), Description: "X",
		Lines: []domain.Line{{Account: "572", Debit: dec("1")}, {Account: "430", Credit: dec("1")}}}, l, 1); !isViolation(err, "accounting.period_closed") {
		t.Fatalf("closed period: %v", err)
	}
	l.Reopen(domain.Period{Year: 2025, Month: 9})
	if l.IsClosed(domain.Period{Year: 2025, Month: 9}) {
		t.Fatal("reopened")
	}
}

func TestEntry(t *testing.T) {
	company := domain.OrganizationID{UUID: fw.NewUUID()}
	l, _ := domain.ReconstituteLedger(domain.NewLedgerID(), domain.LedgerState{Company: company, StartMonth: 1})
	d := domain.Draft{Company: company, Date: vocab.MustDate(2026, 9, 28), Description: "Factura FA-1"}
	d.Lines = []domain.Line{{Account: "43000000", Debit: dec("121")}, {Account: "70000000", Credit: dec("100")}, {Account: "47700000", Credit: dec("20")}}
	if _, err := domain.Post(domain.NewEntryID(), d, l, 1); err == nil {
		t.Fatal("unbalanced")
	}
	d.Lines[2].Credit = dec("21")
	d.Lines = append(d.Lines, domain.Line{Account: "47700001", Credit: dec("0")})
	e, err := domain.Post(domain.NewEntryID(), d, l, 7)
	if err != nil || len(e.State().Lines) != 3 || !e.Total().Equal(dec("121")) || e.State().Number != 7 || e.State().Month != 9 {
		t.Fatalf("zero lines dropped: %v %+v", err, e)
	}
	// A corrective invoice: signed amounts turn into the opposite side.
	neg := domain.Normalize([]domain.Line{{Account: "43000000", Debit: dec("-36.30")}, {Account: "70000000", Credit: dec("-30")}})
	if !neg[0].Credit.Equal(dec("36.30")) || !neg[0].Debit.IsZero() || !neg[1].Debit.Equal(dec("30")) {
		t.Fatalf("normalize: %+v", neg)
	}
	rev, err := e.Reversal(vocab.MustDate(2026, 10, 1), domain.Source{Type: "manual"})
	if err != nil || !rev.Lines[0].Credit.Equal(dec("121")) {
		t.Fatal(err)
	}
	r, _ := domain.Post(domain.NewEntryID(), rev, l, 8)
	r.LinkReversal(e.ID())
	_ = e.MarkReversed(r.ID())
	if _, err := e.Reversal(vocab.MustDate(2026, 10, 1), domain.Source{}); !isViolation(err, "accounting.already_reversed") {
		t.Fatal("reversed once")
	}
	if _, err := r.Reversal(vocab.MustDate(2026, 10, 1), domain.Source{}); !isViolation(err, "accounting.reversal_of_reversal") {
		t.Fatal("no reversal of a reversal")
	}
	c, _ := domain.ReconstituteCounter(domain.NewCounterID(), company, 2026, 0)
	if c.Next() != 1 || c.Next() != 2 {
		t.Fatal("counter")
	}
}
