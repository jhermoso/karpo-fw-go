package host_test

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	expapp "github.com/jhermoso/karpo-fw-go/contexts/exports/application"
	expdomain "github.com/jhermoso/karpo-fw-go/contexts/exports/domain"
	geoinfra "github.com/jhermoso/karpo-fw-go/contexts/geography/infrastructure"
	hrapp "github.com/jhermoso/karpo-fw-go/contexts/hr/application"
	impapp "github.com/jhermoso/karpo-fw-go/contexts/imports/application"
	parapp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pardomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	"github.com/jhermoso/karpo-fw-go/host"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
)

const bom = "\ufeff"

// datasetsScenario loads two companies with their people and exports each list of Parties the
// grid asks for, as an administrator and as someone who sees one company only.
func datasetsScenario(t *testing.T, sw *hotswap.Switch) {
	ctx := context.Background()
	h, err := host.Compose(sw, host.Options{JWTSecret: []byte("datasets-test-secret")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.Start(ctx); err != nil {
		t.Fatal(err)
	}
	admin, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "admin", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
	admin.GlobalAdmin = true
	actx := authz.WithContext(ctx, admin)

	org := "unitType,name,parentOrg\nInternalOrganization,Maccorp Exact Change,\nInternalOrganization,Karpo Servicios,\nDepartment,Operaciones,Maccorp Exact Change\n"
	people := "employeeNumber,firstName,lastName,email,gender,legalEntity,hireDate,terminationDate\n" +
		"P-1,Ana,\"García, de la O\",ana@maccorp.test,F,Maccorp Exact Change,2020-03-01,\n" +
		"P-2,Luis,Pérez,luis@maccorp.test,M,Maccorp / Karpo (PLURIEMPLEO),2021-05-10,2024-12-31\n" +
		"P-3,Eva,=Ruiz,eva@karpo.test,F,Karpo Servicios,2025-02-03,\n"
	run, err := h.Imports.Service.Execute.Handle(actx, impapp.RunImport{Source: "personio", Files: []impapp.FileDTO{
		{Role: "org-units", Name: "org.csv", Content: org}, {Role: "people", Name: "people.csv", Content: people}}})
	if err != nil || run.Status != "succeeded" {
		t.Fatalf("import: %+v %v", run, err)
	}
	companies, _ := h.Parties.Organizations.All(ctx)
	id := map[string]string{}
	for _, c := range companies {
		id[c.Name] = c.ID
	}
	maccorp, karpo := id["Maccorp Exact Change"], id["Karpo Servicios"]
	// A customer of Maccorp, with its tax number.
	customer, err := h.Parties.Service.RegisterOrganization.Handle(actx, parapp.RegisterOrganization{LegalName: "Cliente Uno SL",
		Roles: []string{pardomain.RoleCustomer.String()}, Affiliation: &parapp.NewAffiliation{Organization: maccorp, RelationshipType: pardomain.RelCustomer.String()}})
	if err != nil {
		t.Fatal(err)
	}
	cid, _ := pardomain.ParsePartyID(customer.ID)
	if _, err := h.Parties.Service.AddIdentification.Handle(actx, parapp.AddIdentification{PartyID: cid, DocumentType: "c0000000-0004-0000-0000-000000000003",
		Country: "ES", Number: "A58818501", Primary: true}); err != nil {
		t.Fatal(err)
	}

	// export asks for a list as a file, lets the chores write it and reads it.
	export := func(c context.Context, dataset string, filter map[string]string) []string {
		t.Helper()
		job, err := h.Exports.Service.Start.Handle(c, expapp.StartExport{Dataset: dataset, Filter: filter})
		if err != nil {
			t.Fatalf("%s: %v", dataset, err)
		}
		if chores, err := h.RunChores(ctx); err != nil || chores.ExportsWritten != 1 {
			t.Fatalf("%s: chores %+v %v", dataset, chores, err)
		}
		jid, _ := expdomain.ParseJobID(job.ID)
		file, err := h.Exports.Service.Download.Handle(c, expapp.DownloadJob{ID: jid})
		if err != nil {
			t.Fatalf("%s: %v", dataset, err)
		}
		defer file.Content.Close()
		body, _ := io.ReadAll(file.Content)
		lines := strings.Split(strings.TrimSuffix(strings.TrimPrefix(string(body), bom), "\r\n"), "\r\n")
		slices.Sort(lines[1:]) // the header first, the rows in a known order
		return lines
	}
	same := func(what string, got []string, want ...string) {
		t.Helper()
		if !slices.Equal(got, want) {
			t.Fatalf("%s:\n%s\nwant\n%s", what, strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	}

	sets, err := h.Exports.Service.Datasets.Handle(actx, expapp.ListDatasets{})
	keys := []string{}
	for _, s := range sets {
		keys = append(keys, s.Key)
	}
	if err != nil || !slices.Equal(keys, []string{"customer-accounts", "customers", "employees", "internal-organizations", "invoices", "orders", "organizations", "parties",
		"party-relationships", "party-roles", "persons", "purchase-invoices", "receivables"}) {
		t.Fatalf("lists that can be exported: %v %v", keys, err)
	}

	// People: a surname with a comma goes between quotes.
	same("persons", export(actx, "persons", nil), "Nombre,Tipo,Estado",
		"\"Ana García, de la O\",Persona,Activo", "Eva =Ruiz,Persona,Activo", "Luis Pérez,Persona,Activo")
	same("persons of Karpo", export(actx, "persons", map[string]string{"organization": karpo}), "Nombre,Tipo,Estado",
		"Eva =Ruiz,Persona,Activo", "Luis Pérez,Persona,Activo")
	same("persons called luis", export(actx, "persons", map[string]string{"name": "luis"}), "Nombre,Tipo,Estado", "Luis Pérez,Persona,Activo")
	same("internal organizations", export(actx, "internal-organizations", nil), "Nombre,Tipo,Estado",
		"Karpo Servicios,Organización,Activo", "Maccorp Exact Change,Organización,Activo")
	same("organizations", export(actx, "organizations", nil), "Nombre,Tipo,Estado", "Cliente Uno SL,Organización,Activo",
		"Karpo Servicios,Organización,Activo", "Maccorp Exact Change,Organización,Activo", "Operaciones,Organización,Activo")
	if all := export(actx, "parties", nil); len(all) != 1+7 {
		t.Fatalf("parties: %v", all)
	}
	// Customers, with the tax number the C# column always left empty.
	same("customers", export(actx, "customers", nil), "Nombre,Tipo,CIF/NIF,Estado", "Cliente Uno SL,Organización,A58818501,Activo")

	// Employees come from HR, with the name of Parties.
	same("employees", export(actx, "employees", nil), "Nombre,Número empleado,Fecha contratación,Fecha baja,Activo",
		"\"Ana García, de la O\",P-1,2020-03-01,,Sí", "Eva =Ruiz,P-3,2025-02-03,,Sí", "Luis Pérez,P-2,2021-05-10,2024-12-31,No",
		"Luis Pérez,P-2,2021-05-10,2024-12-31,No")
	same("employees of Karpo still there", export(actx, "employees", map[string]string{"organization": karpo, "active": "true"}),
		"Nombre,Número empleado,Fecha contratación,Fecha baja,Activo", "Eva =Ruiz,P-3,2025-02-03,,Sí")

	// A row per role, and each relationship once, from the side it starts at.
	roles := export(actx, "party-roles", map[string]string{"name": "Luis"})
	if len(roles) != 2 || roles[0] != "Participante,Tipo de rol,Fecha inicio,Fecha expiración,Activo" || !strings.HasPrefix(roles[1], "Luis Pérez,Employee,") ||
		!strings.HasSuffix(roles[1], ",,Sí") {
		t.Fatalf("roles of Luis: %v", roles)
	}
	rels := export(actx, "party-relationships", nil)
	employment, rollup, custom := 0, 0, 0
	for _, r := range rels[1:] {
		switch {
		case strings.HasPrefix(r, "Employment,"):
			employment++
		case strings.HasPrefix(r, "Organization Rollup,Operaciones,Maccorp Exact Change,"):
			rollup++
		case strings.Contains(r, "Cliente Uno SL") && strings.Contains(r, "Maccorp Exact Change"):
			custom++
		}
	}
	if rels[0] != "Tipo de relación,Participante origen,Participante destino,Fecha inicio,Fecha expiración,Estado,Observaciones" ||
		employment != 4 || rollup != 1 || custom != 1 || len(rels) != 1+6 {
		t.Fatalf("relationships: %v", rels)
	}

	// Who sees one company gets the file of that company, whatever they ask for.
	scoped := func(perms ...authz.Permission) context.Context {
		ac, err := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "clerk", Kind: authz.Service,
			Permissions: append([]authz.Permission{expapp.PermJobRead, expapp.PermJobCreate}, perms...),
			Grants:      []authz.Grant{{OrganizationID: fw.MustParseUUID(karpo), Level: authz.ReadOnly}}})
		if err != nil {
			t.Fatal(err)
		}
		return authz.WithContext(ctx, ac)
	}
	clerk := scoped(parapp.PermPartyRead, hrapp.PermEmploymentRead)
	same("what the clerk of Karpo sees", export(clerk, "persons", nil), "Nombre,Tipo,Estado", "Eva =Ruiz,Persona,Activo", "Luis Pérez,Persona,Activo")
	// Asking for another company narrows what one sees, it never widens it: Luis, whom the clerk
	// sees because he also works for Karpo; not Ana, who works for Maccorp only.
	same("asking for another company", export(clerk, "persons", map[string]string{"organization": maccorp}), "Nombre,Tipo,Estado",
		"Luis Pérez,Persona,Activo")
	if emp := export(clerk, "employees", nil); len(emp) != 1+2 {
		t.Fatalf("employees the clerk sees: %v", emp)
	}
	// Each list asks for the permission of what it lists.
	if _, err := h.Exports.Service.Start.Handle(scoped(parapp.PermPartyRead), expapp.StartExport{Dataset: "employees"}); !errors.Is(err, fw.ErrForbidden) {
		t.Fatalf("employees without the permission of HR: %v", err)
	}
	if _, err := h.Exports.Service.Start.Handle(scoped(parapp.PermPartyRead), expapp.StartExport{Dataset: "party-relationships"}); !errors.Is(err, fw.ErrForbidden) {
		t.Fatalf("relationships without their permission: %v", err)
	}
	if _, err := h.Exports.Service.Start.Handle(clerk, expapp.StartExport{Dataset: "persons", Filter: map[string]string{"city": "x"}}); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("a filter the list does not have: %v", err)
	}
}

func TestDatasets_OnMemory(t *testing.T) {
	store := memory.NewStore("memory")
	if err := geoinfra.LoadMemory(context.Background(), store); err != nil {
		t.Fatal(err)
	}
	sw := hotswap.New(store)
	t.Cleanup(func() { _ = sw.Close(context.Background()) })
	datasetsScenario(t, sw)
}

func TestDatasets_OnSQLite(t *testing.T) {
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
	datasetsScenario(t, hotswap.New(db))
}
