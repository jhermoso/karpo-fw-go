package domain_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/security/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

func party() domain.PartyID { return domain.PartyID{UUID: fw.NewUUID()} }

func org() domain.OrganizationID { return domain.OrganizationID{UUID: fw.NewUUID()} }

func newUser(t *testing.T, name string, accesses ...domain.OrganizationAccess) *domain.User {
	t.Helper()
	u, err := domain.RegisterUser(domain.NewUserID(), party(), name)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range accesses {
		if err := u.GrantAccess(a); err != nil {
			t.Fatal(err)
		}
	}
	return u
}

func full(o domain.OrganizationID) domain.OrganizationAccess {
	return domain.OrganizationAccess{Organization: o, Level: domain.Full}
}

func eventTypes(evts []fw.Event) string {
	names := make([]string, len(evts))
	for i, e := range evts {
		names[i] = strings.TrimPrefix(e.EventType(), "security.")
	}
	return strings.Join(names, ",")
}

func TestUsername(t *testing.T) {
	for _, ok := range []string{"ana", "ana.garcia", "Ana_García-2", "ana@example.com", "a+b@x.io"} {
		if _, err := domain.NormalizeUsername(ok); err != nil {
			t.Errorf("%q should be valid: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "an", "ana garcia", ".ana", "ana;drop", strings.Repeat("a", 101)} {
		if _, err := domain.NormalizeUsername(bad); !errors.Is(err, fw.ErrValidation) {
			t.Errorf("%q should be rejected: %v", bad, err)
		}
	}
	// C# kept "Admin" and "admin" as two users; here the lookup key ignores case.
	if domain.UsernameKey(" Admin ") != domain.UsernameKey("admin") {
		t.Fatal("user names are unique ignoring case")
	}
	if _, err := domain.RegisterUser(domain.NewUserID(), domain.PartyID{}, "ana"); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("a user is a party: %v", err)
	}
}

func TestPasswordPolicy(t *testing.T) {
	if err := domain.CheckPassword("correct horse battery", "ana"); err != nil {
		t.Fatal(err)
	}
	for name, p := range map[string]string{"short": "elevenchars", "long": strings.Repeat("x", 129), "username": "Ana.Garcia.Lopez"} {
		if err := domain.CheckPassword(p, "ana.garcia.lopez"); !errors.Is(err, fw.ErrValidation) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestLockout(t *testing.T) {
	u := newUser(t, "ana")
	policy := domain.LockoutPolicy{MaxAttempts: 3, Duration: 15 * time.Minute}
	now := time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)
	if u.RecordFailedLogin(policy, now) || u.RecordFailedLogin(policy, now) {
		t.Fatal("two failures do not lock with a limit of three")
	}
	if !u.RecordFailedLogin(policy, now) || !u.IsLockedAt(now.Add(14*time.Minute)) || u.CanSignInAt(now) {
		t.Fatal("the third failure locks for 15 minutes")
	}
	if u.IsLockedAt(now.Add(15 * time.Minute)) {
		t.Fatal("the lock ends after its duration")
	}
	// An expired lock starts a new count: in C# the counter stayed at the limit and the next
	// failure locked again at once.
	later := now.Add(16 * time.Minute)
	if u.RecordFailedLogin(policy, later) || u.FailedAttempts() != 1 {
		t.Fatalf("after the lock expires the count restarts: %d", u.FailedAttempts())
	}
	u.Unlock()
	if u.FailedAttempts() != 0 || u.LockedUntil() != nil {
		t.Fatal("unlock forgets the failures")
	}
	u.RecordLogin(later)
	if u.LastLoginAt() == nil || !u.LastLoginAt().Equal(later) {
		t.Fatal("a login is recorded")
	}
	if got := eventTypes(u.PendingEvents()); got != "user_registered,login_failed,login_failed,login_failed,user_locked,login_failed,user_unlocked,user_logged_in" {
		t.Fatalf("events: %s", got)
	}
}

func TestPasswordAndActivation(t *testing.T) {
	u := newUser(t, "ana")
	if u.HasPassword() || u.SetPassword("", false, false) == nil {
		t.Fatal("a user starts without password and an empty hash is rejected")
	}
	u.RecordFailedLogin(domain.LockoutPolicy{MaxAttempts: 1, Duration: time.Hour}, fw.Now())
	if err := u.SetPassword("hash-1", true, true); err != nil {
		t.Fatal(err)
	}
	if !u.MustChangePassword() || u.IsLockedAt(fw.Now()) || u.FailedAttempts() != 0 {
		t.Fatal("a reset forces the change and lifts the lock")
	}
	u.UpgradePasswordHash("hash-2")
	if u.PasswordHash() != "hash-2" || !u.MustChangePassword() {
		t.Fatal("upgrading the hash is not a password change")
	}
	if _, ok := u.AuditSnapshot()["passwordHash"]; ok || strings.Contains(strings.Join(slices.Collect(mapValues(u.AuditSnapshot())), " "), "hash-2") {
		t.Fatal("the audit snapshot never carries the hash")
	}
	u.Deactivate()
	u.Deactivate()
	if u.CanSignInAt(fw.Now()) || u.SetPassword("hash-3", false, false) == nil || u.GrantAccess(full(org())) == nil ||
		u.Rename("ana.g") == nil || u.LinkIdentity(domain.ExternalIdentity{Issuer: "i", Subject: "s"}) == nil {
		t.Fatal("an inactive user cannot sign in nor be changed")
	}
	u.Activate()
	if !u.IsActive() || !strings.HasSuffix(eventTypes(u.PendingEvents()), "user_activation_changed,user_activation_changed") {
		t.Fatalf("one event per real transition: %s", eventTypes(u.PendingEvents()))
	}
}

func mapValues(m map[string]any) func(func(string) bool) {
	return func(yield func(string) bool) {
		for _, v := range m {
			if s, ok := v.(string); ok && !yield(s) {
				return
			}
		}
	}
}

func TestAccessesAndIdentitiesAreUnique(t *testing.T) {
	acme := org()
	u := newUser(t, "ana", full(acme))
	if err := u.GrantAccess(full(acme)); err != nil || len(u.Accesses()) != 1 {
		t.Fatal("granting the same access twice changes nothing")
	}
	if err := u.GrantAccess(domain.OrganizationAccess{Organization: acme, Level: domain.ReadOnly}); err != nil || u.Accesses()[0].Level != domain.ReadOnly {
		t.Fatal("granting again changes the level")
	}
	if err := u.GrantAccess(domain.OrganizationAccess{Organization: acme, Level: "Owner"}); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("the level is not free text: %v", err)
	}
	if !u.RevokeAccess(acme) || u.RevokeAccess(acme) {
		t.Fatal("revoke reports whether there was an access")
	}
	id := domain.ExternalIdentity{Issuer: "https://id.example", Subject: "8f2c"}
	_ = u.LinkIdentity(id)
	_ = u.LinkIdentity(id)
	if len(u.Identities()) != 1 || !domain.WithIdentity(id).IsSatisfiedBy(u) {
		t.Fatal("an identity is linked once")
	}
	if err := u.LinkIdentity(domain.ExternalIdentity{Issuer: "https://id.example"}); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("an identity needs issuer and subject: %v", err)
	}
	if !u.UnlinkIdentity(id) || len(u.Identities()) != 0 {
		t.Fatal("unlink")
	}
	if got := eventTypes(u.PendingEvents()); got != "user_registered,access_granted,access_changed,access_revoked,identity_linked,identity_unlinked" {
		t.Fatalf("events: %s", got)
	}
	// Duplicates cannot come from storage either.
	_, err := domain.ReconstituteUser(u.ID(), domain.UserState{Party: u.Party(), Username: "ana", Active: true,
		Accesses: []domain.OrganizationAccess{full(acme), full(acme)}})
	if !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("duplicated accesses: %v", err)
	}
}

func TestCatalogAndSystemRoles(t *testing.T) {
	codes := []domain.Permission{domain.Wildcard, domain.PermUserRead, domain.PermUserUpdate, domain.PermRoleRead, domain.PermRoleUpdate,
		"Parties.Party.Read", "Parties.Party.Create", "Parties.Party.Update", "Parties.PartyRole.Assign", "Payroll.Payslip.Approve"}
	c := domain.NewCatalog(codes...)
	want := map[domain.RoleID][]domain.Permission{
		domain.RoleGlobalSuperAdmin: {domain.Wildcard},
		// Everything but the wildcard and what is reserved to the global administrator.
		domain.RoleOrganizationAdmin: {"Parties.Party.Create", "Parties.Party.Read", "Parties.Party.Update", "Parties.PartyRole.Assign",
			"Payroll.Payslip.Approve", domain.PermRoleRead, domain.PermUserRead, domain.PermUserUpdate},
		// Read, Create and Update outside Security: approving a payslip or assigning a party role is not ordinary work.
		domain.RoleStandardUser: {"Parties.Party.Create", "Parties.Party.Read", "Parties.Party.Update"},
		domain.RoleReadOnlyUser: {"Parties.Party.Read", domain.PermRoleRead, domain.PermUserRead},
		domain.RoleCustomer:     nil,
	}
	for _, seed := range domain.SystemRoles() {
		r, err := domain.NewSystemRole(seed, c)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(r.Permissions(), want[seed.ID]) {
			t.Errorf("%s: %v", seed.Name, r.Permissions())
		}
		if r.Describe("Other", "") == nil || r.SetPermissions(nil) == nil || r.Retire() == nil {
			t.Errorf("%s: a system role cannot be edited nor retired", seed.Name)
		}
		if r.IsGlobalAdmin() != (seed.ID == domain.RoleGlobalSuperAdmin) {
			t.Errorf("%s: global admin", seed.Name)
		}
	}
	if len(domain.SystemRoles()) != 5 || domain.RoleGlobalSuperAdmin.String() != "20000000-0000-0000-0001-000000000001" ||
		domain.RoleCustomer.String() != "20000000-0000-0000-0001-000000000005" ||
		domain.BootstrapAdminUser.String() != "20000000-0000-0000-0002-000000000003" {
		t.Fatal("the seeded identities are the C# ones")
	}

	std, _ := domain.NewSystemRole(domain.SystemRoles()[2], c)
	grown := domain.NewCatalog(append(codes, "Purchases.Invoice.Read", "Purchases.Invoice.Register")...)
	if !std.SyncSystemPermissions(grown) || std.SyncSystemPermissions(grown) || !slices.Contains(std.Permissions(), "Purchases.Invoice.Read") ||
		slices.Contains(std.Permissions(), "Purchases.Invoice.Register") {
		t.Fatalf("a new context reaches the system roles through the rule: %v", std.Permissions())
	}

	if err := c.Check([]domain.Permission{"Parties.Party.Read", "Orders.Order.Read"}); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("unknown permission: %v", err)
	}
	// The identity of a permission is its code: the derived storage id is stable.
	if domain.PermissionIDOf("Parties.Party.Read") != domain.PermissionIDOf("Parties.Party.Read") ||
		domain.PermissionIDOf("Parties.Party.Read") == domain.PermissionIDOf("Parties.Party.Create") {
		t.Fatal("permission ids derive from the code")
	}
	for _, bad := range []domain.Permission{"", "Parties.Party", "Parties..Read", "Parties.Party.Read.More", " Parties.Party.Read"} {
		if bad.WellFormed() {
			t.Errorf("%q is malformed", bad)
		}
	}
	if _, err := domain.DeclarePermission("Parties.Party", ""); !errors.Is(err, fw.ErrValidation) {
		t.Fatal("a malformed code is not declared")
	}
}

func TestCustomRole(t *testing.T) {
	r, err := domain.DefineRole(domain.NewRoleID(), "  Purchasing   clerk ", "Registers supplier invoices",
		[]domain.Permission{"Purchases.Invoice.Register", "Purchases.Invoice.Read", "Purchases.Invoice.Read"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Name() != "Purchasing clerk" || r.NameKey() != "purchasing clerk" || !slices.Equal(r.Permissions(),
		[]domain.Permission{"Purchases.Invoice.Read", "Purchases.Invoice.Register"}) {
		t.Fatalf("normalized: %q %v", r.Name(), r.Permissions())
	}
	// Only the GlobalSuperAdmin system role carries the wildcard: a custom role with it was the
	// way around the C# check by role name.
	if err := r.SetPermissions([]domain.Permission{domain.Wildcard}); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("wildcard: %v", err)
	}
	if _, err := domain.DefineRole(domain.NewRoleID(), "Super", "", []domain.Permission{domain.Wildcard}); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("wildcard on define: %v", err)
	}
	if err := r.SetPermissions([]domain.Permission{"Purchases.Invoice.Read"}); err != nil {
		t.Fatal(err)
	}
	_ = r.SetPermissions([]domain.Permission{"Purchases.Invoice.Read"})
	if err := r.Describe("Purchasing", ""); err != nil {
		t.Fatal(err)
	}
	if err := r.Retire(); err != nil {
		t.Fatal(err)
	}
	if got := eventTypes(r.PendingEvents()); got != "role_defined,role_permissions_changed,role_described,role_retired" {
		t.Fatalf("events: %s", got)
	}
	if _, err := domain.DefineRole(domain.NewRoleID(), " ", "", nil); !errors.Is(err, fw.ErrValidation) {
		t.Fatal("a role needs a name")
	}
}

// The three rules of delegated administration (decision 5), with the example of the document:
// Marta administers Acme; Pedro works in Beta.
func TestDelegatedAdministration(t *testing.T) {
	acme, beta := org(), org()
	clerkRole, _ := domain.DefineRole(domain.NewRoleID(), "Clerk", "", []domain.Permission{"Parties.Party.Read"})
	payrollRole, _ := domain.DefineRole(domain.NewRoleID(), "Payroll", "", []domain.Permission{"Payroll.Payslip.Approve"})
	superRole, _ := domain.NewSystemRole(domain.SystemRoles()[0], domain.NewCatalog(domain.Wildcard))

	martaUser := newUser(t, "marta", full(acme))
	marta := domain.Administrator{User: martaUser.ID(), Permissions: []domain.Permission{"Parties.Party.Read", domain.PermUserUpdate},
		Organizations: []domain.OrganizationID{acme}, Full: []domain.OrganizationID{acme}}
	global := domain.Administrator{User: domain.NewUserID(), Global: true}

	// Rule 1: nobody grants what they do not have.
	if marta.CanAssign(clerkRole) != nil {
		t.Fatal("Marta holds every permission of Clerk")
	}
	for _, r := range []*domain.Role{payrollRole, superRole} {
		if err := marta.CanAssign(r); !errors.Is(err, fw.ErrForbidden) {
			t.Fatalf("%s carries permissions Marta does not have: %v", r.Name(), err)
		}
	}
	if global.CanAssign(superRole) != nil || global.CanGrant(beta) != nil {
		t.Fatal("the global administrator assigns any role and grants any organization")
	}
	if marta.CanGrant(acme) != nil || !errors.Is(marta.CanGrant(beta), fw.ErrForbidden) {
		t.Fatal("Marta grants Acme, not Beta")
	}
	viewer := domain.Administrator{User: domain.NewUserID(), Organizations: []domain.OrganizationID{acme}}
	if !errors.Is(viewer.CanGrant(acme), fw.ErrForbidden) {
		t.Fatal("a read-only access does not grant")
	}

	// Rule 2: you administer whom you fully encompass.
	colleague := newUser(t, "carlos", full(acme))
	pedro := newUser(t, "pedro", full(beta))
	nobody := newUser(t, "nuevo")
	if marta.CanAdminister(colleague) != nil || !marta.Sees(colleague) {
		t.Fatal("Marta administers the users of Acme")
	}
	if marta.Sees(pedro) || marta.Sees(nobody) {
		t.Fatal("Marta does not see users without an access to Acme")
	}
	// Marta may give Pedro access to Acme (and take it back)...
	if err := marta.CanChangeAccess(pedro, acme); err != nil {
		t.Fatal(err)
	}
	_ = pedro.GrantAccess(domain.OrganizationAccess{Organization: acme, Level: domain.ReadOnly})
	// ...and now she sees him, but still cannot administer him: she does not encompass Beta.
	// In C#, after that grant she could edit him, strip his roles or delete him.
	if !marta.Sees(pedro) || !errors.Is(marta.CanAdminister(pedro), fw.ErrForbidden) {
		t.Fatal("sharing one organization is not enough to administer a user")
	}
	if !errors.Is(marta.CanAdminister(nobody), fw.ErrForbidden) || global.CanAdminister(nobody) != nil {
		t.Fatal("a user without accesses belongs to the global administrator")
	}
	if !errors.Is(viewer.CanAdminister(colleague), fw.ErrForbidden) {
		t.Fatal("a read-only access does not administer (C# only checked the read scope)")
	}
	if domain.AdministrableBy().IsSatisfiedBy(colleague) || domain.VisibleTo().IsSatisfiedBy(colleague) {
		t.Fatal("without organizations nothing is administrable nor visible (fail closed)")
	}

	// Rule 3: nobody administers themselves, the global administrator included.
	if !errors.Is(marta.CanAdminister(martaUser), fw.ErrForbidden) || !errors.Is(marta.CanChangeAccess(martaUser, acme), fw.ErrForbidden) {
		t.Fatal("Marta cannot administer her own user nor grant herself organizations")
	}
	self := newUser(t, "root")
	if root := (domain.Administrator{User: self.ID(), Global: true}); !errors.Is(root.CanAdminister(self), fw.ErrForbidden) {
		t.Fatal("a global administrator does not administer itself either")
	}

	// ...and one global administrator always remains: the specification finds the others.
	_ = self.AssignRole(superRole)
	other := newUser(t, "root2")
	_ = other.AssignRole(superRole)
	others := domain.GlobalAdministrators(self.ID())
	if others.IsSatisfiedBy(self) || !others.IsSatisfiedBy(other) || others.IsSatisfiedBy(colleague) {
		t.Fatal("the other active global administrators")
	}
	other.Deactivate()
	if others.IsSatisfiedBy(other) {
		t.Fatal("an inactive global administrator does not count")
	}
}

func TestRolesOfAUser(t *testing.T) {
	u := newUser(t, "ana")
	clerk, _ := domain.DefineRole(domain.NewRoleID(), "Clerk", "", nil)
	_ = u.AssignRole(clerk)
	_ = u.AssignRole(clerk)
	if len(u.Roles()) != 1 || !domain.HoldingRole(clerk.ID()).IsSatisfiedBy(u) || domain.HoldingRole().IsSatisfiedBy(u) {
		t.Fatal("a role is assigned once")
	}
	if !u.RevokeRole(clerk.ID()) || u.RevokeRole(clerk.ID()) || u.HasRole(clerk.ID()) {
		t.Fatal("revoke")
	}
	if got := eventTypes(u.PendingEvents()); got != "user_registered,user_role_assigned,user_role_revoked" {
		t.Fatalf("events: %s", got)
	}
}

func TestSession(t *testing.T) {
	now := time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)
	user, family := domain.NewUserID(), fw.NewUUID()
	s, err := domain.OpenSession(user, family, "refresh-token", now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if s.TokenHash() == "refresh-token" || s.TokenHash() != domain.HashToken("refresh-token") || len(s.TokenHash()) != 64 {
		t.Fatal("only the hash of the token is kept")
	}
	if !s.UsableAt(now) || s.UsableAt(now.Add(time.Hour)) {
		t.Fatal("a session is usable until it expires")
	}
	if !domain.SessionOfToken("refresh-token").IsSatisfiedBy(s) || !domain.LiveSessionsOf(user).IsSatisfiedBy(s) ||
		!domain.LiveSessionsOfFamily(family).IsSatisfiedBy(s) {
		t.Fatal("specifications")
	}
	if !s.End(domain.EndedRotated, now) || s.End(domain.EndedLogout, now) || s.Reason() != domain.EndedRotated {
		t.Fatal("a session ends once")
	}
	if s.UsableAt(now) || !s.WasRotated() || domain.LiveSessionsOf(user).IsSatisfiedBy(s) {
		t.Fatal("presenting a rotated token again is a reuse")
	}
	if _, err := domain.OpenSession(user, fw.UUID{}, "t", now, time.Hour); !errors.Is(err, fw.ErrValidation) {
		t.Fatal("a session needs a family")
	}
}
