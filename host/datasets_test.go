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
	hrdomain "github.com/jhermoso/karpo-fw-go/contexts/hr/domain"
	parapp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pardomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	"github.com/jhermoso/karpo-fw-go/host"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
)

const bom = "\ufeff"

// datasetsScenario carga dos empresas con su gente y exporta cada lista de Parties que pide la
// rejilla, como administrador y como alguien que sólo ve una empresa.
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

	// La organización: dos empresas, y un departamento de la primera.
	register := func(name string, role string) string {
		t.Helper()
		d, err := h.Parties.Service.RegisterOrganization.Handle(actx, parapp.RegisterOrganization{LegalName: name, Roles: []string{role}})
		if err != nil {
			t.Fatal(err)
		}
		return d.ID
	}
	company := register("Añil Cambios", pardomain.RoleInternalOrganization.String())
	karpo := register("Karpo Servicios", pardomain.RoleInternalOrganization.String())
	department := register("Operaciones", pardomain.RoleDepartment.String())
	if _, err := h.Parties.Service.EstablishRelationship.Handle(actx, parapp.EstablishRelationship{Type: pardomain.RelOrganizationRollup.String(),
		From: department, To: company}); err != nil {
		t.Fatal(err)
	}
	// Personas, dadas de alta con la empresa que las contrata; contratadas por HR, y Luis por una segunda.
	person := func(given, surname, gender, employer string) string {
		t.Helper()
		p, err := h.Parties.Service.RegisterPerson.Handle(actx, parapp.RegisterPerson{GivenName: given, FirstSurname: surname, Gender: gender,
			Affiliation: &parapp.NewAffiliation{Organization: employer, RelationshipType: pardomain.RelEmployment.String()}})
		if err != nil {
			t.Fatal(err)
		}
		return p.ID
	}
	// Como hacía la importación de las personas: cuando HR aún no sabe que la empresa es una para la
	// que trabaja la persona, se le dice antes a Parties, y que es empleada si aún no lo es.
	hireAt := func(who, employer, number, hired, left string) {
		t.Helper()
		start, _ := vocab.ParseDate(hired)
		hire := hrapp.Hire{Person: who, Employer: employer, Number: number, Hired: start}
		e, err := h.HR.Service.Hire.Handle(actx, hire)
		var rule *fw.RuleViolationError
		if errors.As(err, &rule) && rule.Code == "hr.not_affiliated" {
			pid, _ := pardomain.ParsePartyID(who)
			p, gerr := h.Parties.Service.Get.Handle(actx, parapp.GetParty{ID: pid})
			if gerr != nil {
				t.Fatal(gerr)
			}
			employee := false
			for _, role := range p.Roles {
				employee = employee || role.Active && role.RoleType == pardomain.RoleEmployee.String()
			}
			if !employee {
				if _, err := h.Parties.Service.AssignRole.Handle(actx, parapp.AssignRole{PartyID: pid, RoleType: pardomain.RoleEmployee.String()}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := h.Parties.Service.EstablishRelationship.Handle(actx, parapp.EstablishRelationship{Type: pardomain.RelEmployment.String(),
				From: who, To: employer}); err != nil {
				t.Fatal(err)
			}
			e, err = h.HR.Service.Hire.Handle(actx, hire)
		}
		if err != nil {
			t.Fatal(err)
		}
		if left != "" {
			end, _ := vocab.ParseDate(left)
			eid, _ := hrdomain.ParseEmploymentID(e.ID)
			if _, err := h.HR.Service.Terminate.Handle(actx, hrapp.Terminate{ID: eid, On: end, Reason: "seed"}); err != nil {
				t.Fatal(err)
			}
		}
	}
	ana := person("Ana", "García, de la O", string(pardomain.GenderFemale), company)
	luis := person("Luis", "Pérez", string(pardomain.GenderMale), company)
	eva := person("Eva", "=Ruiz", string(pardomain.GenderFemale), karpo)
	hireAt(ana, company, "P-1", "2020-03-01", "")
	hireAt(luis, company, "P-2", "2021-05-10", "2024-12-31")
	hireAt(luis, karpo, "P-2", "2021-05-10", "2024-12-31")
	hireAt(eva, karpo, "P-3", "2025-02-03", "")
	// Un cliente de la primera empresa, con su número fiscal.
	customer, err := h.Parties.Service.RegisterOrganization.Handle(actx, parapp.RegisterOrganization{LegalName: "Cliente Uno SL",
		Roles: []string{pardomain.RoleCustomer.String()}, Affiliation: &parapp.NewAffiliation{Organization: company, RelationshipType: pardomain.RelCustomer.String()}})
	if err != nil {
		t.Fatal(err)
	}
	cid, _ := pardomain.ParsePartyID(customer.ID)
	if _, err := h.Parties.Service.AddIdentification.Handle(actx, parapp.AddIdentification{PartyID: cid, DocumentType: "c0000000-0004-0000-0000-000000000003",
		Country: "ES", Number: "A58818501", Primary: true}); err != nil {
		t.Fatal(err)
	}

	// export pide una lista como fichero, deja que las tareas la escriban y la lee.
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
		slices.Sort(lines[1:]) // primero la cabecera, las filas en un orden conocido
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
	if err != nil || !slices.Equal(keys, []string{"assets", "customer-accounts", "customers", "employees", "internal-organizations", "invoices", "journal", "orders", "organizations", "parties",
		"party-relationships", "party-roles", "payables", "persons", "purchase-invoices", "receivables"}) {
		t.Fatalf("lists that can be exported: %v %v", keys, err)
	}

	// Personas: un apellido con una coma va entre comillas.
	same("persons", export(actx, "persons", nil), "Nombre,Tipo,Estado",
		"\"Ana García, de la O\",Persona,Activo", "Eva =Ruiz,Persona,Activo", "Luis Pérez,Persona,Activo")
	same("persons of Karpo", export(actx, "persons", map[string]string{"organization": karpo}), "Nombre,Tipo,Estado",
		"Eva =Ruiz,Persona,Activo", "Luis Pérez,Persona,Activo")
	same("persons called luis", export(actx, "persons", map[string]string{"name": "luis"}), "Nombre,Tipo,Estado", "Luis Pérez,Persona,Activo")
	same("internal organizations", export(actx, "internal-organizations", nil), "Nombre,Tipo,Estado",
		"Añil Cambios,Organización,Activo", "Karpo Servicios,Organización,Activo")
	same("organizations", export(actx, "organizations", nil), "Nombre,Tipo,Estado", "Añil Cambios,Organización,Activo", "Cliente Uno SL,Organización,Activo",
		"Karpo Servicios,Organización,Activo", "Operaciones,Organización,Activo")
	if all := export(actx, "parties", nil); len(all) != 1+7 {
		t.Fatalf("parties: %v", all)
	}
	// Clientes, con el número fiscal que la columna de C# siempre dejaba vacío.
	same("customers", export(actx, "customers", nil), "Nombre,Tipo,CIF/NIF,Estado", "Cliente Uno SL,Organización,A58818501,Activo")

	// Los empleados vienen de HR, con el nombre de Parties.
	same("employees", export(actx, "employees", nil), "Nombre,Número empleado,Fecha contratación,Fecha baja,Activo",
		"\"Ana García, de la O\",P-1,2020-03-01,,Sí", "Eva =Ruiz,P-3,2025-02-03,,Sí", "Luis Pérez,P-2,2021-05-10,2024-12-31,No",
		"Luis Pérez,P-2,2021-05-10,2024-12-31,No")
	same("employees of Karpo still there", export(actx, "employees", map[string]string{"organization": karpo, "active": "true"}),
		"Nombre,Número empleado,Fecha contratación,Fecha baja,Activo", "Eva =Ruiz,P-3,2025-02-03,,Sí")

	// Una fila por rol, y cada relación una vez, desde el lado en que empieza.
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
		case strings.HasPrefix(r, "Organization Rollup,Operaciones,Añil Cambios,"):
			rollup++
		case strings.Contains(r, "Cliente Uno SL") && strings.Contains(r, "Añil Cambios"):
			custom++
		}
	}
	if rels[0] != "Tipo de relación,Participante origen,Participante destino,Fecha inicio,Fecha expiración,Estado,Observaciones" ||
		employment != 4 || rollup != 1 || custom != 1 || len(rels) != 1+6 {
		t.Fatalf("relationships: %v", rels)
	}

	// Quien sólo ve una empresa recibe el fichero de esa empresa, pida lo que pida.
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
	// Pedir otra empresa estrecha lo que se ve, nunca lo amplía: Luis, a quien el administrativo ve
	// porque también trabaja para Karpo; no Ana, que sólo trabaja para la primera empresa.
	same("asking for another company", export(clerk, "persons", map[string]string{"organization": company}), "Nombre,Tipo,Estado",
		"Luis Pérez,Persona,Activo")
	if emp := export(clerk, "employees", nil); len(emp) != 1+2 {
		t.Fatalf("employees the clerk sees: %v", emp)
	}
	// Cada lista pide el permiso de lo que lista.
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
