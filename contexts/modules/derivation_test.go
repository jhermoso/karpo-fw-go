package modules_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/modules"
	mapp "github.com/jhermoso/karpo-fw-go/contexts/modules/application"
	"github.com/jhermoso/karpo-fw-go/contexts/modules/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
)

// banks plays Parties: the companies that are financial institutions have the financial capability.
type banks map[fw.UUID]bool

var financial = mapp.FeatureRef{Kind: domain.Capability, Code: "financial"}

func (banks) Features() []mapp.FeatureRef { return []mapp.FeatureRef{financial} }

func (b banks) Of(_ context.Context, organization fw.UUID) ([]mapp.FeatureRef, error) {
	if b[organization] {
		return []mapp.FeatureRef{financial}, nil
	}
	return nil, nil
}

func (b banks) Holders(_ context.Context, f mapp.FeatureRef) ([]fw.UUID, error) {
	out := []fw.UUID{}
	for id, ok := range b {
		if ok && f == financial {
			out = append(out, id)
		}
	}
	return out, nil
}

// A capability a company has because of what it is: nobody switches it, and everybody is told the
// same, whoever asks and however.
func TestModules_DerivedCapability(t *testing.T) {
	ctx := context.Background()
	sw := hotswap.New(memory.NewStore("memory"))
	t.Cleanup(func() { _ = sw.Close(ctx) })
	bank, shop := fw.NewUUID(), fw.NewUUID()
	is := banks{bank: true}
	mm := modules.Compose(sw, modules.WithDerivation(is))
	if _, err := mm.EnsureCatalog(ctx); err != nil {
		t.Fatal(err)
	}
	ac, err := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "manager", Kind: authz.Service, Permissions: mapp.Permissions(),
		Grants: []authz.Grant{{OrganizationID: bank, Level: authz.Full}, {OrganizationID: shop, Level: authz.Full}}})
	if err != nil {
		t.Fatal(err)
	}
	user := authz.WithContext(ctx, ac)
	svc := mm.Service
	violates := func(err error) {
		t.Helper()
		var rv *fw.RuleViolationError
		if !errors.As(err, &rv) || rv.Code != "modules.derived" {
			t.Fatalf("want modules.derived: %v", err)
		}
	}

	// It is not switched by hand, on or off, for the one that has it or the one that does not.
	for _, org := range []fw.UUID{bank, shop} {
		_, err := svc.Activate.Handle(user, mapp.SwitchFeature{Organization: org.String(), Kind: "capability", Code: "Financial"})
		violates(err)
		_, err = svc.Deactivate.Handle(user, mapp.SwitchFeature{Organization: org.String(), Kind: "capability", Code: "financial"})
		violates(err)
	}
	// The rest go on as always.
	if _, err := svc.Activate.Handle(user, mapp.SwitchFeature{Organization: bank.String(), Kind: "module", Code: "ventas"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Activate.Handle(user, mapp.SwitchFeature{Organization: shop.String(), Kind: "capability", Code: "logistics"}); err != nil {
		t.Fatal(err)
	}

	has := func(org fw.UUID) bool {
		t.Helper()
		ok, err := mm.Features.Has(ctx, org.String(), "capability", "financial")
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if !has(bank) || has(shop) {
		t.Fatal("the bank has it, the shop does not")
	}
	if of, err := mm.Features.Of(ctx, bank.String(), "capability"); err != nil || !slices.Equal(of, []string{"financial"}) {
		t.Fatalf("capabilities of the bank: %v %v", of, err)
	}
	if of, err := mm.Features.Of(ctx, shop.String(), "capability"); err != nil || !slices.Equal(of, []string{"logistics"}) {
		t.Fatalf("capabilities of the shop: %v %v", of, err)
	}

	// What the interface asks: the union over the companies of who calls.
	cur, err := svc.Current.Handle(user, mapp.GetCurrent{})
	if err != nil || !slices.Equal(cur.Capabilities, []string{"financial", "logistics"}) || !slices.Equal(cur.Modules, []string{"ventas"}) {
		t.Fatalf("current: %+v %v", cur, err)
	}
	only, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "clerk", Kind: authz.Service, Permissions: mapp.Permissions(),
		Grants: []authz.Grant{{OrganizationID: shop, Level: authz.ReadOnly}}})
	if cur, err := svc.Current.Handle(authz.WithContext(ctx, only), mapp.GetCurrent{}); err != nil || !slices.Equal(cur.Capabilities, []string{"logistics"}) {
		t.Fatalf("current of the shop: %+v %v", cur, err)
	}

	// The list of a company says which it has by being what it is; and which companies have it.
	list, err := svc.Of.Handle(user, mapp.ListActivations{Organization: bank.String()})
	if err != nil || len(list) != 2 || list[0].Code != "financial" || !list[0].Derived || !list[0].Active || list[1].Code != "ventas" || list[1].Derived {
		t.Fatalf("activations of the bank: %+v %v", list, err)
	}
	orgs, err := svc.Organizations.Handle(user, mapp.ListOrganizations{Kind: "capability", Code: "financial"})
	if err != nil || !slices.Equal(orgs, []string{bank.String()}) {
		t.Fatalf("who has it: %v %v", orgs, err)
	}
	if none, err := svc.Organizations.Handle(authz.WithContext(ctx, only), mapp.ListOrganizations{Kind: "capability", Code: "financial"}); err != nil || len(none) != 0 {
		t.Fatalf("outside one's companies: %v %v", none, err)
	}

	// The day the shop becomes a financial institution it has it, with nothing else to do.
	is[shop] = true
	if !has(shop) {
		t.Fatal("the shop is a financial institution now")
	}
	is[bank] = false
	if has(bank) {
		t.Fatal("the bank is not one any more")
	}
}
