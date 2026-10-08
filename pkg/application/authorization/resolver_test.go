package authorization_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jhermoso/karpo-fw-go/pkg/application/authorization"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

func TestResolver(t *testing.T) {
	ctx := context.Background()
	dir := authorization.NewMemoryDirectory()
	org := domain.NewUUID()
	ana, admin, inactive, locked, svc := domain.NewUUID(), domain.NewUUID(), domain.NewUUID(), domain.NewUUID(), domain.NewUUID()
	dir.Put(ana, authz.Subject{Active: true, Permissions: []authz.Permission{"Parties.Party.Create"},
		Grants: []authz.Grant{{OrganizationID: org, Level: authz.Full}}})
	dir.Put(admin, authz.Subject{Active: true, Roles: []string{authorization.DefaultGlobalAdminRole}})
	dir.Put(inactive, authz.Subject{})
	dir.Put(locked, authz.Subject{Active: true, Locked: true})
	r := authorization.NewResolver(dir, authorization.Options{ServicePrincipals: []authz.ServicePrincipal{
		{Subject: svc, Permissions: []authz.Permission{authz.Wildcard, "Billing.Invoice.Read"}}}})

	userP := func(id domain.UUID) authz.Principal {
		return authz.Principal{Subject: id, Name: "u", PartyID: domain.NewUUID(), Kind: authz.Human}
	}
	cases := []struct {
		name    string
		p       authz.Principal
		outcome authz.Outcome
		reason  string
	}{
		{"incomplete", authz.Principal{Subject: ana, Name: "ana", Kind: authz.Human}, authz.Deny, authz.ReasonIncompletePrincipal},
		{"unknown", userP(domain.NewUUID()), authz.Deny, authz.ReasonUnknownSubject},
		{"inactive", userP(inactive), authz.Deny, authz.ReasonInactiveUser},
		{"locked", userP(locked), authz.Deny, authz.ReasonLockedUser},
		{"unknown service", authz.Principal{Subject: domain.NewUUID(), Name: "x", Kind: authz.Service}, authz.Deny, authz.ReasonUnknownService},
		{"user", userP(ana), authz.Allow, ""},
		{"admin", userP(admin), authz.Allow, ""},
		{"service", authz.Principal{Subject: svc, Name: "billing", Kind: authz.Service}, authz.Allow, ""},
	}
	for _, tc := range cases {
		res := r.Resolve(ctx, tc.p, authz.Request{CorrelationID: "c-1"})
		if res.Outcome != tc.outcome || res.Reason != tc.reason || (res.Outcome == authz.Allow) != (res.Context != nil) {
			t.Errorf("%s: %+v", tc.name, res)
		}
	}

	c := r.Resolve(ctx, userP(ana), authz.Request{CorrelationID: "c-1"}).Context
	if c.GlobalAdmin || !c.CanWrite(org) || c.CorrelationID != "c-1" || c.TenantID != "erp-security" || c.PolicyVersion == "" {
		t.Fatalf("user context: %+v", c)
	}
	if !r.Resolve(ctx, userP(admin), authz.Request{}).Context.GlobalAdmin {
		t.Fatal("GlobalSuperAdmin role makes a global admin")
	}
	s := r.Resolve(ctx, authz.Principal{Subject: svc, Name: "billing", Kind: authz.Service}, authz.Request{}).Context
	if s.HasPermission("Parties.Party.Create") || !s.HasPermission("Billing.Invoice.Read") {
		t.Fatal("service principals never receive the wildcard")
	}

	dir.FailWith(errors.New("db down"))
	if res := r.Resolve(ctx, userP(ana), authz.Request{}); res.Outcome != authz.Indeterminate || res.Context != nil {
		t.Fatalf("a failing directory must be indeterminate (fail closed): %+v", res)
	}
}

type fixedAuthenticator struct {
	name string
	err  error
}

func (f fixedAuthenticator) Authenticate(_ context.Context, credentials string) (authz.Principal, error) {
	if credentials == "" {
		return authz.Principal{}, authz.ErrNoCredentials
	}
	return authz.Principal{Name: f.name}, f.err
}

func TestAuthenticators_FirstThatRecognizesTheCredentialsWins(t *testing.T) {
	ctx := context.Background()
	down := errors.New("identity source unavailable")
	own := fixedAuthenticator{name: "own", err: authz.ErrInvalidCredentials}
	external := fixedAuthenticator{name: "external"}

	p, err := authorization.Authenticators(own, external).Authenticate(ctx, "token")
	if err != nil || p.Name != "external" {
		t.Fatalf("the second authenticator recognizes the token: %+v %v", p, err)
	}
	if _, err := authorization.Authenticators(own, external).Authenticate(ctx, ""); !errors.Is(err, authz.ErrNoCredentials) {
		t.Fatalf("no credentials: %v", err)
	}
	if _, err := authorization.Authenticators(own, own).Authenticate(ctx, "token"); !errors.Is(err, authz.ErrInvalidCredentials) {
		t.Fatalf("nobody recognizes the token: %v", err)
	}
	// An outage is never reported as a wrong token (401): the caller sees the failure.
	_, err = authorization.Authenticators(fixedAuthenticator{err: down}, own).Authenticate(ctx, "token")
	if !errors.Is(err, down) || errors.Is(err, authz.ErrInvalidCredentials) {
		t.Fatalf("outage: %v", err)
	}
	if _, err := authorization.Authenticators().Authenticate(ctx, "token"); !errors.Is(err, authz.ErrNoCredentials) {
		t.Fatalf("no authenticators: %v", err)
	}
}
