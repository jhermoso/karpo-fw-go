package integration

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	accinfra "github.com/jhermoso/karpo-fw-go/contexts/accounting/infrastructure"
	astinfra "github.com/jhermoso/karpo-fw-go/contexts/assets/infrastructure"
	bilinfra "github.com/jhermoso/karpo-fw-go/contexts/billing/infrastructure"
	docinfra "github.com/jhermoso/karpo-fw-go/contexts/documents/infrastructure"
	exginfra "github.com/jhermoso/karpo-fw-go/contexts/exchange/infrastructure"
	expapp "github.com/jhermoso/karpo-fw-go/contexts/exports/application"
	expdomain "github.com/jhermoso/karpo-fw-go/contexts/exports/domain"
	expinfra "github.com/jhermoso/karpo-fw-go/contexts/exports/infrastructure"
	facinfra "github.com/jhermoso/karpo-fw-go/contexts/facilities/infrastructure"
	finapp "github.com/jhermoso/karpo-fw-go/contexts/financial/application"
	fininfra "github.com/jhermoso/karpo-fw-go/contexts/financial/infrastructure"
	fisinfra "github.com/jhermoso/karpo-fw-go/contexts/fiscal/infrastructure"
	geoinfra "github.com/jhermoso/karpo-fw-go/contexts/geography/infrastructure"
	hrapp "github.com/jhermoso/karpo-fw-go/contexts/hr/application"
	hrinfra "github.com/jhermoso/karpo-fw-go/contexts/hr/infrastructure"
	impapp "github.com/jhermoso/karpo-fw-go/contexts/imports/application"
	impinfra "github.com/jhermoso/karpo-fw-go/contexts/imports/infrastructure"
	invinfra "github.com/jhermoso/karpo-fw-go/contexts/inventory/infrastructure"
	modinfra "github.com/jhermoso/karpo-fw-go/contexts/modules/infrastructure"
	ordinfra "github.com/jhermoso/karpo-fw-go/contexts/orders/infrastructure"
	parapp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pardomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	parinfra "github.com/jhermoso/karpo-fw-go/contexts/parties/infrastructure"
	payinfra "github.com/jhermoso/karpo-fw-go/contexts/payments/infrastructure"
	prlinfra "github.com/jhermoso/karpo-fw-go/contexts/payroll/infrastructure"
	proinfra "github.com/jhermoso/karpo-fw-go/contexts/products/infrastructure"
	purinfra "github.com/jhermoso/karpo-fw-go/contexts/purchases/infrastructure"
	recinfra "github.com/jhermoso/karpo-fw-go/contexts/receivables/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/security"
	secapp "github.com/jhermoso/karpo-fw-go/contexts/security/application"
	secinfra "github.com/jhermoso/karpo-fw-go/contexts/security/infrastructure"
	shpinfra "github.com/jhermoso/karpo-fw-go/contexts/shipments/infrastructure"
	treinfra "github.com/jhermoso/karpo-fw-go/contexts/treasury/infrastructure"
	wrkinfra "github.com/jhermoso/karpo-fw-go/contexts/work/infrastructure"
	"github.com/jhermoso/karpo-fw-go/host"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// TestHost migrates every context into one database of each engine (no two of them may want the
// same table or index), verifies the schema, starts the host on it and does what only the whole
// does: an import that registers companies in Parties, an account in one of them, its export
// written by the chores, and the messages of every context delivered.
func TestHost(t *testing.T) {
	for _, e := range engines {
		if e.name == "oracle-dotnet-guids" {
			continue
		}
		t.Run(e.name, func(t *testing.T) {
			db := open(t, e)
			ctx := context.Background()
			for _, drop := range []func(context.Context, *sqlrepo.DB){expinfra.DropAll, impinfra.DropAll, modinfra.DropAll, exginfra.DropAll, fininfra.DropAll,
				wrkinfra.DropAll, shpinfra.DropAll, docinfra.DropAll, astinfra.DropAll, accinfra.DropAll, treinfra.DropAll, payinfra.DropAll, purinfra.DropAll,
				bilinfra.DropAll, ordinfra.DropAll, invinfra.DropAll, recinfra.DropAll, prlinfra.DropAll, hrinfra.DropAll, fisinfra.DropAll, proinfra.DropAll,
				secinfra.DropAll, parinfra.DropAll, dropPartiesTables, facinfra.DropAll, geoinfra.DropAll} {
				drop(ctx, db)
			}
			m, err := sqlrepo.NewMigrator(db, host.Migrations())
			if err != nil {
				t.Fatal(err)
			}
			applied, err := m.Migrate(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if len(applied) < 2*len(host.Migrations()) { // every context has its tables and its technical ones at least
				t.Fatalf("migrated %d steps of %d contexts", len(applied), len(host.Migrations()))
			}
			if err := m.Verify(ctx); err != nil {
				t.Fatal(err)
			}

			t.Setenv(security.EnvBootstrapUser, "root")
			t.Setenv(security.EnvBootstrapPassword, "boot-password-0001")
			h, err := host.Compose(hotswap.New(db), host.Options{JWTSecret: []byte("host-integration")})
			if err != nil {
				t.Fatal(err)
			}
			started, err := h.Start(ctx)
			if err != nil || started.Permissions != len(host.Permissions()) || started.Features == 0 || started.Bootstrap != secapp.BootstrapCreated {
				t.Fatalf("started: %+v %v", started, err)
			}
			if again, err := h.Start(ctx); err != nil || again.Features != 0 || again.Bootstrap != secapp.BootstrapNotNeeded {
				t.Fatalf("started again: %+v %v", again, err)
			}

			admin, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "admin", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
			admin.GlobalAdmin = true
			actx := authz.WithContext(ctx, admin)
			run, err := h.Imports.Service.Execute.Handle(actx, impapp.RunImport{Source: "personio", Files: []impapp.FileDTO{{Role: "org-units", Name: "org.csv",
				Content: "unitType,name\nInternalOrganization,Añil Cambios\nInternalOrganization,Karpo Servicios\n"}}})
			if err != nil || run.Status != "succeeded" || run.Counts[0].Created != 2 {
				t.Fatalf("import: %+v %v", run, err)
			}
			again, err := h.Imports.Service.Execute.Handle(actx, impapp.RunImport{Source: "personio", Files: []impapp.FileDTO{{Role: "org-units", Name: "org.csv",
				Content: "unitType,name\nInternalOrganization,añil  cambios\n"}}})
			if err != nil || again.Counts[0].Unchanged != 1 {
				t.Fatalf("import again: %+v %v", again, err)
			}
			companies, err := h.Parties.Organizations.All(ctx)
			if err != nil || len(companies) != 2 {
				t.Fatalf("companies: %+v %v", companies, err)
			}

			// The rest of the organization, through the loaders of the host: departments and people in
			// Parties, a work center in Facilities, employments in HR (one person in two companies).
			org := "unitType,name,parentOrg\nInternalOrganization,Añil Cambios,\nInternalOrganization,Karpo Servicios,\n" +
				"Department,Operaciones,Añil Cambios\nDepartment,Operaciones,Karpo Servicios\nOffice,Oficina Sol,Añil Cambios\n"
			people := "employeeNumber,firstName,lastName,email,gender,legalEntity,hireDate,terminationDate\n" +
				"P-1,Íñigo,Núñez,inigo@anil.test,M,Añil Cambios,2020-03-01,\n" +
				"P-2,Lucía,Pérez,lucia@anil.test,F,Añil / Karpo (PLURIEMPLEO),2021-05-10,2024-12-31\n"
			whole := impapp.RunImport{Source: "personio", Files: []impapp.FileDTO{{Role: "org-units", Name: "org.csv", Content: org},
				{Role: "people", Name: "people.csv", Content: people}}}
			loaded, err := h.Imports.Service.Execute.Handle(actx, whole)
			if err != nil || loaded.Status != "succeeded" {
				t.Fatalf("the whole organization: %+v %v", loaded, err)
			}
			want := map[string]impapp.CountDTO{"legal-entity": {Read: 2, Unchanged: 2}, "department": {Read: 2, Created: 2}, "work-center": {Read: 1, Created: 1},
				"person": {Read: 2, Created: 2}, "employment": {Read: 3, Created: 3}}
			for _, c := range loaded.Counts {
				kind := c.Kind
				c.Kind = ""
				if c != want[kind] {
					t.Fatalf("the whole organization, %s: %+v", kind, c)
				}
			}
			twice, err := h.Imports.Service.Execute.Handle(actx, whole)
			if err != nil || twice.Status != "succeeded" {
				t.Fatalf("again: %+v %v", twice, err)
			}
			for _, c := range twice.Counts {
				if c.Created != 0 || c.Updated != 0 || c.Failed != 0 || c.Unchanged != c.Read {
					t.Fatalf("again, %s: %+v", c.Kind, c)
				}
			}
			jobs, err := h.HR.Service.SearchEmployments.Handle(actx, hrapp.SearchEmployments{Number: "P-2", Size: 10})
			if err != nil || jobs.Total != 2 || jobs.Items[0].Terminated != "2024-12-31" || jobs.Items[0].Hired != "2021-05-10" || jobs.Items[0].Person != jobs.Items[1].Person {
				t.Fatalf("one person in two companies: %+v %v", jobs.Items, err)
			}

			// The finance sector exists only for a financial institution: the role is given in Parties.
			open := finapp.OpenAccount{Company: companies[0].ID, Number: "ES9121000418450200051332", Holder: companies[1].ID, Name: "Cuenta de pago"}
			var rv *fw.RuleViolationError
			if _, err := h.Financial.Service.Open.Handle(actx, open); !errors.As(err, &rv) || rv.Code != "financial.not_an_institution" {
				t.Fatalf("not an institution yet: %v", err)
			}
			bank, _ := pardomain.ParsePartyID(companies[0].ID)
			if _, err := h.Parties.Service.AssignRole.Handle(actx, parapp.AssignRole{PartyID: bank, RoleType: pardomain.RoleFinancialInstitution.String()}); err != nil {
				t.Fatal(err)
			}
			acc, err := h.Financial.Service.Open.Handle(actx, open)
			if err != nil {
				t.Fatal(err)
			}
			job, err := h.Exports.Service.Start.Handle(actx, expapp.StartExport{Dataset: "customer-accounts", Filter: map[string]string{"company": companies[0].ID}})
			if err != nil {
				t.Fatal(err)
			}
			chores, err := h.RunChores(ctx)
			if err != nil || chores.ExportsWritten != 1 {
				t.Fatalf("chores: %+v %v", chores, err)
			}
			jid, _ := expdomain.ParseJobID(job.ID)
			file, err := h.Exports.Service.Download.Handle(actx, expapp.DownloadJob{ID: jid})
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(file.Content)
			file.Content.Close()
			if !strings.Contains(string(body), acc.Number+",Cuenta de pago,EUR,active,"+companies[1].ID) {
				t.Fatalf("file: %q", body)
			}
			if idle, err := h.RunChores(ctx); err != nil || idle != (host.Chores{}) {
				t.Fatalf("nothing left to do: %+v %v", idle, err)
			}
			moved, err := h.Deliver(ctx)
			if err != nil || moved < 5 {
				t.Fatalf("delivered: %d %v", moved, err)
			}
			if moved, err = h.Deliver(ctx); err != nil || moved != 0 {
				t.Fatalf("delivered twice: %d %v", moved, err)
			}
		})
	}
}
