package integration

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/assets"
	asapp "github.com/jhermoso/karpo-fw-go/contexts/assets/application"
	asdomain "github.com/jhermoso/karpo-fw-go/contexts/assets/domain"
	asinfra "github.com/jhermoso/karpo-fw-go/contexts/assets/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/audit"
	happ "github.com/jhermoso/karpo-fw-go/contexts/audit/application"
	"github.com/jhermoso/karpo-fw-go/contexts/modules"
	mapp "github.com/jhermoso/karpo-fw-go/contexts/modules/application"
	mdomain "github.com/jhermoso/karpo-fw-go/contexts/modules/domain"
	minfra "github.com/jhermoso/karpo-fw-go/contexts/modules/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// TestAuditContext reads through Audit, on every engine, the history two contexts keep in their
// SQL audit logs: versions in order, the changed fields with their old and new values as each
// engine gives them back (text and booleans), the events raised, and the guard (the history of
// an asset for who can see it, the catalog of Modules for global administrators).
func TestAuditContext(t *testing.T) {
	for _, e := range engines {
		if e.name == "oracle-dotnet-guids" {
			continue
		}
		t.Run(e.name, func(t *testing.T) {
			db := open(t, e)
			ctx := context.Background()
			asinfra.DropAll(ctx, db)
			minfra.DropAll(ctx, db)
			m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{asinfra.Migrations(), minfra.Migrations()})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := m.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			sw := hotswap.New(db)
			am := assets.Compose(sw)
			mm := modules.Compose(sw)
			hm := audit.Compose().
				Register(asdomain.AssetKind, am.Audit, happ.Seeing(asdomain.ParseAssetID, func(id asdomain.AssetID) asapp.GetAsset { return asapp.GetAsset{ID: id} }, am.Service.GetAsset)).
				Register(mdomain.FeatureKind, mm.Audit, nil)
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			admin, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "admin", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
			admin.GlobalAdmin = true
			actx := authz.WithContext(ctx, admin)
			acme := fw.NewUUID()

			van, err := am.Service.Register.Handle(actx, asapp.RegisterAsset{Company: acme.String(), Code: "FUR-01", Name: "Furgoneta", Class: "vehicles",
				Acquired: vocab.MustDate(2026, 1, 10), InService: vocab.MustDate(2026, 1, 16), Cost: "12000", Residual: "2000", LifeMonths: 48})
			must(err)
			_, err = am.Service.Depreciate.Handle(actx, asapp.RunDepreciation{Company: acme.String(), Year: 2026, Month: 3})
			must(err)
			vid, _ := asdomain.ParseAssetID(van.ID)
			_, err = am.Service.Dispose.Handle(actx, asapp.DisposeAsset{ID: vid, Date: vocab.MustDate(2026, 7, 10), Kind: "sale", Proceeds: "11000"})
			must(err)

			find := func(e happ.EntryDTO, field string) happ.ChangeDTO {
				i := slices.IndexFunc(e.Changes, func(c happ.ChangeDTO) bool { return c.Field == field })
				if i < 0 {
					return happ.ChangeDTO{}
				}
				return e.Changes[i]
			}
			trail, err := hm.Service.Trail.Handle(actx, happ.GetTrail{Type: "assets.asset", ID: van.ID})
			if err != nil || len(trail) != 3 || trail[0].Operation != "created" || trail[0].Version != 1 || trail[1].Version != 2 || trail[2].Version != 3 ||
				trail[0].At == "" || trail[0].Actor == "" {
				t.Fatalf("history: %+v %v", trail, err)
			}
			if c := find(trail[1], "accumulated"); c.Old != "0" || c.New != "524.19" || !slices.Contains(trail[1].Events, "assets.depreciation_charged") {
				t.Fatalf("the quarter: %+v", trail[1])
			}
			if c := find(trail[2], "status"); c.Old != "in-service" || c.New != "disposed" || !slices.Contains(trail[2].Events, "assets.asset_disposed") {
				t.Fatalf("the sale: %+v", trail[2])
			}

			// Someone of the company who can see the asset and read histories; someone of another one.
			member := func(org fw.UUID) context.Context {
				ac, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "member", Kind: authz.Service,
					Permissions: []authz.Permission{happ.PermTrailRead, asapp.PermAssetRead}, Grants: []authz.Grant{{OrganizationID: org, Level: authz.Full}}})
				return authz.WithContext(ctx, ac)
			}
			if own, err := hm.Service.Trail.Handle(member(acme), happ.GetTrail{Type: "assets.asset", ID: van.ID}); err != nil || len(own) != 3 {
				t.Fatalf("a member of the company: %d %v", len(own), err)
			}
			if _, err := hm.Service.Trail.Handle(member(fw.NewUUID()), happ.GetTrail{Type: "assets.asset", ID: van.ID}); !errors.Is(err, fw.ErrNotFound) {
				t.Fatalf("a member of another company: %v", err)
			}

			feature, err := mm.Service.Define.Handle(actx, mapp.DefineFeature{Kind: "module", Code: "tpv", Name: "TPV"})
			must(err)
			fid, _ := mdomain.ParseFeatureID(feature.ID)
			_, err = mm.Service.Change.Handle(actx, mapp.ChangeFeature{ID: fid, Name: "Punto de venta", Retired: true})
			must(err)
			if _, err := hm.Service.Trail.Handle(member(acme), happ.GetTrail{Type: "modules.feature", ID: feature.ID}); err == nil {
				t.Fatal("the catalog has no company: only a global administrator reads its history")
			}
			entries, err := hm.Service.Trail.Handle(actx, happ.GetTrail{Type: "modules.feature", ID: feature.ID})
			if err != nil || len(entries) != 2 || find(entries[1], "name").New != "Punto de venta" || find(entries[1], "retired").Old != false ||
				find(entries[1], "retired").New != true {
				t.Fatalf("history of the catalog: %+v %v", entries, err)
			}
		})
	}
}
