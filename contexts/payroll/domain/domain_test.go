package domain_test

import (
	"errors"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/payroll/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

func isViolation(err error, code string) bool {
	var rv *fw.RuleViolationError
	return errors.As(err, &rv) && rv.Code == code
}

func concepts() map[string]domain.Concept {
	out := map[string]domain.Concept{}
	for _, c := range domain.WellKnownConcepts() {
		out[c.Code] = c
	}
	return out
}

func dec(s string) vocab.Decimal { return vocab.MustDecimal(s) }

func TestConcepts(t *testing.T) {
	ids := map[domain.ConceptID]bool{}
	for _, c := range domain.WellKnownConcepts() {
		if err := c.Validate(); err != nil || ids[c.ID] {
			t.Fatalf("%s: %v (duplicate %v)", c.Code, err, ids[c.ID])
		}
		ids[c.ID] = true
	}
	bad := domain.Concept{Code: "X", Name: "X", Kind: domain.KindIncomeTax, Taxable: true}
	if bad.Validate() == nil {
		t.Fatal("only earnings count in the bases")
	}
}

func TestLines(t *testing.T) {
	c := concepts()
	l, err := domain.NewLine(c["HORAS_EXTRA"], domain.LineInput{Quantity: dec("3.5"), UnitAmount: dec("18.333")})
	if err != nil || !l.Amount.Equal(dec("64.17")) || l.Description != "Horas extraordinarias" {
		t.Fatalf("quantity × unit, rounded: %v %v", l.Amount, err)
	}
	l, err = domain.NewLine(c["IRPF_GEN"], domain.LineInput{Base: dec("2105.25"), Percent: dec("15")})
	if err != nil || !l.Amount.Equal(dec("315.79")) {
		t.Fatalf("base × percent, half away from zero: %v %v", l.Amount, err)
	}
	if _, err := domain.NewLine(c["IRPF_GEN"], domain.LineInput{Base: dec("100"), Percent: dec("15"), Amount: dec("20")}); err == nil {
		t.Fatal("the amount of a rate line is computed")
	}
	if _, err := domain.NewLine(c["SALARIO_BASE"], domain.LineInput{Amount: dec("0")}); err == nil {
		t.Fatal("an earning needs an amount")
	}
	if _, err := domain.NewLine(c["INFO_BASE_CC"], domain.LineInput{Amount: dec("0")}); err != nil {
		t.Fatalf("an information line may be zero: %v", err)
	}
	if _, err := domain.NewLine(c["SALARIO_BASE"], domain.LineInput{Amount: dec("10.005")}); err == nil {
		t.Fatal("amounts are in cents")
	}
}

func TestPayslip(t *testing.T) {
	c := concepts()
	st := domain.PayslipState{Employment: domain.EmploymentID{UUID: fw.NewUUID()}, Person: domain.PersonID{UUID: fw.NewUUID()},
		Employer: domain.OrganizationID{UUID: fw.NewUUID()}, Kind: domain.Ordinary, Start: vocab.MustDate(2026, 9, 1),
		End: vocab.MustDate(2026, 9, 30), PaymentDate: vocab.MustDate(2026, 9, 30)}
	long := st
	long.End = vocab.MustDate(2026, 10, 1)
	if _, err := domain.DraftPayslip(domain.NewPayslipID(), long); err == nil {
		t.Fatal("a period of at most one month")
	}
	p, err := domain.DraftPayslip(domain.NewPayslipID(), st)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Approve(); !isViolation(err, "payroll.no_earnings") {
		t.Fatalf("no earnings: %v", err)
	}
	add := func(code string, in domain.LineInput) domain.LineID {
		l, err := domain.NewLine(c[code], in)
		if err != nil {
			t.Fatal(err)
		}
		id, err := p.AddLine(l)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	add("SALARIO_BASE", domain.LineInput{Amount: dec("2000")})
	add("PLUS_CONVENIO", domain.LineInput{Amount: dec("105.25")})
	add("SS_EMP_CG", domain.LineInput{Base: dec("2105.25"), Percent: dec("4.70")})
	add("SS_EMP_DES", domain.LineInput{Base: dec("2105.25"), Percent: dec("1.55")})
	add("IRPF_GEN", domain.LineInput{Base: dec("2105.25"), Percent: dec("15")})
	add("SS_ER_CC", domain.LineInput{Base: dec("2105.25"), Percent: dec("23.60")})
	info := add("INFO_BASE_CC", domain.LineInput{Amount: dec("2105.25")})
	tot := p.Totals()
	// 2105.25 − 98.95 − 32.63 − 315.79 = 1657.88; company cost 2105.25 + 496.84.
	if !tot.Gross.Equal(dec("2105.25")) || !tot.SocialSecurity.Equal(dec("131.58")) || !tot.IncomeTax.Equal(dec("315.79")) ||
		!tot.Net.Equal(dec("1657.88")) || !tot.EmployerCost.Equal(dec("496.84")) || !tot.CompanyCost.Equal(dec("2602.09")) ||
		!tot.TaxableBase.Equal(dec("2105.25")) {
		t.Fatalf("totals: %+v", tot)
	}
	if err := p.RemoveLine(info); err != nil {
		t.Fatal(err)
	}
	if err := p.Approve(); err != nil {
		t.Fatal(err)
	}
	l, _ := domain.NewLine(c["SALARIO_BASE"], domain.LineInput{Amount: dec("1")})
	if _, err := p.AddLine(l); !isViolation(err, "payroll.payslip_not_draft") {
		t.Fatal("an approved payslip is immutable")
	}
	if p.Discard() == nil {
		t.Fatal("an approved payslip is cancelled, not discarded")
	}
	if err := p.Cancel("importe erróneo"); err != nil || p.Status() != domain.Cancelled {
		t.Fatal(err)
	}

	q, _ := domain.DraftPayslip(domain.NewPayslipID(), st)
	s1, _ := domain.NewLine(c["SALARIO_BASE"], domain.LineInput{Amount: dec("100")})
	s2, _ := domain.NewLine(c["EMBARGO"], domain.LineInput{Amount: dec("150")})
	_, _ = q.AddLine(s1)
	_, _ = q.AddLine(s2)
	if err := q.Approve(); !isViolation(err, "payroll.negative_net") {
		t.Fatalf("negative net: %v", err)
	}
}

func TestProfileSplits(t *testing.T) {
	p, err := domain.OpenProfile(domain.NewProfileID(), domain.ProfileState{Employment: domain.EmploymentID{UUID: fw.NewUUID()},
		Person: domain.PersonID{UUID: fw.NewUUID()}, Employer: domain.OrganizationID{UUID: fw.NewUUID()}, ContributionGroup: 5,
		IncomeTaxRate: dec("15"), Salary: domain.Salary{Amount: dec("2000"), Periodicity: domain.Monthly, PaymentsPerYear: 14}})
	if err != nil || !p.Terms().Salary.Annual().Equal(dec("28000")) {
		t.Fatal(err)
	}
	if err := p.SetTerms(domain.Terms{ContributionGroup: 12}); err == nil {
		t.Fatal("contribution group 1 to 11")
	}
	from := vocab.MustDate(2026, 1, 1)
	a := mustIBAN("ES9121000418450200051332")
	b := mustIBAN("ES7921000813610123456789")
	g := mustIBAN("ES6621000418401234567891")
	if _, err := p.AddSplit(domain.Split{IBAN: a, Percent: dec("10"), Amount: dec("5"), From: from}); err == nil {
		t.Fatal("one mode per split")
	}
	if _, err := p.AddSplit(domain.Split{IBAN: a, Residual: true, From: from}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.AddSplit(domain.Split{IBAN: b, Residual: true, From: vocab.MustDate(2026, 6, 1)}); !isViolation(err, "payroll.two_residuals") {
		t.Fatalf("one residual at a time: %v", err)
	}
	if _, err := p.AddSplit(domain.Split{IBAN: b, Percent: dec("30"), Priority: 1, From: from}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.AddSplit(domain.Split{IBAN: b, Percent: dec("80"), From: vocab.MustDate(2026, 3, 1)}); !isViolation(err, "payroll.split_over_100") {
		t.Fatalf("over 100: %v", err)
	}
	if _, err := p.AddSplit(domain.Split{IBAN: g, Amount: dec("200"), Garnishment: true, From: from}); err != nil {
		t.Fatal(err)
	}
	pays, err := p.Distribute(dec("1657.88"), vocab.MustDate(2026, 9, 30))
	if err != nil || len(pays) != 3 {
		t.Fatalf("distribute: %+v %v", pays, err)
	}
	// Garnishment first (200), then 30 % of the net (497.36), the residual the rest (960.52).
	if pays[0].IBAN != g || !pays[0].Amount.Equal(dec("200")) || !pays[1].Amount.Equal(dec("497.36")) || pays[2].IBAN != a ||
		!pays[2].Amount.Equal(dec("960.52")) {
		t.Fatalf("payments: %+v", pays)
	}
	for _, s := range p.Splits() {
		if s.Residual {
			_ = p.EndSplit(s.ID, vocab.MustDate(2026, 8, 31))
		}
	}
	if _, err := p.Distribute(dec("1000"), vocab.MustDate(2026, 9, 30)); !isViolation(err, "payroll.no_residual_account") {
		t.Fatalf("money without account: %v", err)
	}
}

func TestEmployerAccount(t *testing.T) {
	if !domain.ValidCCC("28123456742") || domain.ValidCCC("28123456743") || domain.ValidCCC("2812345674") {
		t.Fatal("CCC control digits")
	}
	st := domain.EmployerAccountState{Employer: domain.OrganizationID{UUID: fw.NewUUID()}, Code: "28/1234567/42", Regime: "0111",
		Method: domain.DirectDebit}
	if _, err := domain.RegisterEmployerAccount(domain.NewEmployerAccountID(), st); err == nil {
		t.Fatal("a direct debit needs a bank account")
	}
	st.IBAN = mustIBAN("ES9121000418450200051332")
	a, err := domain.RegisterEmployerAccount(domain.NewEmployerAccountID(), st)
	if err != nil || a.Code() != "28123456742" || !a.IsActive() {
		t.Fatal(err)
	}
	a.Deactivate()
	if a.IsActive() {
		t.Fatal("baja")
	}
}

func mustIBAN(s string) vocab.IBAN {
	i, err := vocab.NewIBAN(s)
	if err != nil {
		panic(err)
	}
	return i
}
