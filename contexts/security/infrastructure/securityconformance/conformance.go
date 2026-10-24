// Package securityconformance comprueba los mapeos SQL del contexto Security sobre cualquier
// motor: cada especificación que usa el contexto tiene que devolver en la base de datos
// exactamente lo que devuelve en memoria, los agregados tienen que sobrevivir a un viaje de ida y
// vuelta, y el esquema tiene que imponer la unicidad en la que se apoya el dominio. Se ejecuta
// sobre SQLite con las pruebas del contexto y sobre los otros cuatro motores en el módulo de
// integración.
package securityconformance

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/security/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/security/infrastructure"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

func ids[ID fw.Identifier, T fw.AggregateRoot[ID]](t *testing.T, what string, xs []T, err error) []string {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = x.ID().String()
	}
	slices.Sort(out)
	return out
}

// same comprueba que una especificación da los mismos agregados en SQL y en memoria.
func same[ID fw.Identifier, T fw.AggregateRoot[ID]](t *testing.T, name string, ctx context.Context, sql, mem fw.Repository[ID, T], s spec.Specification[T], want int) {
	t.Helper()
	got, err := sql.Find(ctx, s)
	inSQL := ids(t, name+" in SQL", got, err)
	expected, err := mem.Find(ctx, s)
	inMemory := ids(t, name+" in memory", expected, err)
	if !slices.Equal(inSQL, inMemory) {
		t.Errorf("%s: SQL returns %v, memory %v", name, inSQL, inMemory)
	}
	if want >= 0 && len(inSQL) != want {
		t.Errorf("%s: %d aggregates, want %d", name, len(inSQL), want)
	}
	n, err := sql.Count(ctx, s)
	if err != nil || int(n) != len(inSQL) {
		t.Errorf("%s: count %d %v, found %d", name, n, err, len(inSQL))
	}
}

func userState(u *domain.User) string {
	snap := u.AuditSnapshot()
	keys := make([]string, 0, len(snap))
	for k := range snap {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k + "=")
		b.WriteString(strings.TrimSpace(strings.ReplaceAll(toString(snap[k]), "\n", " ")) + ";")
	}
	stamp := func(t *time.Time) string {
		if t == nil {
			return "-"
		}
		return t.UTC().Format(time.RFC3339)
	}
	b.WriteString("hash=" + u.PasswordHash() + ";failed=" + toString(u.FailedAttempts()) + ";locked=" + stamp(u.LockedUntil()) +
		";login=" + stamp(u.LastLoginAt()))
	for _, a := range u.Accesses() {
		b.WriteString(";subs:" + a.Organization.String() + "=" + toString(a.IncludeSubsidiaries))
	}
	return b.String()
}

func toString(v any) string { return fmt.Sprint(v) }

// GrantedBackfill comprueba la migración 4 (D-98) sobre una base que ya tenía las tres primeras: lo
// concedido a cada rol del sistema arranca como los permisos que llevaba entonces, no como lo que
// calcula la regla, y un rol propio no recibe nada. Empieza borrando el esquema de Security de db y
// lo deja migrado del todo.
func GrantedBackfill(t *testing.T, db *sqlrepo.DB) {
	t.Helper()
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	infrastructure.DropAll(ctx, db)
	all := infrastructure.Migrations()
	before := sqlrepo.MigrationSet{Context: all.Context, Migrations: all.Migrations[:3]}
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{before})
	must(err)
	_, err = m.Migrate(ctx)
	must(err)
	// Datos de antes: OrganizationAdmin sin un permiso que la regla le da, y un rol propio.
	_, err = db.Delete(ctx, "sec_role_permissions", sqlrepo.Values{"role_id": domain.RoleOrganizationAdmin, "code": string(domain.PermUserRead)})
	must(err)
	clerk := domain.NewRoleID()
	must(db.Insert(ctx, "sec_roles", sqlrepo.Values{"id": clerk, "version": int64(1), "name": "Clerk", "name_key": "clerk", "system_role": false}))
	must(db.Insert(ctx, "sec_role_permissions", sqlrepo.Values{"role_id": clerk, "code": string(domain.PermUserRead)}))

	m, err = infrastructure.Migrator(db)
	must(err)
	_, err = m.Migrate(ctx)
	must(err)
	must(m.Verify(ctx))
	roles := sqlrepo.MustRepository(db, infrastructure.RoleMapping())
	for _, seed := range domain.SystemRoles() {
		r, err := roles.Get(ctx, seed.ID)
		must(err)
		if !slices.Equal(r.Granted(), r.Permissions()) {
			t.Errorf("%s: granted %v, holds %v", seed.Name, r.Granted(), r.Permissions())
		}
	}
	if admin, err := roles.Get(ctx, domain.RoleOrganizationAdmin); err != nil || slices.Contains(admin.Granted(), domain.PermUserRead) {
		t.Errorf("what the role held, not what the rule computes: %v", err)
	}
	if r, err := roles.Get(ctx, clerk); err != nil || len(r.Granted()) != 0 || len(r.Permissions()) != 1 {
		t.Errorf("a custom role records no grants: %v", err)
	}
}

// Run comprueba los mapeos sobre db, cuyo esquema de Security tiene que estar migrado y sin usuarios aún.
func Run(t *testing.T, db *sqlrepo.DB) {
	ctx := context.Background()
	store := memory.NewStore("conformance")
	sqlUsers, memUsers := sqlrepo.MustRepository(db, infrastructure.UserMapping()), memory.NewRepository[domain.UserID, *domain.User](store)
	sqlRoles, memRoles := sqlrepo.MustRepository(db, infrastructure.RoleMapping()), memory.NewRepository[domain.RoleID, *domain.Role](store)
	sqlPerms, memPerms := sqlrepo.MustRepository(db, infrastructure.PermissionMapping()), memory.NewRepository[domain.PermissionID, *domain.PermissionEntry](store)
	sqlSessions, memSessions := sqlrepo.MustRepository(db, infrastructure.SessionMapping()), memory.NewRepository[domain.SessionID, *domain.Session](store)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	// both guarda un agregado en la base de datos y un gemelo en memoria.
	both := func(save func(sql bool) error) {
		t.Helper()
		must(save(true))
		must(save(false))
	}

	// --- La semilla de la migración es lo que calcula la regla estándar -----------------------
	var own []domain.Permission
	for _, d := range domain.OwnPermissions() {
		own = append(own, d.Code)
		e, err := domain.DeclarePermission(d.Code, d.Description)
		must(err)
		must(memPerms.Save(ctx, e))
	}
	catalog := domain.NewCatalog(own...)
	for _, seed := range domain.SystemRoles() {
		r, err := domain.NewSystemRole(seed, catalog)
		must(err)
		must(memRoles.Save(ctx, r))
		stored, err := sqlRoles.Get(ctx, seed.ID)
		if err != nil || stored.Name() != seed.Name || !stored.IsSystem() || !slices.Equal(stored.Permissions(), r.Permissions()) {
			t.Fatalf("seeded role %s: %v", seed.Name, err)
		}
		// La migración 4 deja como concedido lo que cada rol del sistema llevaba (D-98).
		if !slices.Equal(stored.Granted(), r.Granted()) {
			t.Fatalf("granted to %s: %v, want %v", seed.Name, stored.Granted(), r.Granted())
		}
	}

	// --- Datos -------------------------------------------------------------------------------
	acme, beta := domain.OrganizationID{UUID: fw.NewUUID()}, domain.OrganizationID{UUID: fw.NewUUID()}
	idp := "https://id.example"
	now := time.Date(2026, 10, 4, 9, 30, 0, 0, time.UTC)
	until := now.Add(15 * time.Minute)

	roleIDs := map[string]domain.RoleID{"clerk": domain.NewRoleID(), "auditor": domain.NewRoleID()}
	rolePerms := map[string][]domain.Permission{"clerk": {domain.PermUserRead, domain.PermUserUpdate}, "auditor": {domain.PermUserRead}}
	for _, name := range []string{"clerk", "auditor"} {
		both(func(sql bool) error {
			r, err := domain.DefineRole(roleIDs[name], strings.ToUpper(name[:1])+name[1:], "A "+name, rolePerms[name])
			if err != nil {
				return err
			}
			if sql {
				return sqlRoles.Save(ctx, r)
			}
			return memRoles.Save(ctx, r)
		})
	}
	for code, active := range map[domain.Permission]bool{"Conformance.Thing.Read": true, "Conformance.Thing.Write": false} {
		both(func(sql bool) error {
			e, err := domain.DeclarePermission(code, "")
			if err != nil {
				return err
			}
			if !active {
				e.Deactivate()
			}
			if sql {
				return sqlPerms.Save(ctx, e)
			}
			return memPerms.Save(ctx, e)
		})
	}

	type seedUser struct {
		name       string
		accesses   []domain.OrganizationAccess
		roles      []domain.RoleID
		identities []domain.ExternalIdentity
		hash       string
		inactive   bool
		locked     bool
	}
	seeds := []seedUser{
		{name: "ana", accesses: []domain.OrganizationAccess{{Organization: acme, Level: domain.Full}}, roles: []domain.RoleID{roleIDs["clerk"]},
			identities: []domain.ExternalIdentity{{Issuer: idp, Subject: "a"}}, hash: "pbkdf2-sha256$1$c2FsdA$a2V5", locked: true},
		{name: "bea", accesses: []domain.OrganizationAccess{{Organization: acme, Level: domain.ReadOnly}, {Organization: beta, Level: domain.Full}},
			roles: []domain.RoleID{roleIDs["clerk"], roleIDs["auditor"]}},
		{name: "carl", accesses: []domain.OrganizationAccess{{Organization: beta, Level: domain.Restricted}},
			roles: []domain.RoleID{domain.RoleGlobalSuperAdmin}, inactive: true},
		{name: "dora", roles: []domain.RoleID{domain.RoleGlobalSuperAdmin},
			identities: []domain.ExternalIdentity{{Issuer: idp, Subject: "d"}, {Issuer: "https://other.example", Subject: "d"}}},
		{name: "Ed.Ward", accesses: []domain.OrganizationAccess{{Organization: acme, Level: domain.Full, IncludeSubsidiaries: true}}},
	}
	userIDs := map[string]domain.UserID{}
	parties := map[string]domain.PartyID{}
	for _, s := range seeds {
		userIDs[s.name], parties[s.name] = domain.NewUserID(), domain.PartyID{UUID: fw.NewUUID()}
		both(func(sql bool) error {
			st := domain.UserState{Party: parties[s.name], Username: s.name, PasswordHash: s.hash, Roles: s.roles, Accesses: s.accesses,
				Identities: s.identities, Active: !s.inactive}
			if s.locked {
				st.FailedAttempts, st.LockedUntil, st.LastLoginAt, st.MustChangePassword = 3, &until, &now, true
			}
			u, err := domain.ReconstituteUser(userIDs[s.name], st)
			if err != nil {
				return err
			}
			if sql {
				return sqlUsers.Save(ctx, u)
			}
			return memUsers.Save(ctx, u)
		})
	}
	family := fw.NewUUID()
	sessionIDs := []domain.SessionID{domain.NewSessionID(), domain.NewSessionID(), domain.NewSessionID()}
	for i, def := range []struct {
		user, token string
		family      fw.UUID
		ended       bool
	}{{"ana", "token-1", family, true}, {"ana", "token-2", family, false}, {"bea", "token-3", fw.NewUUID(), false}} {
		both(func(sql bool) error {
			st := domain.SessionState{User: userIDs[def.user], Family: def.family, TokenHash: domain.HashToken(def.token),
				IssuedAt: now, ExpiresAt: now.Add(time.Hour)}
			if def.ended {
				st.EndedAt, st.Reason = &now, domain.EndedRotated
			}
			s, err := domain.ReconstituteSession(sessionIDs[i], st)
			if err != nil {
				return err
			}
			if sql {
				return sqlSessions.Save(ctx, s)
			}
			return memSessions.Save(ctx, s)
		})
	}

	// --- Cada especificación da en SQL lo que da en memoria ------------------------------------
	for name, tc := range map[string]struct {
		s    spec.Specification[*domain.User]
		want int
	}{
		"all users":                          {nil, 5},
		"by user name, ignoring case":        {domain.UserNamed("ED.ward"), 1},
		"unknown user name":                  {domain.UserNamed("nobody"), 0},
		"user name contains":                 {domain.UserFieldUsername.ContainsFold("AR"), 2},
		"by ids":                             {domain.UsersWithIDs(userIDs["ana"], userIDs["dora"]), 2},
		"by party":                           {domain.UsersOfParties(parties["bea"]), 1},
		"visible to acme":                    {domain.VisibleTo(acme), 3},
		"visible to acme or beta":            {domain.VisibleTo(acme, beta), 4},
		"visible to nobody":                  {domain.VisibleTo(), 0},
		"administrable with full on acme":    {domain.AdministrableBy(acme), 2}, // ana y Ed.Ward; bea también trabaja en beta
		"administrable with full on both":    {domain.AdministrableBy(acme, beta), 4},
		"administrable with full on beta":    {domain.AdministrableBy(beta), 1}, // carl; dora no tiene accesos
		"administrable without full access":  {domain.AdministrableBy(), 0},
		"holding a role":                     {domain.HoldingRole(roleIDs["clerk"]), 2},
		"holding any of two roles":           {domain.HoldingRole(roleIDs["auditor"], domain.RoleGlobalSuperAdmin), 3},
		"holding no role":                    {domain.HoldingRole(), 0},
		"linked identity":                    {domain.WithIdentity(domain.ExternalIdentity{Issuer: idp, Subject: "d"}), 1},
		"identity of another issuer":         {domain.WithIdentity(domain.ExternalIdentity{Issuer: "https://other.example", Subject: "a"}), 0},
		"other active global administrators": {domain.GlobalAdministrators(domain.UserID{}), 1}, // carl está inactivo
		"no other global administrator":      {domain.GlobalAdministrators(userIDs["dora"]), 0},
		"active and visible":                 {domain.VisibleTo(beta).And(domain.UserFieldActive.Eq(true)), 1},
		"name taken by another user":         {domain.UserNamed("ANA").And(domain.UserFieldID.Ne(userIDs["ana"])), 0},
	} {
		same(t, "users: "+name, ctx, sqlUsers, memUsers, tc.s, tc.want)
	}
	for name, tc := range map[string]struct {
		s    spec.Specification[*domain.Role]
		want int
	}{
		"all roles":              {nil, 7},
		"by name, ignoring case": {domain.RoleNamed("  CLERK "), 1},
		"by ids":                 {domain.RolesWithIDs(roleIDs["clerk"], domain.RoleCustomer), 2},
		"carrying a permission":  {domain.RoleFieldPermissions.Any(domain.RolePermFieldCode.Eq(string(domain.PermUserUpdate))), 2}, // clerk y OrganizationAdmin
		"carrying the wildcard":  {domain.RoleFieldPermissions.Any(domain.RolePermFieldCode.Eq(string(domain.Wildcard))), 1},
		"without any permission": {domain.RoleFieldPermissions.IsEmpty(), 2}, // Customer, y StandardUser mientras sólo declara Security
		"name taken by another":  {domain.RoleNamed("auditor").And(domain.RoleFieldID.Ne(roleIDs["auditor"])), 0},
	} {
		same(t, "roles: "+name, ctx, sqlRoles, memRoles, tc.s, tc.want)
	}
	same(t, "permissions in force", ctx, sqlPerms, memPerms, domain.PermFieldActive.Eq(true), len(own)+1)
	same(t, "all permissions", ctx, sqlPerms, memPerms, nil, len(own)+2)
	same(t, "permission by code", ctx, sqlPerms, memPerms, domain.PermFieldCode.Eq("Conformance.Thing.Write"), 1)
	for name, tc := range map[string]struct {
		s    spec.Specification[*domain.Session]
		want int
	}{
		"of a token":          {domain.SessionOfToken("token-2"), 1},
		"of an unknown token": {domain.SessionOfToken("token-9"), 0},
		"live of a user":      {domain.LiveSessionsOf(userIDs["ana"]), 1},
		"live of a family":    {domain.LiveSessionsOfFamily(family), 1},
		"live of a token":     {domain.SessionOfToken("token-1").And(domain.SessionFieldEndedAt.IsNull()), 0},
	} {
		same(t, "sessions: "+name, ctx, sqlSessions, memSessions, tc.s, tc.want)
	}

	// --- Ida y vuelta: lo que vuelve de SQL es lo que se guardó --------------------------------
	for name, id := range userIDs {
		fromSQL, err := sqlUsers.Get(ctx, id)
		must(err)
		fromMemory, err := memUsers.Get(ctx, id)
		must(err)
		if a, b := userState(fromSQL), userState(fromMemory); a != b {
			t.Errorf("user %s:\n sql    %s\n memory %s", name, a, b)
		}
	}
	// Un rol del sistema al que el global retiró un permiso guarda lo retirado como concedido.
	admin, err := sqlRoles.Get(ctx, domain.RoleOrganizationAdmin)
	must(err)
	granted := admin.Granted()
	must(admin.SetPermissions([]domain.Permission{domain.PermUserRead}, catalog))
	must(sqlRoles.Save(ctx, admin))
	admin, err = sqlRoles.Get(ctx, domain.RoleOrganizationAdmin)
	must(err)
	if !slices.Equal(admin.Permissions(), []domain.Permission{domain.PermUserRead}) || !slices.Equal(admin.Granted(), granted) ||
		admin.SystemSyncPending(catalog) {
		t.Errorf("system role round trip: %v granted %v", admin.Permissions(), admin.Granted())
	}
	if clerk, err := sqlRoles.Get(ctx, roleIDs["clerk"]); err != nil || len(clerk.Granted()) != 0 {
		t.Errorf("a custom role records no grants: %v", err)
	}
	rotated, err := sqlSessions.Get(ctx, sessionIDs[0])
	must(err)
	if rotated.UsableAt(now) || !rotated.WasRotated() || !rotated.IssuedAt().Equal(now) || !rotated.ExpiresAt().Equal(now.Add(time.Hour)) || rotated.Family() != family {
		t.Errorf("session round trip: %+v", rotated)
	}

	// --- Los hijos se sustituyen con el agregado, bajo concurrencia optimista ------------------
	bea, err := sqlUsers.Get(ctx, userIDs["bea"])
	must(err)
	stale, err := sqlUsers.Get(ctx, userIDs["bea"])
	must(err)
	bea.RevokeAccess(acme)
	bea.RevokeRole(roleIDs["auditor"])
	must(bea.LinkIdentity(domain.ExternalIdentity{Issuer: idp, Subject: "b"}))
	must(sqlUsers.Save(ctx, bea))
	bea, err = sqlUsers.Get(ctx, userIDs["bea"])
	must(err)
	if len(bea.Accesses()) != 1 || bea.Accesses()[0].Organization != beta || len(bea.Roles()) != 1 || len(bea.Identities()) != 1 {
		t.Errorf("children after the update: %+v %+v %+v", bea.Accesses(), bea.Roles(), bea.Identities())
	}
	stale.Unlock()
	stale.Deactivate()
	if err := sqlUsers.Save(ctx, stale); !errors.Is(err, fw.ErrConflict) {
		t.Errorf("a stale user must not overwrite: %v", err)
	}

	// --- El esquema impone la unicidad en la que se apoya el dominio ---------------------------
	twin, err := domain.RegisterUser(domain.NewUserID(), domain.PartyID{UUID: fw.NewUUID()}, "ANA")
	must(err)
	if err := sqlUsers.Save(ctx, twin); err == nil {
		t.Error("two users with the same name, ignoring case")
	}
	thief, err := domain.RegisterUser(domain.NewUserID(), domain.PartyID{UUID: fw.NewUUID()}, "thief")
	must(err)
	must(thief.LinkIdentity(domain.ExternalIdentity{Issuer: idp, Subject: "a"}))
	if err := db.Do(ctx, func(ctx context.Context) error { return sqlUsers.Save(ctx, thief) }); err == nil {
		t.Error("an external identity linked with two users")
	}
	if n, _ := sqlUsers.Count(ctx, domain.UserNamed("thief")); n != 0 {
		t.Error("the user with the stolen identity must be rolled back with its children")
	}
	copycat, err := domain.DefineRole(domain.NewRoleID(), "CLERK", "", nil)
	must(err)
	if err := sqlRoles.Save(ctx, copycat); err == nil {
		t.Error("two roles with the same name, ignoring case")
	}
	replay, err := domain.OpenSession(userIDs["bea"], fw.NewUUID(), "token-3", now, time.Hour)
	must(err)
	if err := sqlSessions.Save(ctx, replay); err == nil {
		t.Error("two sessions with the same refresh token")
	}
}
