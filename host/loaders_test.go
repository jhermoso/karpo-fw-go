package host_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"testing"

	facapp "github.com/jhermoso/karpo-fw-go/contexts/facilities/application"
	geoinfra "github.com/jhermoso/karpo-fw-go/contexts/geography/infrastructure"
	hrapp "github.com/jhermoso/karpo-fw-go/contexts/hr/application"
	impapp "github.com/jhermoso/karpo-fw-go/contexts/imports/application"
	parapp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	"github.com/jhermoso/karpo-fw-go/host"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
)

const personioOrg = `unitType,name,parentOrg,notes
InternalOrganization,Maccorp Exact Change,,
InternalOrganization,Karpo Servicios,,
Department,Operaciones,Maccorp Exact Change,
Department,Operaciones,Karpo Servicios,homónimo
Office,Oficina Sol,Maccorp Exact Change,Puerta del Sol
Office,Aeropuerto T4,,
`

const personioHeader = "employeeNumber,firstName,lastName,preferredName,email,gender,legalEntity,department,workCenter,jobTitle,isSupervisor,notes,hireDate,terminationDate\n"

const personioPeople = personioHeader + `P-MC-001,Ana,García López,,ana@maccorp.test,F,Maccorp Exact Change,Operaciones,Oficina Sol,"Auxiliar, Caja",Sí,,01/03/2020,
P-MC-002,Luis,Pérez,Lucho Pérez,luis@maccorp.test,M,Maccorp / Karpo (PLURIEMPLEO),Operaciones,Oficina Sol,Cajero,no,,2021-05-10,2024-12-31
P-MC-006,Eva,Ruiz,,eva@karpo.test,,Karpo Servicios,Operaciones,,Gerente,1,,,
`

// A month later: Eva's hire date is known, and Ana has left.
const personioLater = personioHeader + `P-MC-001,Ana,García López,,ana@maccorp.test,F,Maccorp Exact Change,Operaciones,Oficina Sol,"Auxiliar, Caja",Sí,,01/03/2020,2026-09-30
P-MC-002,Luis,Pérez,Lucho Pérez,luis@maccorp.test,M,Maccorp / Karpo (PLURIEMPLEO),Operaciones,Oficina Sol,Cajero,no,,2021-05-10,2024-12-31
P-MC-006,Eva,Ruiz,,eva@karpo.test,,Karpo Servicios,Operaciones,,Gerente,1,,2025-02-03,
`

// loadersScenario imports the organization of two companies from the files of Personio through
// the loaders of the host, and looks in Parties, Facilities and HR for what they became.
func loadersScenario(t *testing.T, sw *hotswap.Switch) {
	ctx := context.Background()
	h, err := host.Compose(sw, host.Options{JWTSecret: []byte("loaders-test-secret")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.Start(ctx); err != nil {
		t.Fatal(err)
	}
	admin, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "importer", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
	admin.GlobalAdmin = true
	actx := authz.WithContext(ctx, admin)
	files := func(people string) impapp.RunImport {
		return impapp.RunImport{Source: "personio", Files: []impapp.FileDTO{{Role: "org-units", Name: "org.csv", Content: personioOrg},
			{Role: "people", Name: "people.csv", Content: people}}}
	}
	counts := func(cs []impapp.CountDTO) map[string]impapp.CountDTO {
		out := map[string]impapp.CountDTO{}
		for _, c := range cs {
			c2 := c
			c2.Kind = ""
			out[c.Kind] = c2
		}
		return out
	}
	said := []string{} // what the run being checked had to say, for when a count is not the expected one
	expect := func(what string, got []impapp.CountDTO, want map[string]impapp.CountDTO) {
		t.Helper()
		g := counts(got)
		for kind, w := range want {
			if g[kind] != w {
				t.Fatalf("%s, %s: %+v, want %+v (%v)", what, kind, g[kind], w, said)
			}
		}
	}
	codes := func(ms []impapp.MessageDTO) []string {
		out := []string{}
		for _, m := range ms {
			out = append(out, m.Kind+":"+m.Key+":"+m.Code)
		}
		slices.Sort(out)
		return out
	}
	sources, err := h.Imports.Service.Sources.Handle(actx, impapp.ListSources{})
	if err != nil || len(sources) != 1 || len(sources[0].Unloaded) != 0 {
		t.Fatalf("every kind of Personio has who loads it: %+v %v", sources, err)
	}

	// Looking first: everything would be created, and nothing is.
	pre, err := h.Imports.Service.Preview.Handle(actx, files(personioPeople))
	if err != nil {
		t.Fatal(err)
	}
	expect("preview", pre.Counts, map[string]impapp.CountDTO{"legal-entity": {Read: 2, Created: 2}, "department": {Read: 2, Created: 2},
		"work-center": {Read: 2, Created: 2}, "person": {Read: 3, Created: 3}, "employment": {Read: 4, Created: 4}})
	if all, _ := h.Parties.Organizations.All(ctx); len(all) != 0 {
		t.Fatalf("a preview writes nothing: %+v", all)
	}

	// The first run.
	run, err := h.Imports.Service.Execute.Handle(actx, files(personioPeople))
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range run.Messages {
		said = append(said, m.Key+": "+m.Text)
	}
	expect("first run", run.Counts, map[string]impapp.CountDTO{"legal-entity": {Read: 2, Created: 2}, "department": {Read: 2, Created: 2},
		"work-center": {Read: 2, Created: 1, Failed: 1}, "person": {Read: 3, Created: 3}, "employment": {Read: 4, Created: 3, Failed: 1}})
	if got := codes(run.Messages); run.Status != "completed-with-errors" || !slices.Equal(got, []string{
		"employment:P-MC-006:imports.hire_date_required", "work-center:Aeropuerto T4:imports.work_center_without_company"}) {
		t.Fatalf("first run: %s %v", run.Status, got)
	}

	// What it became, asked of each context.
	companies, _ := h.Parties.Organizations.All(ctx)
	if len(companies) != 2 {
		t.Fatalf("companies: %+v", companies)
	}
	id := map[string]string{}
	for _, c := range companies {
		id[c.Name] = c.ID
	}
	maccorp, karpo := id["Maccorp Exact Change"], id["Karpo Servicios"]
	for _, company := range []string{maccorp, karpo} { // a department of the same name in each, rolled up to its own
		below, err := h.Parties.Organizations.Descendants(ctx, []string{company})
		if err != nil {
			t.Fatal(err)
		}
		units, _ := h.Parties.Directory.Resolve(ctx, below)
		names := []string{}
		for uid, u := range units {
			if uid != company {
				names = append(names, u.Name)
			}
		}
		if !slices.Equal(names, []string{"Operaciones"}) {
			t.Fatalf("units of %s: %v", company, names)
		}
	}
	people := func(company string) []string {
		t.Helper()
		p, err := h.Parties.Service.Search.Handle(actx, parapp.SearchParties{Kind: "person", Organization: company, Size: 50})
		if err != nil {
			t.Fatal(err)
		}
		out := []string{}
		for _, x := range p.Items {
			out = append(out, x.Name)
		}
		slices.Sort(out)
		return out
	}
	// Luis works for both: registered with the first, affiliated with the second when he was hired there.
	if m, k := people(maccorp), people(karpo); !slices.Equal(m, []string{"Ana García López", "Luis Pérez"}) || !slices.Equal(k, []string{"Eva Ruiz", "Luis Pérez"}) {
		t.Fatalf("people: %v %v", m, k)
	}
	offices, err := h.Facilities.Service.Search.Handle(actx, facapp.SearchFacilities{Organization: maccorp, Size: 50})
	if err != nil || offices.Total != 1 || offices.Items[0].Name != "Oficina Sol" || offices.Items[0].Description != "Puerta del Sol" {
		t.Fatalf("facilities: %+v %v", offices.Items, err)
	}
	jobs := func(company string) map[string]hrapp.EmploymentDTO {
		t.Helper()
		p, err := h.HR.Service.SearchEmployments.Handle(actx, hrapp.SearchEmployments{Employer: company, Size: 50})
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]hrapp.EmploymentDTO{}
		for _, e := range p.Items {
			out[e.Number] = e
		}
		return out
	}
	m, k := jobs(maccorp), jobs(karpo)
	if len(m) != 2 || len(k) != 1 || m["P-MC-001"].Hired != "2020-03-01" || m["P-MC-001"].Terminated != "" || m["P-MC-002"].Hired != "2021-05-10" ||
		m["P-MC-002"].Terminated != "2024-12-31" || k["P-MC-002"].Terminated != "2024-12-31" || k["P-MC-002"].Person != m["P-MC-002"].Person {
		t.Fatalf("employments: %+v %+v", m, k)
	}

	// The same files again: nothing new, nothing twice; what failed fails the same.
	again, err := h.Imports.Service.Execute.Handle(actx, files(personioPeople))
	if err != nil {
		t.Fatal(err)
	}
	expect("second run", again.Counts, map[string]impapp.CountDTO{"legal-entity": {Read: 2, Unchanged: 2}, "department": {Read: 2, Unchanged: 2},
		"work-center": {Read: 2, Unchanged: 1, Failed: 1}, "person": {Read: 3, Unchanged: 3}, "employment": {Read: 4, Unchanged: 3, Failed: 1}})
	if len(people(maccorp)) != 2 || len(jobs(maccorp)) != 2 {
		t.Fatal("nobody twice")
	}

	// A month later: Eva's hire date arrives and she is hired; Ana left and her employment ends.
	later, err := h.Imports.Service.Execute.Handle(actx, files(personioLater))
	if err != nil {
		t.Fatal(err)
	}
	expect("a month later", later.Counts, map[string]impapp.CountDTO{"person": {Read: 3, Unchanged: 3}, "employment": {Read: 4, Created: 1, Updated: 1, Unchanged: 2}})
	m, k = jobs(maccorp), jobs(karpo)
	if m["P-MC-001"].Terminated != "2026-09-30" || k["P-MC-006"].Hired != "2025-02-03" || len(k) != 2 {
		t.Fatalf("a month later: %+v %+v", m, k)
	}

	// The references say what each key of Personio is, and the audit of each context that it came
	// from an import.
	refs, err := h.Imports.Service.References.Handle(actx, impapp.SearchReferences{Source: "personio", Size: 50})
	if err != nil || refs.Total != 12 { // 2 companies, 2 departments, 1 work center, 3 persons, 4 employments
		t.Fatalf("references: %d %v", refs.Total, err)
	}
	trail, err := h.Parties.Audit.Trail(ctx, "parties.party", maccorp)
	if err != nil || len(trail) == 0 || trail[0].Import == nil || trail[0].Import.SourceKey != "personio" || trail[0].Import.RunID.String() != run.ID {
		t.Fatalf("provenance: %+v %v", trail, err)
	}
}

func TestLoaders_OnMemory(t *testing.T) {
	store := memory.NewStore("memory")
	if err := geoinfra.LoadMemory(context.Background(), store); err != nil {
		t.Fatal(err)
	}
	sw := hotswap.New(store)
	t.Cleanup(func() { _ = sw.Close(context.Background()) })
	loadersScenario(t, sw)
}

func TestLoaders_OnSQLite(t *testing.T) {
	ctx := context.Background()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "karpo.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
	m, err := sqlrepo.NewMigrator(db, host.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	loadersScenario(t, hotswap.New(db))
}
