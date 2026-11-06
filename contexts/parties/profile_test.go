package parties_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/parties/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
)

// El perfil de la empresa interna (D-133) por HTTP, en memoria y en SQLite: se lee con el permiso
// de leer parties y se cambia con el de modificarlas, en el ámbito de la empresa; una zona que no
// es IANA da 422 Company.InvalidTimeZone, y una vacía la limpia.
func TestParties_InternalOrganizationProfile(t *testing.T) {
	for name, useSQL := range map[string]bool{"memory": false, "sqlite": true} {
		t.Run(name, func(t *testing.T) {
			e := compose(t, newMemoryDirectory)
			if useSQL {
				ctx := context.Background()
				raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "profile.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
				if err != nil {
					t.Fatal(err)
				}
				raw.SetMaxOpenConns(1)
				t.Cleanup(func() { _ = raw.Close() })
				db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
				m, _ := infrastructure.Migrator(db)
				if _, err := m.Migrate(ctx); err != nil {
					t.Fatal(err)
				}
				if err := e.sw.Swap(ctx, db); err != nil {
					t.Fatal(err)
				}
			}
			e.profile()
		})
	}
}

func (e *env) profile() {
	e.t.Helper()
	var acme, globex, pedro papp.PartyDTO
	e.must(e.do("POST", "/api/organizations", e.admin, map[string]any{"legalName": "Acme Perfil",
		"roles": []string{domain.RoleInternalOrganization.String()}}, &acme), 201, "register acme")
	e.must(e.do("POST", "/api/organizations", e.admin, map[string]any{"legalName": "Globex Perfil",
		"roles": []string{domain.RoleInternalOrganization.String()}}, &globex), 201, "register globex")
	e.grant("clerk", authz.Full, acme.ID)
	e.grant("viewer", authz.ReadOnly, acme.ID)
	e.grant("reader", authz.Full, acme.ID)
	e.grant("outsider", authz.Full, globex.ID)
	e.must(e.do("POST", "/api/persons", e.clerk, map[string]any{"givenName": "Pedro", "firstSurname": "Perfil",
		"affiliation": map[string]any{"organization": acme.ID, "relationshipType": domain.RelCustomer.String()}}, &pedro), 201, "a customer of acme")

	path := "/api/profiles/internal-organizations/" + acme.ID
	var got papp.InternalOrganizationProfileDTO
	e.must(e.do("GET", path, e.viewer, nil, &got), 200, "a profile not saved yet")
	if got.InternalOrganizationID != acme.ID || got.TimeZone != nil {
		e.badProfile(got)
	}
	e.must(e.do("PUT", path, e.clerk, map[string]any{"timeZone": "Atlantic/Canary"}, &got), 200, "the clerk sets the zone")
	if got.TimeZone == nil || *got.TimeZone != "Atlantic/Canary" {
		e.badProfile(got)
	}
	e.must(e.do("GET", path, e.reader, nil, &got), 200, "read it back")
	if got.TimeZone == nil || *got.TimeZone != "Atlantic/Canary" {
		e.badProfile(got)
	}
	if tz, err := e.mod.CompanyTimeZones.TimeZone(context.Background(), acme.ID); err != nil || tz != "Atlantic/Canary" {
		e.t.Fatalf("the zone other contexts read: %q %v", tz, err)
	}

	e.mustProblem(e.problem("PUT", path, e.clerk, map[string]any{"timeZone": "Romance Standard Time"}), 422, domain.ErrInvalidTimeZone,
		"a Windows name is not an IANA code")
	e.mustProblem(e.problem("PUT", path, e.clerk, map[string]any{"timeZone": "Mars/Olympus"}), 422, domain.ErrInvalidTimeZone,
		"an unknown zone")
	e.must(e.do("PUT", path, e.viewer, map[string]any{"timeZone": "Europe/Madrid"}, nil), 403, "a read-only grant cannot change it")
	e.must(e.do("PUT", path, e.reader, map[string]any{"timeZone": "Europe/Madrid"}, nil), 403, "nor can who lacks the update permission")
	e.must(e.do("GET", path, e.outsider, nil, nil), 404, "another company does not see it")
	e.must(e.do("PUT", path, e.outsider, map[string]any{"timeZone": "Europe/Madrid"}, nil), 404, "nor change it")
	e.must(e.do("GET", path, "", nil, nil), 401, "anonymous callers do not")
	e.must(e.do("GET", "/api/profiles/internal-organizations/"+pedro.ID, e.clerk, nil, nil), 404, "a party that is not an internal organization")
	e.must(e.do("PUT", "/api/profiles/internal-organizations/"+globex.ID, e.admin, map[string]any{"timeZone": "UTC"}, nil), 200, "the global admin")

	e.must(e.do("PUT", path, e.clerk, map[string]any{"timeZone": nil}, &got), 200, "null clears it")
	if got.TimeZone != nil {
		e.badProfile(got)
	}
	e.must(e.do("PUT", path, e.clerk, map[string]any{"timeZone": "Atlantic/Canary"}, nil), 200, "set it again")
	e.must(e.do("PUT", path, e.clerk, map[string]any{}, &got), 200, "an empty body clears it: the PUT replaces the profile")
	if got.TimeZone != nil {
		e.badProfile(got)
	}
	trail, err := e.mod.Audit.Trail(context.Background(), domain.InternalOrganizationProfileKind, acme.ID)
	if err != nil || len(trail) != 4 || trail[0].Operation != "created" || trail[3].Operation != "updated" {
		e.t.Fatalf("audit of the profile: %+v %v", trail, err)
	}
}

func (e *env) badProfile(got papp.InternalOrganizationProfileDTO) {
	e.t.Helper()
	tz := "<nil>"
	if got.TimeZone != nil {
		tz = *got.TimeZone
	}
	e.t.Fatalf("profile: %s %s", got.InternalOrganizationID, tz)
}
