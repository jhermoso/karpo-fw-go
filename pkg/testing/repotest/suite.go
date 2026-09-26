package repotest

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
)

// Harness provides fresh backends to the suite.
type Harness struct {
	// New returns an empty repository and the unit of work its operations join.
	New func(t *testing.T) (domain.Repository[WidgetID, *Widget], domain.UnitOfWork)
	// EvaluatesInMemory is true for stores whose native execution is IsSatisfiedBy (memory):
	// they can evaluate any custom specification, so the ErrUnsupported check is skipped.
	EvaluatesInMemory bool
}

// Run executes the whole conformance suite.
func Run(t *testing.T, h Harness) {
	t.Run("Lifecycle", func(t *testing.T) { testLifecycle(t, h) })
	t.Run("NotFound", func(t *testing.T) { testNotFound(t, h) })
	t.Run("OptimisticConcurrency", func(t *testing.T) { testConcurrency(t, h) })
	t.Run("DuplicateIdentity", func(t *testing.T) { testDuplicate(t, h) })
	t.Run("Delete", func(t *testing.T) { testDelete(t, h) })
	t.Run("Children", func(t *testing.T) { testChildren(t, h) })
	t.Run("UnitOfWorkRollback", func(t *testing.T) { testRollback(t, h) })
	t.Run("SpecificationEquivalence", func(t *testing.T) { testSpecs(t, h) })
	t.Run("OrderingAndPaging", func(t *testing.T) { testPaging(t, h) })
	if !h.EvaluatesInMemory {
		t.Run("UntranslatableSpecificationFails", func(t *testing.T) { testUnsupported(t, h) })
	}
}

var base = time.Date(2025, 1, 1, 8, 0, 0, 0, time.UTC)

func ptr(s string) *string { return &s }

func mustWidget(t *testing.T, name string, price int64, active bool, color *string, created time.Time, parts ...Part) *Widget {
	t.Helper()
	w, err := NewWidget(NewWidgetID(), name, price, active, color, created, parts...)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func assertSame(t *testing.T, want, got *Widget) {
	t.Helper()
	if want.ID() != got.ID() || want.Name() != got.Name() || want.Price() != got.Price() ||
		want.Active() != got.Active() || !want.CreatedAt().Equal(got.CreatedAt()) {
		t.Fatalf("aggregate mismatch:\nwant %+v\ngot  %+v", want, got)
	}
	if (want.Color() == nil) != (got.Color() == nil) || (want.Color() != nil && *want.Color() != *got.Color()) {
		t.Fatalf("color mismatch: want %v got %v", want.Color(), got.Color())
	}
	if !slices.Equal(want.Parts(), got.Parts()) {
		t.Fatalf("parts mismatch: want %v got %v", want.Parts(), got.Parts())
	}
}

func testLifecycle(t *testing.T, h Harness) {
	ctx := context.Background()
	repo, _ := h.New(t)
	w := mustWidget(t, "Alpha", 100, true, ptr("red"), base, Part{"bolt", 2}, Part{"nut", 4})
	if w.Version() != 0 {
		t.Fatalf("new aggregate must have version 0")
	}
	if err := repo.Save(ctx, w); err != nil {
		t.Fatalf("save: %v", err)
	}
	if w.Version() != 1 {
		t.Fatalf("expected version 1 after insert, got %d", w.Version())
	}
	got, err := repo.Get(ctx, w.ID())
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	assertSame(t, w, got)
	if got.Version() != 1 || len(got.PendingEvents()) != 0 {
		t.Fatalf("loaded aggregate must carry version 1 and no events: v=%d events=%d", got.Version(), len(got.PendingEvents()))
	}

	if err := got.Rename("Alpha II"); err != nil {
		t.Fatal(err)
	}
	got.SetPrice(150)
	if err := repo.Save(ctx, got); err != nil {
		t.Fatalf("update: %v", err)
	}
	if got.Version() != 2 {
		t.Fatalf("expected version 2 after update, got %d", got.Version())
	}
	again, err := repo.Get(ctx, w.ID())
	if err != nil {
		t.Fatal(err)
	}
	assertSame(t, got, again)
}

func testNotFound(t *testing.T, h Harness) {
	repo, _ := h.New(t)
	_, err := repo.Get(context.Background(), NewWidgetID())
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func testConcurrency(t *testing.T, h Harness) {
	ctx := context.Background()
	repo, _ := h.New(t)
	w := mustWidget(t, "Beta", 200, true, nil, base)
	if err := repo.Save(ctx, w); err != nil {
		t.Fatal(err)
	}
	a, err := repo.Get(ctx, w.ID())
	if err != nil {
		t.Fatal(err)
	}
	b, err := repo.Get(ctx, w.ID())
	if err != nil {
		t.Fatal(err)
	}
	a.SetPrice(210)
	if err := repo.Save(ctx, a); err != nil {
		t.Fatalf("first writer must win: %v", err)
	}
	b.SetPrice(220)
	if err := repo.Save(ctx, b); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale writer must get ErrConflict, got %v", err)
	}
	final, _ := repo.Get(ctx, w.ID())
	if final.Price() != 210 || final.Version() != 2 {
		t.Fatalf("stale write leaked: price=%d version=%d", final.Price(), final.Version())
	}
}

func testDuplicate(t *testing.T, h Harness) {
	ctx := context.Background()
	repo, _ := h.New(t)
	w := mustWidget(t, "Gamma", 300, true, nil, base)
	if err := repo.Save(ctx, w); err != nil {
		t.Fatal(err)
	}
	dup, err := NewWidget(w.ID(), "Gamma copy", 1, false, nil, base)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, dup); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate identity must get ErrConflict, got %v", err)
	}
}

func testDelete(t *testing.T, h Harness) {
	ctx := context.Background()
	repo, _ := h.New(t)
	w := mustWidget(t, "Delta", 400, true, nil, base, Part{"bolt", 1})
	if err := repo.Save(ctx, w); err != nil {
		t.Fatal(err)
	}
	stale, _ := repo.Get(ctx, w.ID())
	w.SetPrice(401)
	if err := repo.Save(ctx, w); err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(ctx, stale); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("deleting a stale aggregate must get ErrConflict, got %v", err)
	}
	if err := repo.Delete(ctx, w); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.Get(ctx, w.ID()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
	if err := repo.Delete(ctx, w); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("deleting a missing aggregate must get ErrNotFound, got %v", err)
	}
	if n, _ := repo.Count(ctx, FieldParts.Any(PartName.Eq("bolt"))); n != 0 {
		t.Fatalf("children must be deleted with the aggregate")
	}
}

func testChildren(t *testing.T, h Harness) {
	ctx := context.Background()
	repo, _ := h.New(t)
	w := mustWidget(t, "Epsilon", 500, true, nil, base, Part{"a", 1}, Part{"b", 2}, Part{"c", 3})
	other := mustWidget(t, "Zeta", 600, true, nil, base, Part{"z", 9})
	for _, x := range []*Widget{w, other} {
		if err := repo.Save(ctx, x); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := repo.Get(ctx, w.ID())
	assertSame(t, w, got)

	got.ReplaceParts(Part{"c", 30}, Part{"d", 4})
	if err := repo.Save(ctx, got); err != nil {
		t.Fatal(err)
	}
	all, err := repo.Find(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 widgets, got %d", len(all))
	}
	for _, x := range all {
		switch x.ID() {
		case w.ID():
			assertSame(t, got, x)
		case other.ID():
			assertSame(t, other, x)
		}
	}
}

func testRollback(t *testing.T, h Harness) {
	ctx := context.Background()
	repo, uow := h.New(t)
	kept := mustWidget(t, "Eta", 700, true, nil, base)
	if err := repo.Save(ctx, kept); err != nil {
		t.Fatal(err)
	}

	boom := errors.New("boom")
	fresh := mustWidget(t, "Theta", 800, true, nil, base, Part{"x", 1})
	err := uow.Do(ctx, func(ctx context.Context) error {
		if err := repo.Save(ctx, fresh); err != nil {
			return err
		}
		k, err := repo.Get(ctx, kept.ID())
		if err != nil {
			return err
		}
		k.SetPrice(1)
		if err := repo.Save(ctx, k); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("expected the unit of work error, got %v", err)
	}
	if _, err := repo.Get(ctx, fresh.ID()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("insert must be rolled back, got %v", err)
	}
	if fresh.Version() != 0 {
		t.Fatalf("rolled back aggregate must recover version 0, got %d", fresh.Version())
	}
	k, _ := repo.Get(ctx, kept.ID())
	if k.Price() != 700 || k.Version() != 1 {
		t.Fatalf("update must be rolled back: price=%d version=%d", k.Price(), k.Version())
	}

	// A committed unit of work persists everything.
	if err := uow.Do(ctx, func(ctx context.Context) error { return repo.Save(ctx, fresh) }); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Get(ctx, fresh.ID()); err != nil {
		t.Fatalf("committed insert must be visible: %v", err)
	}
}

// fixtures avoids values whose comparison depends on collation (case variants, trailing
// spaces, empty strings) so results are identical on every engine.
func fixtures(t *testing.T) []*Widget {
	day := func(n int) time.Time { return base.AddDate(0, 0, n) }
	return []*Widget{
		mustWidget(t, "Alpha", 100, true, ptr("red"), day(0), Part{"bolt", 2}, Part{"nut", 10}),
		mustWidget(t, "Beta", 200, false, ptr("blue"), day(1), Part{"nail", 7}),
		mustWidget(t, "Gamma", 300, true, nil, day(2)),
		mustWidget(t, "Delta", 400, true, ptr("red"), day(3), Part{"bolt", 1}),
		mustWidget(t, "Epsilon", 500, false, nil, day(4), Part{"screw", 3}, Part{"nail", 12}),
		mustWidget(t, "Zeta 50%_off", 600, true, ptr("green"), day(5)),
		mustWidget(t, "Eta_x", 700, true, ptr("blue"), day(6), Part{"washer", 5}),
		mustWidget(t, "Theta", 800, false, ptr("red"), day(7), Part{"bolt", 20}),
		mustWidget(t, "Iota", 900, true, nil, day(8), Part{"nut", 1}),
		mustWidget(t, "Kappa", 1000, true, ptr("green"), day(9)),
		mustWidget(t, "Lambda", 1100, false, nil, day(10), Part{"nail", 6}),
		mustWidget(t, "Mu", 1200, true, ptr("red"), day(11), Part{"bolt", 3}, Part{"screw", 8}),
	}
}

func seed(t *testing.T, repo domain.Repository[WidgetID, *Widget]) []*Widget {
	all := fixtures(t)
	for _, w := range all {
		if err := repo.Save(context.Background(), w); err != nil {
			t.Fatalf("seed %s: %v", w.Name(), err)
		}
	}
	return all
}

func ids(ws []*Widget) []string {
	out := make([]string, len(ws))
	for i, w := range ws {
		out[i] = w.Name()
	}
	slices.Sort(out)
	return out
}

func testSpecs(t *testing.T, h Harness) {
	ctx := context.Background()
	repo, _ := h.New(t)
	all := seed(t, repo)

	cases := []struct {
		name string
		s    spec.Specification[*Widget]
	}{
		{"nil matches all", nil},
		{"all", spec.All[*Widget]()},
		{"none", spec.None[*Widget]()},
		{"id eq", FieldID.Eq(all[3].ID())},
		{"id in", FieldID.In(all[0].ID(), all[5].ID(), NewWidgetID())},
		{"id not in", FieldID.NotIn(all[0].ID(), all[1].ID())},
		{"name eq", FieldName.Eq("Beta")},
		{"name ne", FieldName.Ne("Beta")},
		{"name in", FieldName.In("Alpha", "Gamma", "Nope")},
		{"name in empty", FieldName.In()},
		{"name not in empty", FieldName.NotIn()},
		{"starts with", FieldName.StartsWith("Ka")},
		{"ends with", FieldName.EndsWith("ta")},
		{"contains literal percent", FieldName.Contains("%")},
		{"contains literal underscore", FieldName.Contains("_")},
		{"contains", FieldName.Contains("amb")},
		{"equal fold", FieldName.EqualFold("ALPHA")},
		{"contains fold", FieldName.ContainsFold("ET")},
		{"name ordered", FieldName.Lt("E")},
		{"price gt", FieldPrice.Gt(600)},
		{"price ge", FieldPrice.Ge(600)},
		{"price lt", FieldPrice.Lt(300)},
		{"price le", FieldPrice.Le(300)},
		{"price between", FieldPrice.Between(250, 750)},
		{"active", FieldActive.Eq(true)},
		{"inactive", FieldActive.Ne(true)},
		{"color null", FieldColor.IsNull()},
		{"color not null", FieldColor.IsNotNull()},
		{"color eq", FieldColor.Eq("red")},
		{"color in", FieldColor.In("red", "green")},
		{"created after", FieldCreatedAt.After(base.AddDate(0, 0, 8))},
		{"created at or before", FieldCreatedAt.AtOrBefore(base.AddDate(0, 0, 2))},
		{"created between", FieldCreatedAt.Between(base.AddDate(0, 0, 3), base.AddDate(0, 0, 6))},
		{"created eq", FieldCreatedAt.Eq(base.AddDate(0, 0, 4))},
		{"any part", FieldParts.Any(PartName.Eq("bolt"))},
		{"any part compound", FieldParts.Any(PartQty.Ge(5).And(PartName.StartsWith("n")))},
		{"no bolt", FieldParts.None(PartName.Eq("bolt"))},
		{"no parts", FieldParts.IsEmpty()},
		{"has parts", FieldParts.Any(nil)},
		{"and", FieldActive.Eq(true).And(FieldPrice.Gt(500), FieldColor.IsNotNull())},
		{"or", FieldName.Eq("Beta").Or(FieldPrice.Ge(1100))},
		{"not", FieldActive.Eq(true).Not()},
		{"double not", FieldActive.Eq(true).Not().Not()},
		{"nested", spec.And(spec.Or(FieldColor.Eq("red"), FieldColor.IsNull()), spec.Not(FieldParts.Any(PartName.Eq("bolt"))))},
		{"custom", Premium(500)},
		{"custom negated combined", Premium(500).Not().And(FieldPrice.Gt(300))},
		{"custom inside or", Premium(1000).Or(FieldName.Eq("Alpha"))},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var want []*Widget
			for _, w := range all {
				if tc.s == nil || tc.s.IsSatisfiedBy(w) {
					want = append(want, w)
				}
			}
			got, err := repo.Find(ctx, tc.s)
			if err != nil {
				t.Fatalf("find: %v", err)
			}
			if !slices.Equal(ids(want), ids(got)) {
				expr := "nil"
				if tc.s != nil {
					expr = spec.Format(tc.s.Expr())
				}
				t.Fatalf("store and in-memory evaluation differ for %s\nwant %v\ngot  %v", expr, ids(want), ids(got))
			}
			n, err := repo.Count(ctx, tc.s)
			if err != nil || n != int64(len(want)) {
				t.Fatalf("count: want %d got %d (err %v)", len(want), n, err)
			}
			ok, err := repo.Exists(ctx, tc.s)
			if err != nil || ok != (len(want) > 0) {
				t.Fatalf("exists: want %v got %v (err %v)", len(want) > 0, ok, err)
			}
		})
	}
}

func testPaging(t *testing.T, h Harness) {
	ctx := context.Background()
	repo, _ := h.New(t)
	all := seed(t, repo)

	s := FieldPrice.Ge(200)
	sorted := slices.Clone(all)
	sorted = slices.DeleteFunc(sorted, func(w *Widget) bool { return !s.IsSatisfiedBy(w) })
	slices.SortFunc(sorted, func(a, b *Widget) int { return int(b.Price() - a.Price()) })

	var names []string
	for number := 1; ; number++ {
		page, err := repo.FindPage(ctx, s, domain.NewPageRequest(number, 4, FieldPrice.Desc()))
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != int64(len(sorted)) || page.TotalPages != 3 {
			t.Fatalf("page %d: total=%d pages=%d", number, page.Total, page.TotalPages)
		}
		if len(page.Items) == 0 {
			break
		}
		for _, w := range page.Items {
			names = append(names, w.Name())
		}
	}
	var want []string
	for _, w := range sorted {
		want = append(want, w.Name())
	}
	if !slices.Equal(want, names) {
		t.Fatalf("paging order mismatch\nwant %v\ngot  %v", want, names)
	}

	asc, err := repo.Find(ctx, FieldActive.Eq(true), FieldCreatedAt.Asc())
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(asc); i++ {
		if asc[i-1].CreatedAt().After(asc[i].CreatedAt()) {
			t.Fatalf("time ordering broken at %d", i)
		}
	}
	byName, err := repo.Find(ctx, FieldActive.Eq(false), FieldName.Asc())
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, len(byName))
	for i, w := range byName {
		got[i] = w.Name()
	}
	if !slices.IsSorted(got) {
		t.Fatalf("name ordering broken: %v", got)
	}

	beyond, err := repo.FindPage(ctx, s, domain.NewPageRequest(99, 4, FieldPrice.Desc()))
	if err != nil || len(beyond.Items) != 0 || beyond.Total != int64(len(sorted)) {
		t.Fatalf("page beyond the end: %+v %v", beyond, err)
	}
}

func testUnsupported(t *testing.T, h Harness) {
	repo, _ := h.New(t)
	_, err := repo.Find(context.Background(), Unknown().And(FieldPrice.Gt(1)))
	if !errors.Is(err, domain.ErrUnsupported) {
		t.Fatalf("untranslatable specification must fail with ErrUnsupported, got %v", err)
	}
	if err != nil && !strings.Contains(err.Error(), "repotest.unknown") {
		t.Fatalf("error should name the specification: %v", err)
	}
}
