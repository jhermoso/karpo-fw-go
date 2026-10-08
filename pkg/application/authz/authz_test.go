package authz_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
)

var (
	orgA, orgB, orgC = domain.NewUUID(), domain.NewUUID(), domain.NewUUID()
	subject, party   = domain.NewUUID(), domain.NewUUID()
	create           = authz.MustPermission("Parties.Party.Create")
)

func human(t *testing.T, mutate func(*authz.Context)) *authz.Context {
	t.Helper()
	c := authz.Context{Subject: subject, SubjectName: "ana", Kind: authz.Human, ActorPartyID: party,
		Grants:      []authz.Grant{{OrganizationID: orgA, Level: authz.Full}, {OrganizationID: orgB, Level: authz.ReadOnly}},
		Permissions: []authz.Permission{create}}
	if mutate != nil {
		mutate(&c)
	}
	ctx, err := authz.NewContext(c)
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

func TestPermissionCodes(t *testing.T) {
	for code, want := range map[string]bool{
		"Parties.Party.Create": true, "*.*.*": true,
		"Parties.Party": false, "Parties..Create": false, "a.b.c.d": false, "": false,
	} {
		if got := authz.Permission(code).WellFormed(); got != want {
			t.Errorf("%q well formed = %v", code, got)
		}
	}
	if authz.PermissionOf("Parties", "Party", "Create") != create {
		t.Fatal("PermissionOf")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("malformed permission must panic at composition time")
		}
	}()
	authz.MustPermission("Parties.Create")
}

func TestNewContext_Invariants(t *testing.T) {
	_, err := authz.NewContext(authz.Context{Subject: subject, SubjectName: "ana", Kind: authz.Human})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("human without party must fail: %v", err)
	}
	_, err = authz.NewContext(authz.Context{Subject: subject, SubjectName: "ana", Kind: authz.Human, ActorPartyID: party,
		Grants: []authz.Grant{{OrganizationID: orgA}}, EffectiveOrganizations: []domain.UUID{orgC}})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("effective outside grants must fail: %v", err)
	}
	c := human(t, nil)
	if len(c.EffectiveOrganizations) != 2 {
		t.Fatalf("without request the scope is every grant: %v", c.EffectiveOrganizations)
	}
	c = human(t, func(c *authz.Context) { c.RequestedOrganizations = []domain.UUID{orgB, orgC} })
	if len(c.EffectiveOrganizations) != 1 || c.EffectiveOrganizations[0] != orgB {
		t.Fatalf("scope must be grants ∩ requested: %v", c.EffectiveOrganizations)
	}
}

func TestHasPermission(t *testing.T) {
	c := human(t, nil)
	if !c.HasPermission(create) || c.HasPermission("Parties.Party.Delete") || c.HasPermission("Parties.Party") {
		t.Fatal("explicit permissions")
	}
	w := human(t, func(c *authz.Context) { c.Permissions = []authz.Permission{authz.Wildcard} })
	if !w.HasPermission("Any.Thing.Here") || w.HasPermission("malformed") {
		t.Fatal("wildcard grants every well-formed code")
	}
	g := human(t, func(c *authz.Context) { c.GlobalAdmin = true; c.Permissions = nil })
	if !g.HasPermission("Any.Thing.Here") {
		t.Fatal("global admin")
	}
}

func TestAccessLevels_P2(t *testing.T) {
	if authz.ParseAccessLevel("Full") != authz.Full || authz.ParseAccessLevel("ReadOnly") != authz.ReadOnly ||
		authz.ParseAccessLevel("bogus") != authz.Restricted || authz.ParseAccessLevel("") != authz.Restricted {
		t.Fatal("parse: unknown values are Restricted")
	}
	c := human(t, func(c *authz.Context) {
		c.Grants = append(c.Grants, authz.Grant{OrganizationID: orgC, Level: authz.Restricted, IncludeSubsidiaries: true})
	})
	cases := []struct {
		org         domain.UUID
		read, write bool
	}{{orgA, true, true}, {orgB, true, false}, {orgC, true, false}, {domain.NewUUID(), false, false}}
	for i, tc := range cases {
		if c.CanRead(tc.org) != tc.read || c.CanWrite(tc.org) != tc.write {
			t.Errorf("case %d: read=%v write=%v", i, c.CanRead(tc.org), c.CanWrite(tc.org))
		}
	}
	// Full grant outside the requested scope does not allow writing.
	scoped := human(t, func(c *authz.Context) { c.RequestedOrganizations = []domain.UUID{orgB} })
	if scoped.CanWrite(orgA) {
		t.Fatal("writes are limited to the effective scope")
	}
}

func TestRequire(t *testing.T) {
	ctx := context.Background()
	if err := authz.Require(ctx, create); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("no context must be unauthorized: %v", err)
	}
	ctx = authz.WithContext(ctx, human(t, nil))
	if err := authz.Require(ctx, create); err != nil {
		t.Fatal(err)
	}
	if err := authz.Require(ctx, "Parties.Party.Delete"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("missing permission must be forbidden: %v", err)
	}
	if authz.RequireWrite(ctx, orgA) != nil || !errors.Is(authz.RequireWrite(ctx, orgB), domain.ErrForbidden) {
		t.Fatal("RequireWrite")
	}
}

func TestResolutionErrors(t *testing.T) {
	if !errors.Is(authz.Denied(authz.ReasonIncompletePrincipal).Err(), domain.ErrUnauthorized) ||
		!errors.Is(authz.Denied(authz.ReasonInactiveUser).Err(), domain.ErrForbidden) ||
		!errors.Is(authz.Undetermined("down").Err(), authz.ErrIndeterminate) ||
		authz.Allowed(human(t, nil)).Err() != nil {
		t.Fatal("resolution to error mapping")
	}
}

func TestServicePrincipalContext_DropsWildcard(t *testing.T) {
	sp := authz.ServicePrincipal{Subject: domain.NewUUID(), Name: "billing",
		Permissions: []authz.Permission{authz.Wildcard, "Billing.Invoice.Read", "bad"}}
	c, err := authz.ServicePrincipalContext("t", sp, authz.Request{RequestedOrganizations: []domain.UUID{orgA}})
	if err != nil {
		t.Fatal(err)
	}
	if c.GlobalAdmin || c.HasPermission("Parties.Party.Create") || !c.HasPermission("Billing.Invoice.Read") ||
		len(c.EffectiveOrganizations) != 0 || c.Kind != authz.Service {
		t.Fatalf("service principal context: %+v", c)
	}
	if c.Actor().PartyID != sp.Subject || c.Actor().Name != "billing" {
		t.Fatal("service actor")
	}
}

func TestPolicyVersion_IsDeterministic(t *testing.T) {
	g := []authz.Grant{{OrganizationID: orgA, Level: authz.Full}, {OrganizationID: orgB, Level: authz.ReadOnly}}
	a := authz.PolicyVersion(true, []string{"b", "a"}, []authz.Permission{"X.Y.Z", "A.B.C"}, g)
	b := authz.PolicyVersion(true, []string{"a", "b"}, []authz.Permission{"A.B.C", "X.Y.Z"}, []authz.Grant{g[1], g[0]})
	if a != b || len(a) != 64 {
		t.Fatal("order must not change the version")
	}
	if a == authz.PolicyVersion(false, []string{"a", "b"}, []authz.Permission{"A.B.C", "X.Y.Z"}, g) {
		t.Fatal("any relevant change must change the version")
	}
}

type row struct{ Owner domain.UUID }

var owner = spec.Comparable("owner_id", func(r row) domain.UUID { return r.Owner })

func TestScopeSpec(t *testing.T) {
	id := func(u domain.UUID) domain.UUID { return u }
	rows := []row{{orgA}, {orgB}, {orgC}}
	count := func(s spec.Spec[row]) (n int) {
		for _, r := range rows {
			if s.IsSatisfiedBy(r) {
				n++
			}
		}
		return n
	}
	if n := count(authz.ScopeSpec(context.Background(), owner, id)); n != 0 {
		t.Fatalf("without context nothing matches, got %d", n)
	}
	ctx := authz.WithContext(context.Background(), human(t, func(c *authz.Context) { c.RequestedOrganizations = []domain.UUID{orgA} }))
	if n := count(authz.ScopeSpec(ctx, owner, id)); n != 1 {
		t.Fatalf("scope must keep only orgA, got %d", n)
	}
	admin := authz.WithContext(context.Background(), human(t, func(c *authz.Context) { c.GlobalAdmin = true }))
	if n := count(authz.ScopeSpec(admin, owner, id)); n != 3 {
		t.Fatalf("global admin sees everything, got %d", n)
	}
}
