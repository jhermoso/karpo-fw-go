package spec_test

import (
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
)

type line struct {
	sku string
	qty int
}

type order struct {
	number   string
	total    float64
	paid     bool
	coupon   *string
	placedAt time.Time
	lines    []line
}

var (
	number   = spec.Text[order]("number", func(o order) string { return o.number })
	total    = spec.Ordered[order, float64]("total", func(o order) float64 { return o.total })
	paid     = spec.Comparable[order, bool]("paid", func(o order) bool { return o.paid })
	coupon   = spec.Optional[order, string]("coupon", func(o order) *string { return o.coupon })
	placedAt = spec.Time[order]("placed_at", func(o order) time.Time { return o.placedAt })
	lines    = spec.Collection[order, line]("lines", func(o order) []line { return o.lines })
	sku      = spec.Text[line]("sku", func(l line) string { return l.sku })
	qty      = spec.Ordered[line, int]("qty", func(l line) int { return l.qty })
)

func TestFields_EvaluateInMemory(t *testing.T) {
	c := "WELCOME"
	t0 := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	o := order{number: "SO-2026-001", total: 120.5, paid: true, coupon: &c, placedAt: t0,
		lines: []line{{"A-1", 2}, {"B-7", 10}}}

	cases := []struct {
		name string
		s    spec.Spec[order]
		want bool
	}{
		{"eq", number.Eq("SO-2026-001"), true},
		{"ne", number.Ne("SO-2026-001"), false},
		{"in", number.In("x", "SO-2026-001"), true},
		{"in empty", number.In(), false},
		{"not in empty", number.NotIn(), true},
		{"starts", number.StartsWith("SO-"), true},
		{"ends", number.EndsWith("001"), true},
		{"contains case sensitive", number.Contains("so-"), false},
		{"contains fold", number.ContainsFold("so-"), true},
		{"equal fold", number.EqualFold("so-2026-001"), true},
		{"gt", total.Gt(100), true},
		{"le", total.Le(100), false},
		{"between", total.Between(120.5, 121), true},
		{"bool", paid.Eq(true), true},
		{"optional eq", coupon.Eq("WELCOME"), true},
		{"optional null", coupon.IsNull(), false},
		{"time after", placedAt.After(t0.Add(-time.Second)), true},
		{"time eq other zone", placedAt.Eq(t0.In(time.FixedZone("CET", 3600))), true},
		{"any", lines.Any(qty.Ge(10).And(sku.StartsWith("B"))), true},
		{"any nil", lines.Any(nil), true},
		{"none", lines.None(sku.Eq("A-1")), false},
		{"empty", lines.IsEmpty(), false},
		{"or", number.Eq("x").Or(paid.Eq(true)), true},
		{"not", paid.Eq(true).Not(), false},
		{"double not", paid.Eq(true).Not().Not(), true},
		{"all", spec.All[order](), true},
		{"none spec", spec.None[order](), false},
		{"zero spec", spec.Spec[order]{}, true},
		{"custom", spec.Custom("orders.big", func(o order) bool { return o.total > 100 }, 100), true},
	}
	for _, tc := range cases {
		if got := tc.s.IsSatisfiedBy(o); got != tc.want {
			t.Errorf("%s: got %v want %v (%s)", tc.name, got, tc.want, spec.Format(tc.s.Expr()))
		}
	}
	nullCoupon := o
	nullCoupon.coupon = nil
	if !coupon.IsNull().IsSatisfiedBy(nullCoupon) || coupon.Eq("WELCOME").IsSatisfiedBy(nullCoupon) {
		t.Error("null optional semantics")
	}
}

func TestComposition_BuildsFlatTranslatableTrees(t *testing.T) {
	s := spec.And[order](paid.Eq(true), spec.And[order](total.Gt(10), number.StartsWith("SO")), spec.All[order]())
	and, ok := s.Expr().(spec.AndExpr)
	if !ok || len(and.Exprs) != 3 {
		t.Fatalf("expected flattened AND with 3 children, got %s", spec.Format(s.Expr()))
	}
	if _, ok := spec.And[order](paid.Eq(true), spec.None[order]()).Expr().(spec.Const); !ok {
		t.Fatal("AND with FALSE must fold to FALSE")
	}
	if c, ok := spec.Or[order](paid.Eq(true), spec.All[order]()).Expr().(spec.Const); !ok || !c.Value {
		t.Fatal("OR with TRUE must fold to TRUE")
	}
	if _, ok := paid.Eq(true).Not().Not().Expr().(spec.Compare); !ok {
		t.Fatal("double negation must cancel")
	}
	any := lines.Any(qty.Gt(1)).Expr().(spec.AnyExpr)
	if any.Collection != "lines" || any.Where.(spec.Compare).Field != "qty" {
		t.Fatalf("unexpected ANY node %+v", any)
	}

	var fields []string
	spec.Walk(s.Or(lines.Any(sku.Eq("x"))).Expr(), func(e spec.Expr) bool {
		if c, ok := e.(spec.Compare); ok {
			fields = append(fields, c.Field)
		}
		return true
	})
	if len(fields) != 4 {
		t.Fatalf("walk visited %v", fields)
	}
	if got := spec.Format(paid.Eq(true).And(coupon.IsNull()).Expr()); got != `(paid = true AND coupon IS NULL)` {
		t.Fatalf("format: %s", got)
	}
}

func TestOrder_TypedComparison(t *testing.T) {
	a, b := order{total: 1, number: "b"}, order{total: 2, number: "a"}
	orders := []spec.Order[order]{total.Desc(), number.Asc()}
	if spec.CompareAll(orders, a, b) <= 0 {
		t.Fatal("desc total must put b first")
	}
	if o := placedAt.Asc(); o.Field != "placed_at" || o.Desc {
		t.Fatalf("unexpected order %+v", o)
	}
}
