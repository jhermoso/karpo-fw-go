package domain_test

import (
	"errors"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/assets/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

func isViolation(err error, code string) bool {
	var rv *fw.RuleViolationError
	return errors.As(err, &rv) && rv.Code == code
}

func dec(s string) vocab.Decimal { return vocab.MustDecimal(s) }

func date(s string) vocab.Date {
	d, err := vocab.ParseDate(s)
	if err != nil {
		panic(err)
	}
	return d
}

var company = domain.OrganizationID{UUID: fw.NewUUID()}

// A van of 12,000 with 2,000 of residual value over 48 months, in service on 16 January 2026:
// 208.33 a month, 107.53 the first one (16 of 31 days).
func van() domain.AssetState {
	return domain.AssetState{Company: company, Code: " fur-01 ", Class: domain.Vehicles, Details: domain.Details{Name: "Furgoneta", Serial: "1234-ABC"},
		Acquired: date("2026-01-10"), InService: date("2026-01-16"), Cost: dec("12000"), Residual: dec("2000"), LifeMonths: 48}
}

func TestAsset_Rules(t *testing.T) {
	bad := []func(*domain.AssetState){
		func(s *domain.AssetState) { s.Code = "FUR 01" },
		func(s *domain.AssetState) { s.Class = "boats" },
		func(s *domain.AssetState) { s.Name = " " },
		func(s *domain.AssetState) { s.InService = date("2026-01-09") },
		func(s *domain.AssetState) { s.Cost = dec("0") },
		func(s *domain.AssetState) { s.Residual = dec("12000") },
		func(s *domain.AssetState) { s.LifeMonths = 0 },
		func(s *domain.AssetState) { s.Class = domain.Land },
	}
	for i, f := range bad {
		s := van()
		f(&s)
		if _, err := domain.RegisterAsset(domain.NewAssetID(), s); !errors.Is(err, fw.ErrValidation) {
			t.Fatalf("case %d: %v", i, err)
		}
	}
	land := van()
	land.Class, land.LifeMonths, land.Residual = domain.Land, 0, dec("0")
	l, err := domain.RegisterAsset(domain.NewAssetID(), land)
	if err != nil {
		t.Fatal(err)
	}
	if c, err := l.DepreciateThrough(domain.Period{Year: 2030, Month: 12}); err != nil || len(c) != 0 || !l.NetBookValue().Equal(dec("12000")) {
		t.Fatalf("land does not depreciate: %+v %v", c, err)
	}
}

func TestAsset_StraightLine(t *testing.T) {
	a, err := domain.RegisterAsset(domain.NewAssetID(), van())
	if err != nil {
		t.Fatal(err)
	}
	if a.State().Code != "FUR-01" || !a.Monthly().Equal(dec("208.33")) || !a.Depreciable().Equal(dec("10000")) {
		t.Fatalf("asset: %+v monthly %s", a.State(), a.Monthly())
	}
	if _, err := a.DepreciateThrough(domain.Period{Year: 2026, Month: 13}); !isViolation(err, "assets.period") {
		t.Fatalf("period: %v", err)
	}
	if c, err := a.DepreciateThrough(domain.Period{Year: 2025, Month: 12}); err != nil || len(c) != 0 {
		t.Fatalf("before service: %+v %v", c, err)
	}
	c, err := a.DepreciateThrough(domain.Period{Year: 2026, Month: 3})
	if err != nil || len(c) != 3 || !c[0].Amount.Equal(dec("107.53")) || !c[1].Amount.Equal(dec("208.33")) || c[2].Period.String() != "2026-03" {
		t.Fatalf("first quarter: %+v %v", c, err)
	}
	if c, err := a.DepreciateThrough(domain.Period{Year: 2026, Month: 3}); err != nil || len(c) != 0 {
		t.Fatalf("a month is charged once: %+v %v", c, err)
	}
	if !a.Accumulated().Equal(dec("524.19")) || !a.NetBookValue().Equal(dec("11475.81")) || a.Status() != "in-service" {
		t.Fatalf("after the quarter: %s %s", a.Accumulated(), a.NetBookValue())
	}
	// To the end of its life and beyond: exactly the depreciable amount, the last charge takes the rest.
	c, err = a.DepreciateThrough(domain.Period{Year: 2031, Month: 12})
	if err != nil || !a.Accumulated().Equal(dec("10000")) || !a.NetBookValue().Equal(dec("2000")) || a.Status() != "depreciated" {
		t.Fatalf("whole life: %s %v", a.Accumulated(), err)
	}
	last := c[len(c)-1]
	// 107.53 + 47 × 208.33 = 9899.04; the 49th month charges the 100.96 left.
	if last.Period.String() != "2030-01" || !last.Amount.Equal(dec("100.96")) || len(a.State().Charges) != 49 {
		t.Fatalf("last charge: %+v of %d", last, len(a.State().Charges))
	}
	if err := a.Change(domain.Details{Name: ""}); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("name: %v", err)
	}
	if err := a.Change(domain.Details{Name: "Furgoneta de reparto"}); err != nil || a.State().Name != "Furgoneta de reparto" || !a.State().Cost.Equal(dec("12000")) {
		t.Fatalf("change: %v", err)
	}
}

func TestAsset_Disposal(t *testing.T) {
	a, _ := domain.RegisterAsset(domain.NewAssetID(), van())
	if err := a.Dispose(date("2026-01-15"), domain.Sale, dec("1")); !isViolation(err, "assets.disposal_date") {
		t.Fatalf("before service: %v", err)
	}
	if err := a.Dispose(date("2026-07-10"), "gift", dec("0")); !isViolation(err, "assets.disposal_kind") {
		t.Fatalf("kind: %v", err)
	}
	if err := a.Dispose(date("2026-07-10"), domain.Scrap, dec("1")); !isViolation(err, "assets.proceeds") {
		t.Fatalf("scrap with proceeds: %v", err)
	}
	if err := a.Dispose(date("2026-07-10"), domain.Sale, dec("0")); !isViolation(err, "assets.proceeds") {
		t.Fatalf("sale without proceeds: %v", err)
	}
	// Sold on 10 July for 11,000: depreciated through June (107.53 + 5 × 208.33 = 1149.18),
	// net book value 10,850.82, a gain of 149.18.
	if err := a.Dispose(date("2026-07-10"), domain.Sale, dec("11000")); err != nil {
		t.Fatal(err)
	}
	if !a.Accumulated().Equal(dec("1149.18")) || !a.Result().Equal(dec("149.18")) || a.Status() != "disposed" || len(a.State().Charges) != 6 {
		t.Fatalf("sold: %s %s", a.Accumulated(), a.Result())
	}
	if err := a.Dispose(date("2026-08-01"), domain.Scrap, dec("0")); !isViolation(err, "assets.disposed") {
		t.Fatalf("twice: %v", err)
	}
	if c, err := a.DepreciateThrough(domain.Period{Year: 2027, Month: 1}); err != nil || len(c) != 0 {
		t.Fatalf("a disposed asset does not depreciate: %+v %v", c, err)
	}

	b, _ := domain.RegisterAsset(domain.NewAssetID(), van())
	_, _ = b.DepreciateThrough(domain.Period{Year: 2026, Month: 7})
	if err := b.Dispose(date("2026-07-10"), domain.Scrap, dec("0")); !isViolation(err, "assets.disposal_date") {
		t.Fatalf("already depreciated through the month: %v", err)
	}
	if err := b.Dispose(date("2026-08-02"), domain.Scrap, dec("0")); err != nil || !b.Result().Equal(dec("-10642.49")) {
		t.Fatalf("scrapped: %s %v", b.Result(), err)
	}
}
