package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/fiscal/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

func isViolation(err error, code string) bool {
	var rv *fw.RuleViolationError
	return errors.As(err, &rv) && rv.Code == code
}

func dec(s string) vocab.Decimal { return vocab.MustDecimal(s) }

func TestTaxRates(t *testing.T) {
	st := domain.TaxRateState{Type: domain.IGIC, Territory: domain.Common, Code: "G", Description: "General", Rate: dec("7"),
		From: vocab.MustDate(2020, 1, 1)}
	if _, err := domain.ReconstituteTaxRate(domain.NewTaxRateID(), st); err == nil {
		t.Fatal("IGIC is levied only in the Canaries")
	}
	st.Territory = domain.Canaries
	st.Surcharge = dec("1")
	if _, err := domain.ReconstituteTaxRate(domain.NewTaxRateID(), st); err == nil {
		t.Fatal("the equivalence surcharge exists only in VAT")
	}
	vat := domain.TaxRateState{Type: domain.VAT, Territory: domain.Common, Code: " g21 ", Description: "General", Rate: dec("21"),
		Surcharge: dec("5.2"), From: vocab.MustDate(2012, 9, 1)}
	r, err := domain.ReconstituteTaxRate(domain.NewTaxRateID(), vat)
	if err != nil || r.State().Code != "G21" || !r.InForceOn(vocab.MustDate(2026, 1, 1)) {
		t.Fatal(err)
	}
	if err := r.End(vocab.MustDate(2011, 1, 1)); !isViolation(err, "fiscal.rate_end_before_start") {
		t.Fatal(err)
	}
	next := vat
	next.Code, next.From = "G21", vocab.MustDate(2027, 1, 1)
	if !r.Overlaps(next) {
		t.Fatal("an open rate overlaps a later one")
	}
	_ = r.End(vocab.MustDate(2026, 12, 31))
	if r.Overlaps(next) {
		t.Fatal("ended before the next one")
	}
	if _, err := domain.ReconstituteTreatment(domain.NewTreatmentID(), domain.TreatmentState{Territory: domain.Common, Code: "E1",
		Description: "Exenta art. 20", Kind: domain.Exempt, Active: true}); err != nil {
		t.Fatal(err)
	}
}

func TestTaxpayer(t *testing.T) {
	tp, err := domain.RegisterTaxpayer(domain.NewTaxpayerID(), domain.OrganizationID{UUID: fw.NewUUID()},
		domain.TaxpayerTerms{Territory: domain.Common, FiscalYearStartMonth: 7, GeneralProrata: dec("85")})
	if err != nil {
		t.Fatal(err)
	}
	if tp.FiscalYearOf(vocab.MustDate(2026, 3, 1)) != 2025 || tp.FiscalYearOf(vocab.MustDate(2026, 7, 1)) != 2026 {
		t.Fatal("fiscal year starting in July")
	}
	if _, err := tp.AddActivity(domain.Activity{Code: "A1", Category: domain.Services, Description: "Consultoría",
		From: vocab.MustDate(2026, 1, 1), Until: vocab.MustDate(2025, 1, 1)}); err == nil {
		t.Fatal("from ≤ until")
	}
	first, _ := tp.AddActivity(domain.Activity{Code: "A1", IAE: "843", Category: domain.Services, Description: "Consultoría",
		From: vocab.MustDate(2020, 1, 1), Primary: true})
	second, _ := tp.AddActivity(domain.Activity{Code: "A2", Category: domain.Trade, Description: "Comercio", From: vocab.MustDate(2024, 1, 1), Primary: true})
	if a, ok := tp.PrimaryActivityOn(vocab.MustDate(2026, 1, 1)); !ok || a.ID != second || first == second {
		t.Fatal("one primary activity")
	}
	if _, err := tp.AddObligation(domain.Obligation{Form: "190", Periodicity: domain.Quarterly, FromYear: 2020}); err == nil {
		t.Fatal("the 190 is annual")
	}
	if _, err := tp.AddObligation(domain.Obligation{Form: "999", Periodicity: domain.Annual, FromYear: 2020}); err == nil {
		t.Fatal("unknown form")
	}
	id, err := tp.AddObligation(domain.Obligation{Form: "111", Periodicity: domain.Quarterly, FromYear: 2020})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tp.AddObligation(domain.Obligation{Form: "111", Periodicity: domain.Monthly, FromYear: 2026}); !isViolation(err, "fiscal.obligation_overlaps") {
		t.Fatal("one obligation per form and year")
	}
	_ = tp.EndObligation(id, 2025)
	if _, err := tp.AddObligation(domain.Obligation{Form: "111", Periodicity: domain.Monthly, FromYear: 2026}); err != nil {
		t.Fatalf("large company from 2026: %v", err)
	}
	if o, ok := tp.ObligationFor("111", 2026); !ok || o.Periodicity != domain.Monthly {
		t.Fatal("monthly 111 in 2026")
	}
}

func TestPeriods(t *testing.T) {
	for code, want := range map[string][2]string{"2T": {"2026-04-01", "2026-06-30"}, "02": {"2026-02-01", "2026-02-28"}, "0A": {"2026-01-01", "2026-12-31"}} {
		p, ok := domain.ParsePeriod(code)
		from, to := p.Bounds(2026)
		if !ok || p.String() != code || from.String() != want[0] || to.String() != want[1] {
			t.Fatalf("%s: %v %v %v", code, ok, from, to)
		}
	}
	for _, bad := range []string{"5T", "13", "00", "1A", ""} {
		if _, ok := domain.ParsePeriod(bad); ok {
			t.Fatalf("%q is not a period", bad)
		}
	}
}

func TestFiling(t *testing.T) {
	payer := domain.OrganizationID{UUID: fw.NewUUID()}
	ana, bea := domain.PartyID{UUID: fw.NewUUID()}, domain.PartyID{UUID: fw.NewUUID()}
	w := func(p domain.PartyID, paid string, base, withheld string, cancelled bool) *domain.Withholding {
		d, _ := vocab.ParseDate(paid)
		x, err := domain.ReconstituteWithholding(domain.WithholdingID{UUID: fw.NewUUID()}, domain.WithholdingState{Payer: payer, Recipient: p,
			PaymentDate: d, Key: "A", Perceptions: dec(base), Withheld: dec(withheld), Cancelled: cancelled})
		if err != nil {
			t.Fatal(err)
		}
		return x
	}
	if _, err := domain.ReconstituteWithholding(domain.WithholdingID{UUID: fw.NewUUID()}, domain.WithholdingState{Payer: payer, Recipient: ana,
		PaymentDate: vocab.MustDate(2026, 1, 31), Key: "A", Perceptions: dec("10"), Withheld: dec("11")}); err == nil {
		t.Fatal("the withholding cannot exceed its base")
	}
	ws := []*domain.Withholding{w(ana, "2026-01-31", "2105.25", "315.79", false), w(ana, "2026-02-28", "2105.25", "315.79", false),
		w(bea, "2026-02-28", "1500", "150", false), w(bea, "2026-03-31", "1500", "150", true)}
	ids := map[domain.PartyID]domain.Identity{ana: {NIF: "12345678Z", Name: "GARCIA ANA", Province: "28"}, bea: {NIF: "00000000X", Name: "LOPEZ BEA"}}
	rs := domain.Summarize(ws, ids)
	tot := domain.TotalsOf(rs)
	if len(rs) != 2 || tot.Recipients != 2 || !tot.Perceptions.Equal(dec("5710.50")) || !tot.Withheld.Equal(dec("781.58")) {
		t.Fatalf("summary: %+v %+v", rs, tot)
	}
	p, _ := domain.ParsePeriod("0A")
	if _, err := domain.DraftFiling(domain.NewFilingID(), payer, "303", 2026, p, domain.Identity{}, nil); err == nil {
		t.Fatal("only 111 and 190 are generated")
	}
	q, _ := domain.ParsePeriod("1T")
	if _, err := domain.DraftFiling(domain.NewFilingID(), payer, "190", 2026, q, domain.Identity{}, nil); err == nil {
		t.Fatal("the 190 is annual")
	}
	f, err := domain.DraftFiling(domain.NewFilingID(), payer, "190", 2026, p, domain.Identity{NIF: "A58818501", Name: "ACME SA"}, rs)
	if err != nil {
		t.Fatal(err)
	}
	if probs := f.Problems(); len(probs) != 2 {
		t.Fatalf("bea: wrong NIF control letter and no province: %v", probs)
	}
	if err := f.Submit(1, "CSV123", time.Now()); !isViolation(err, "fiscal.filing_incomplete") {
		t.Fatalf("incomplete: %v", err)
	}
	ids[bea] = domain.Identity{NIF: "00000000T", Name: "LOPEZ BEA", Province: "08"}
	if err := f.Regenerate(domain.Identity{NIF: "A58818501", Name: "ACME SA"}, domain.Summarize(ws, ids)); err != nil {
		t.Fatal(err)
	}
	if err := f.Submit(1, "CSV123", time.Now()); err != nil || f.State().Status != domain.StatusSubmitted || f.State().Number != 1 {
		t.Fatal(err)
	}
	if f.Regenerate(domain.Identity{}, nil) == nil || f.Discard() == nil {
		t.Fatal("a submitted filing is frozen")
	}
	if err := f.Revert("error en perceptor"); err != nil || f.State().Status != domain.StatusReverted {
		t.Fatal(err)
	}
	c, _ := domain.ReconstituteCounter(domain.NewCounterID(), payer, "190", 2026, 0)
	if c.Next() != 1 || c.Next() != 2 {
		t.Fatal("counter")
	}
}
