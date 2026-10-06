package integration

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/modules"
	mapp "github.com/jhermoso/karpo-fw-go/contexts/modules/application"
	mdomain "github.com/jhermoso/karpo-fw-go/contexts/modules/domain"
	minfra "github.com/jhermoso/karpo-fw-go/contexts/modules/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/messaging/inprocess"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// TestModulesContext runs Modules on every engine: the seed catalog added once, the catalog in
// order and its unique codes, activations with their timestamps round trip, the single sector of
// a company, retirement, what a scoped caller and a global administrator see, the port other
// contexts ask and the Published Language in the outbox.
func TestModulesContext(t *testing.T) {
	for _, e := range engines {
		if e.name == "oracle-dotnet-guids" {
			continue
		}
		t.Run(e.name, func(t *testing.T) {
			db := open(t, e)
			ctx := context.Background()
			minfra.DropAll(ctx, db)
			m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{minfra.Migrations()})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := m.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			sw := hotswap.New(db)
			mm := modules.Compose(sw)
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			violates := func(err error, code string) {
				t.Helper()
				var rv *fw.RuleViolationError
				if !errors.As(err, &rv) || rv.Code != code {
					t.Fatalf("want %s: %v", code, err)
				}
			}
			admin, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "admin", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
			admin.GlobalAdmin = true
			actx := authz.WithContext(ctx, admin)
			svc := mm.Service
			acme, globex := fw.NewUUID(), fw.NewUUID()

			if n, err := mm.EnsureCatalog(ctx); err != nil || n != len(mdomain.Seed) {
				t.Fatalf("seed: %d %v", n, err)
			}
			if n, err := mm.EnsureCatalog(ctx); err != nil || n != 0 {
				t.Fatalf("seed again: %d %v", n, err)
			}
			_, err = svc.Define.Handle(actx, mapp.DefineFeature{Kind: "module", Code: "VENTAS", Name: "Otra"})
			violates(err, "modules.duplicate_feature")
			for _, s := range []string{"retail", "hospitality"} {
				_, err := svc.Define.Handle(actx, mapp.DefineFeature{Kind: "sector", Code: s, Name: s, Description: "Sector " + s})
				must(err)
			}
			catalog, err := svc.Catalog.Handle(actx, mapp.ListCatalog{})
			if err != nil || len(catalog) != 14 || catalog[0].Code != "consulting" || catalog[4].Code != "compras" || catalog[12].Kind != "sector" ||
				catalog[12].Description != "Sector hospitality" || catalog[0].Description != "" {
				t.Fatalf("catalog: %+v %v", catalog, err)
			}

			on := func(org fw.UUID, kind, code, notes string) mapp.SwitchFeature {
				return mapp.SwitchFeature{Organization: org.String(), Kind: kind, Code: code, Notes: notes}
			}
			_, err = svc.Activate.Handle(actx, on(acme, "module", "nominas", ""))
			violates(err, "modules.unknown_feature")
			a, err := svc.Activate.Handle(actx, on(acme, "module", "ventas", "Contrato 2026"))
			if err != nil || !a.Active || a.ActivatedBy != "admin" || a.ActivatedAt == "" || a.Notes != "Contrato 2026" || a.Version != 1 {
				t.Fatalf("on: %+v %v", a, err)
			}
			if again, err := svc.Activate.Handle(actx, on(acme, "module", "ventas", "")); err != nil || again.Version != 1 {
				t.Fatalf("on already: %+v %v", again, err)
			}
			for _, f := range []mapp.SwitchFeature{on(acme, "module", "inventario", ""), on(acme, "capability", "logistics", ""), on(acme, "sector", "retail", ""),
				on(acme, "sector", "hospitality", ""), on(globex, "module", "ventas", ""), on(globex, "module", "crm", "")} {
				_, err := svc.Activate.Handle(actx, f)
				must(err)
			}
			off, err := svc.Deactivate.Handle(actx, on(acme, "module", "inventario", ""))
			if err != nil || off.Active || off.DeactivatedBy != "admin" || off.DeactivatedAt == "" || off.ActivatedAt == "" {
				t.Fatalf("off: %+v %v", off, err)
			}
			_, err = svc.Deactivate.Handle(actx, on(acme, "module", "crm", ""))
			violates(err, "modules.not_active")
			iid, _ := mdomain.ParseFeatureID(catalog[slices.IndexFunc(catalog, func(f mapp.FeatureDTO) bool { return f.Code == "inventario" })].ID)
			retired, err := svc.Change.Handle(actx, mapp.ChangeFeature{ID: iid, Name: "Inventario", Retired: true})
			if err != nil || !retired.Retired || retired.Version != 2 {
				t.Fatalf("retire: %+v %v", retired, err)
			}
			_, err = svc.Activate.Handle(actx, on(acme, "module", "inventario", ""))
			violates(err, "modules.retired_feature")

			acts, err := svc.Of.Handle(actx, mapp.ListActivations{Organization: acme.String()})
			if err != nil || len(acts) != 5 || acts[0].Kind != "capability" || acts[1].Code != "inventario" || acts[1].Active || acts[2].Code != "ventas" ||
				acts[2].Notes != "Contrato 2026" || acts[3].Code != "hospitality" || !acts[3].Active || acts[4].Code != "retail" || acts[4].Active ||
				acts[4].DeactivatedAt == "" {
				t.Fatalf("activations of Acme: %+v %v", acts, err)
			}
			orgs, err := svc.Organizations.Handle(actx, mapp.ListOrganizations{Kind: "module", Code: "ventas"})
			want := []string{acme.String(), globex.String()}
			slices.Sort(want)
			if err != nil || !slices.Equal(orgs, want) {
				t.Fatalf("who has sales: %v %v", orgs, err)
			}
			// A caller of Acme and Globex sees the union; a global administrator, what is offered.
			member, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "member", Kind: authz.Service,
				Grants: []authz.Grant{{OrganizationID: acme, Level: authz.Full}, {OrganizationID: globex, Level: authz.Full}}})
			cur, err := svc.Current.Handle(authz.WithContext(ctx, member), mapp.GetCurrent{})
			if err != nil || !slices.Equal(cur.Modules, []string{"crm", "ventas"}) || !slices.Equal(cur.Capabilities, []string{"logistics"}) ||
				!slices.Equal(cur.Sectors, []string{"hospitality"}) {
				t.Fatalf("current of a member: %+v %v", cur, err)
			}
			all, err := svc.Current.Handle(actx, mapp.GetCurrent{})
			if err != nil || len(all.Modules) != 7 || len(all.Capabilities) != 4 || len(all.Sectors) != 2 {
				t.Fatalf("current of a global administrator: %+v %v", all, err)
			}
			if has, err := mm.Features.Has(ctx, acme.String(), "module", "ventas"); err != nil || !has {
				t.Fatalf("port: %v %v", has, err)
			}
			if of, err := mm.Features.Of(ctx, globex.String(), "module"); err != nil || !slices.Equal(of, []string{"crm", "ventas"}) {
				t.Fatalf("port: %v %v", of, err)
			}
			// Seven switched on, plus retail and inventario switched off.
			if n, err := mm.Relay(inprocess.NewBroker()).RelayOnce(ctx); err != nil || n != 9 {
				t.Fatalf("published: %d %v", n, err)
			}
		})
	}
}
