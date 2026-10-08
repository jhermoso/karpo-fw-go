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
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// financialOf plays Parties: the companies that are financial institutions.
type financialOf map[fw.UUID]bool

var financialRef = mapp.FeatureRef{Kind: mdomain.Capability, Code: "financial"}

func (financialOf) Features() []mapp.FeatureRef { return []mapp.FeatureRef{financialRef} }

func (f financialOf) Of(_ context.Context, organization fw.UUID) ([]mapp.FeatureRef, error) {
	if f[organization] {
		return []mapp.FeatureRef{financialRef}, nil
	}
	return nil, nil
}

func (f financialOf) Holders(context.Context, mapp.FeatureRef) ([]fw.UUID, error) {
	out := []fw.UUID{}
	for id, ok := range f {
		if ok {
			out = append(out, id)
		}
	}
	return out, nil
}

// TestModulesDerivedCapability runs on every engine what happens to an installation that had
// switched the financial capability on by hand when it starts being derived from what the company
// is: what was recorded stays in the table and stops counting, for every way of asking.
func TestModulesDerivedCapability(t *testing.T) {
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
			admin, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "admin", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
			admin.GlobalAdmin = true
			actx := authz.WithContext(ctx, admin)
			shop, bank := fw.NewUUID(), fw.NewUUID()

			// Before: by hand, and to the wrong company.
			before := modules.Compose(sw)
			if _, err := before.EnsureCatalog(ctx); err != nil {
				t.Fatal(err)
			}
			for _, code := range []string{"financial", "logistics"} {
				if _, err := before.Service.Activate.Handle(actx, mapp.SwitchFeature{Organization: shop.String(), Kind: "capability", Code: code}); err != nil {
					t.Fatal(err)
				}
			}
			if ok, _ := before.Features.Has(ctx, shop.String(), "capability", "financial"); !ok {
				t.Fatal("switched on by hand")
			}

			// After: the bank has it and the shop does not, whatever the table says.
			after := modules.Compose(sw, modules.WithDerivation(financialOf{bank: true}))
			for org, want := range map[fw.UUID]bool{shop: false, bank: true} {
				if ok, err := after.Features.Has(ctx, org.String(), "capability", "financial"); err != nil || ok != want {
					t.Fatalf("has: %v %v, want %v", ok, err, want)
				}
			}
			if of, err := after.Features.Of(ctx, shop.String(), "capability"); err != nil || !slices.Equal(of, []string{"logistics"}) {
				t.Fatalf("capabilities of the shop: %v %v", of, err)
			}
			list, err := after.Service.Of.Handle(actx, mapp.ListActivations{Organization: shop.String()})
			if err != nil || len(list) != 1 || list[0].Code != "logistics" {
				t.Fatalf("activations of the shop: %+v %v", list, err)
			}
			list, err = after.Service.Of.Handle(actx, mapp.ListActivations{Organization: bank.String()})
			if err != nil || len(list) != 1 || list[0].Code != "financial" || !list[0].Derived || !list[0].Active {
				t.Fatalf("activations of the bank: %+v %v", list, err)
			}
			orgs, err := after.Service.Organizations.Handle(actx, mapp.ListOrganizations{Kind: "capability", Code: "financial"})
			if err != nil || !slices.Equal(orgs, []string{bank.String()}) {
				t.Fatalf("who has it: %v %v", orgs, err)
			}
			scoped, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "clerk", Kind: authz.Service, Permissions: mapp.Permissions(),
				Grants: []authz.Grant{{OrganizationID: shop, Level: authz.Full}, {OrganizationID: bank, Level: authz.Full}}})
			cur, err := after.Service.Current.Handle(authz.WithContext(ctx, scoped), mapp.GetCurrent{})
			if err != nil || !slices.Equal(cur.Capabilities, []string{"financial", "logistics"}) {
				t.Fatalf("current: %+v %v", cur, err)
			}
			var rv *fw.RuleViolationError
			if _, err := after.Service.Deactivate.Handle(actx, mapp.SwitchFeature{Organization: shop.String(), Kind: "capability", Code: "financial"}); err == nil {
				t.Fatal("what is derived is not switched by hand")
			} else if !errors.As(err, &rv) || rv.Code != "modules.derived" {
				t.Fatalf("want modules.derived: %v", err)
			}
		})
	}
}
