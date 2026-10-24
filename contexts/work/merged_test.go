package work_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	pinfra "github.com/jhermoso/karpo-fw-go/contexts/parties/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/work"
	wapp "github.com/jhermoso/karpo-fw-go/contexts/work/application"
	winfra "github.com/jhermoso/karpo-fw-go/contexts/work/infrastructure"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
)

// mergedParties hace de Parties: dice qué parties están fusionadas en otra.
type mergedParties struct{ ids []string }

func (m *mergedParties) MergedParties(context.Context) ([]string, error) { return m.ids, nil }

// Los listados de trabajos y de tiempo dejan fuera por defecto los de una party fusionada en otra
// (el cliente del trabajo, la persona que imputa) y los traen con includeMergedParties; un trabajo
// sin cliente no se deja fuera, y el trabajo pedido por su identidad se sigue encontrando
// (docs/DECISIONES-COMUNES.md, D-64 y D-100). En memoria y en SQLite: en SQL, el cliente del
// trabajo interno es una columna nula, que un NOT IN solo perdería.
func TestListingsLeaveOutMergedParties_MemoryThenSQLite(t *testing.T) {
	merged := &mergedParties{}
	h := compose(t, work.WithMergedParties(merged))
	leaveOutMerged(h, merged)

	ctx := context.Background()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "merged.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{pinfra.Migrations(), winfra.Migrations()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.sw.Swap(ctx, db); err != nil {
		t.Fatal(err)
	}
	leaveOutMerged(h, merged)
}

func leaveOutMerged(h *host, merged *mergedParties) {
	t := h.t
	ctx := h.adminCtx
	acme, err := h.parties.Service.RegisterOrganization.Handle(ctx, papp.RegisterOrganization{LegalName: "Acme fusiones",
		Roles: []string{pdomain.RoleInternalOrganization.String()}})
	h.ok(err)
	for _, u := range []string{"planner", "lead", "worker", "viewer"} {
		h.grant(u, acme.ID)
	}
	ana, bea := fw.NewUUID().String(), fw.NewUUID().String()
	merged.ids = []string{bea}

	// Un trabajo para cada cliente y uno interno, sin cliente.
	ids := map[string]string{}
	for code, customer := range map[string]string{"W-ANA": ana, "W-BEA": bea, "W-INT": ""} {
		body := map[string]any{"company": acme.ID, "code": code, "kind": "project", "name": "Trabajo " + code, "on": "2026-03-02"}
		if customer != "" {
			body["customer"] = customer
		}
		var w wapp.WorkDTO
		h.must(h.do("POST", "/api/work", "planner", body, &w), 201, "work "+code)
		ids[code] = w.ID
	}

	codes := func(path string) map[string]bool {
		t.Helper()
		var page fw.Page[wapp.WorkDTO]
		h.must(h.do("GET", path, "viewer", nil, &page), 200, path)
		out := map[string]bool{}
		for _, w := range page.Items {
			out[w.Code] = true
		}
		return out
	}
	path := "/api/work?company=" + acme.ID
	if got := codes(path); len(got) != 2 || !got["W-ANA"] || !got["W-INT"] {
		t.Fatalf("works: %v, want W-ANA and W-INT", got)
	}
	if got := codes(path + "&includeMergedParties=true"); len(got) != 3 {
		t.Fatalf("works with includeMergedParties: %v", got)
	}
	h.must(h.do("GET", "/api/work/"+ids["W-BEA"], "viewer", nil, nil), 200, "the work of a merged party, by id")
	// Filtrar por el cliente fusionado trae sus trabajos sin pedir includeMergedParties (D-108).
	if got := codes(path + "&customer=" + bea); len(got) != 1 || !got["W-BEA"] {
		t.Fatalf("works filtered by the merged customer: %v", got)
	}
	// El total y las páginas no cuentan los que quedan fuera.
	for query, want := range map[string]int64{"&size=1": 2, "&size=1&includeMergedParties=true": 3} {
		var page fw.Page[wapp.WorkDTO]
		h.must(h.do("GET", path+query, "viewer", nil, &page), 200, path+query)
		if len(page.Items) != 1 || page.Total != want || page.TotalPages != int(want) {
			t.Fatalf("works%s: %d items, total %d, %d pages; want 1, %d, %d", query, len(page.Items), page.Total, page.TotalPages, want, want)
		}
	}

	// Tiempo de las dos personas en el trabajo interno.
	internal := ids["W-INT"]
	for _, p := range []string{ana, bea} {
		h.must(h.do("POST", "/api/work/"+internal+"/assign", "planner", map[string]any{"person": p, "rate": "20", "from": "2026-03-09"}, nil), 200, "assign")
	}
	h.must(h.do("PUT", "/api/work/"+internal, "planner", map[string]any{"name": "Trabajo W-INT", "plannedStart": "2026-03-09",
		"plannedEnd": "2026-03-10", "estimatedHours": "16"}, nil), 200, "plan")
	h.must(h.do("POST", "/api/work/"+internal+"/progress", "lead", map[string]any{"to": "scheduled", "on": "2026-03-03"}, nil), 200, "scheduled")
	h.must(h.do("POST", "/api/work/"+internal+"/progress", "lead", map[string]any{"to": "in-progress", "on": "2026-03-09"}, nil), 200, "started")
	for _, p := range []string{ana, bea} {
		h.must(h.do("POST", "/api/time", "worker", map[string]any{"work": internal, "person": p, "date": "2026-03-09", "hours": "4"}, nil), 201, "time")
	}

	var times fw.Page[wapp.TimeDTO]
	tpath := "/api/time?company=" + acme.ID
	h.must(h.do("GET", tpath, "viewer", nil, &times), 200, "time")
	if len(times.Items) != 1 || times.Items[0].Person != ana {
		t.Fatalf("time: %+v, want only %s", times.Items, ana)
	}
	h.must(h.do("GET", tpath+"&includeMergedParties=true", "viewer", nil, &times), 200, "time")
	if len(times.Items) != 2 {
		t.Fatalf("time with includeMergedParties: %+v", times.Items)
	}
	h.must(h.do("GET", tpath+"&person="+bea, "viewer", nil, &times), 200, "time of the merged person")
	if len(times.Items) != 1 || times.Items[0].Person != bea {
		t.Fatalf("time filtered by the merged person: %+v", times.Items)
	}
}
