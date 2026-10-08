package domain_test

import (
	"errors"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/products/domain"
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

func good() domain.Details {
	return domain.Details{Name: " Tornillo M8 ", Kind: domain.Good, UoM: "ea", TaxCode: "g21", BasePrice: dec("0.1250"), ForSale: true, ForPurchase: true,
		Stocked: true, Barcodes: []domain.Barcode{{Type: "ean-13", Value: "8412345678905"}}}
}

func TestGTIN(t *testing.T) {
	for code, want := range map[string]bool{"8412345678905": true, "8412345678906": false, "96385074": true, "036000291452": true, "84123A5678905": false, "123": false} {
		if domain.ValidGTIN(code) != want {
			t.Fatalf("%s: want %v", code, want)
		}
	}
}

func TestProduct_Rules(t *testing.T) {
	bad := []func(*domain.Details){
		func(d *domain.Details) { d.Name = " " },
		func(d *domain.Details) { d.UoM = "docena" },
		func(d *domain.Details) { d.BasePrice = dec("-1") },
		func(d *domain.Details) { d.BasePrice = dec("1.00001") },
		func(d *domain.Details) { d.Kind = domain.Service },                                 // a stocked service
		func(d *domain.Details) { d.Stocked, d.Tracking = false, domain.TrackLot },          // tracked without stock
		func(d *domain.Details) { d.Barcodes[0].Value = "8412345678906" },                   // check digit
		func(d *domain.Details) { d.Barcodes = append(d.Barcodes, d.Barcodes[0]) },          // twice
		func(d *domain.Details) { d.Barcodes = []domain.Barcode{{Type: "qr", Value: "x"}} }, // unknown type
		func(d *domain.Details) { d.Components = []domain.Component{{Quantity: dec("1")}} }, // no product
		func(d *domain.Details) { d.Suppliers = []domain.SupplierItem{{LeadDays: 3}} },      // no supplier
	}
	for i, f := range bad {
		d := good()
		f(&d)
		if _, err := domain.RegisterProduct(domain.NewProductID(), company, "TOR-M8", d); !errors.Is(err, fw.ErrValidation) {
			t.Fatalf("case %d: %v", i, err)
		}
	}
	if _, err := domain.RegisterProduct(domain.NewProductID(), company, "tor m8", good()); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("sku with a space: %v", err)
	}
	p, err := domain.RegisterProduct(domain.NewProductID(), company, " tor-m8 ", good())
	if err != nil {
		t.Fatal(err)
	}
	if s := p.State(); s.SKU != "TOR-M8" || s.Name != "Tornillo M8" || s.TaxCode != "G21" || s.Tracking != domain.TrackNone {
		t.Fatalf("normalised: %+v", s)
	}

	d := good()
	d.Components = []domain.Component{{Product: p.ID(), Quantity: dec("2")}}
	if err := p.Change(d); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("a kit of itself: %v", err)
	}
	a, b := domain.PartyID{UUID: fw.NewUUID()}, domain.PartyID{UUID: fw.NewUUID()}
	d = good()
	d.Suppliers = []domain.SupplierItem{{Supplier: a, Preferred: true}, {Supplier: b, Preferred: true}}
	if err := p.Change(d); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("two preferred suppliers: %v", err)
	}
	d = good()
	d.Kind, d.Stocked = domain.Service, false
	if err := p.Change(d); !isViolation(err, "products.kind_fixed") {
		t.Fatalf("kind: %v", err)
	}
	d = good()
	d.Tracking, d.Suppliers = domain.TrackLot, []domain.SupplierItem{{Supplier: a, Code: "M8-ZN", LeadDays: 7, Preferred: true}, {Supplier: b}}
	if err := p.Change(d); err != nil || p.State().Tracking != domain.TrackLot || len(p.State().Suppliers) != 2 {
		t.Fatalf("change: %v", err)
	}
	if !p.Active(date("2026-10-04")) {
		t.Fatal("active")
	}
	if err := p.Discontinue(date("2026-11-01")); err != nil || !p.Active(date("2026-10-31")) || p.Active(date("2026-11-01")) {
		t.Fatalf("discontinue: %v", err)
	}
	if err := p.Change(good()); !isViolation(err, "products.discontinued") {
		t.Fatalf("change discontinued: %v", err)
	}
	if err := p.Discontinue(date("2026-12-01")); !isViolation(err, "products.discontinued") {
		t.Fatalf("twice: %v", err)
	}
}

func TestUnits(t *testing.T) {
	for _, c := range []struct{ q, from, to, want string }{{"1500", "g", "kg", "1.5"}, {"2", "l", "ml", "2000"}, {"90", "min", "h", "1.5"},
		{"1.5", "h", "min", "90"}, {"2", "m3", "l", "2000"}, {"3", "ea", "pza", "3"}, {"250", "cm", "m", "2.5"}} {
		got, err := domain.Convert(dec(c.q), c.from, c.to)
		if err != nil || !got.Equal(dec(c.want)) {
			t.Fatalf("%s %s -> %s: %s %v", c.q, c.from, c.to, got, err)
		}
	}
	for _, c := range [][2]string{{"kg", "l"}, {"caja", "ea"}, {"caja", "pallet"}, {"kg", "docena"}} {
		if _, err := domain.Convert(dec("1"), c[0], c[1]); !isViolation(err, "products.uom_conversion") {
			t.Fatalf("%v: %v", c, err)
		}
	}
}

func TestPriceList(t *testing.T) {
	p, err := domain.RegisterProduct(domain.NewProductID(), company, "TOR-M8", good())
	if err != nil {
		t.Fatal(err)
	}
	pl, err := domain.ReconstitutePriceList(domain.NewPriceListID(), domain.PriceListState{Company: company, Code: "mayor", Name: "Mayoristas",
		Currency: vocab.MustCurrencyCode("EUR"), Active: true})
	if err != nil || pl.State().Code != "MAYOR" {
		t.Fatalf("price list: %v", err)
	}
	line := func(min, price, disc, from, to string) domain.PriceLine {
		l := domain.PriceLine{Product: p.ID(), MinQuantity: dec(min), UnitPrice: dec(price), Discount: dec(disc), From: date(from)}
		if to != "" {
			l.To = date(to)
		}
		return l
	}
	if err := pl.SetPrice(line("0", "0.10", "101", "2026-01-01", "")); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("discount: %v", err)
	}
	if err := pl.SetPrice(line("0", "0.10", "0", "2026-06-01", "2026-05-01")); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("dates: %v", err)
	}
	for _, l := range []domain.PriceLine{line("0", "0.10", "0", "2026-01-01", "2026-12-31"), line("1000", "0.08", "5", "2026-01-01", ""),
		line("0", "0.11", "0", "2027-01-01", "")} {
		if err := pl.SetPrice(l); err != nil {
			t.Fatal(err)
		}
	}
	if err := pl.SetPrice(line("0", "0.09", "0", "2026-12-31", "")); !isViolation(err, "products.price_overlap") {
		t.Fatalf("overlap: %v", err)
	}
	q := domain.QuoteOf(p, pl, dec("10"), date("2026-10-04"))
	if q.Source != "list" || !q.Net.Equal(dec("0.10")) {
		t.Fatalf("small quantity: %+v", q)
	}
	q = domain.QuoteOf(p, pl, dec("1000"), date("2026-10-04"))
	if !q.UnitPrice.Equal(dec("0.08")) || !q.Net.Equal(dec("0.076")) || !q.Discount.Equal(dec("5")) {
		t.Fatalf("volume: %+v", q)
	}
	if q = domain.QuoteOf(p, pl, dec("10"), date("2027-03-01")); !q.Net.Equal(dec("0.11")) {
		t.Fatalf("next year: %+v", q)
	}
	if q = domain.QuoteOf(p, pl, dec("10"), date("2025-12-31")); q.Source != "base" || !q.Net.Equal(dec("0.125")) {
		t.Fatalf("before the list: %+v", q)
	}
	pl.SetActive(false)
	if q = domain.QuoteOf(p, pl, dec("10"), date("2026-10-04")); q.Source != "base" {
		t.Fatalf("retired list: %+v", q)
	}
	if q = domain.QuoteOf(p, nil, dec("10"), date("2026-10-04")); q.Source != "base" {
		t.Fatalf("no list: %+v", q)
	}
	if err := pl.RemovePrice(p.ID(), dec("1000"), date("2026-01-01")); err != nil || len(pl.State().Lines) != 2 {
		t.Fatalf("remove: %v", err)
	}
	if err := pl.RemovePrice(p.ID(), dec("1000"), date("2026-01-01")); !errors.Is(err, fw.ErrNotFound) {
		t.Fatalf("remove twice: %v", err)
	}
}

func TestCategory(t *testing.T) {
	id := domain.NewCategoryID()
	if _, err := domain.ReconstituteCategory(id, domain.CategoryState{Company: company, Code: "FERR", Name: "Ferretería", Parent: id}); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("own parent: %v", err)
	}
	c, err := domain.ReconstituteCategory(id, domain.CategoryState{Company: company, Code: "ferr", Name: "Ferretería"})
	if err != nil || c.State().Code != "FERR" {
		t.Fatalf("category: %v", err)
	}
	if err := c.Rename(""); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("rename: %v", err)
	}
}
