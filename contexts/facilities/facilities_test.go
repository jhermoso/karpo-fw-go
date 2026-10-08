package facilities_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jhermoso/karpo-fw-go/contexts/facilities"
	fapp "github.com/jhermoso/karpo-fw-go/contexts/facilities/application"
	"github.com/jhermoso/karpo-fw-go/contexts/facilities/contracts"
	fdomain "github.com/jhermoso/karpo-fw-go/contexts/facilities/domain"
	finfra "github.com/jhermoso/karpo-fw-go/contexts/facilities/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/geography"
	gapp "github.com/jhermoso/karpo-fw-go/contexts/geography/application"
	gdomain "github.com/jhermoso/karpo-fw-go/contexts/geography/domain"
	ginfra "github.com/jhermoso/karpo-fw-go/contexts/geography/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/parties"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	pinfra "github.com/jhermoso/karpo-fw-go/contexts/parties/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authorization"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution/jwtauth"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/messaging/inprocess"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
	"github.com/jhermoso/karpo-fw-go/pkg/testing/archtest"
)

// host composes three contexts on one hot-swappable backend, as a modular monolith would:
// Parties (organizations, people, facility roles), Geography (address checks) and Facilities.
type host struct {
	t        *testing.T
	srv      *httptest.Server
	sw       *hotswap.Switch
	dir      *authorization.MemoryDirectory
	ids      map[string]fw.UUID
	tokens   map[string]string
	fac      *facilities.Module
	parties  *parties.Module
	geo      *geography.Module
	adminCtx context.Context
}

func compose(t *testing.T) *host {
	ctx := context.Background()
	store := memory.NewStore("memory")
	if err := ginfra.LoadMemory(ctx, store); err != nil {
		t.Fatal(err)
	}
	sw := hotswap.New(store)
	geo := geography.Compose(sw)
	fac := facilities.Compose(sw, facilities.WithAddressChecker(finfra.GeographyAddresses{Checker: geo.Ports}))
	pm := parties.Compose(sw, nil, parties.WithAddressChecker(pinfra.GeographyAddresses{Checker: geo.Ports}),
		parties.WithFacilityDirectory(pinfra.FacilitiesDirectory{Directory: fac.Directory}))

	jwt, _ := jwtauth.New(jwtauth.Config{Secret: []byte("facilities-test")})
	dir := authorization.NewMemoryDirectory()
	h := &host{t: t, sw: sw, dir: dir, ids: map[string]fw.UUID{}, tokens: map[string]string{}, fac: fac, parties: pm, geo: geo}
	perms := []authz.Permission{fapp.PermRead, fapp.PermCreate, fapp.PermUpdate, papp.PermPartyRead, papp.PermPartyUpdate,
		papp.PermPartyCreate, papp.PermRoleAssign, papp.PermRelationshipCreate, gapp.PermBoundaryRead}
	for _, u := range []string{"admin", "clerk", "outsider"} {
		h.ids[u] = fw.NewUUID()
		s := authz.Subject{Active: true, Permissions: perms}
		if u == "admin" {
			s.Roles = []string{authorization.DefaultGlobalAdminRole}
		}
		dir.Put(h.ids[u], s)
		tok, _ := jwt.Issue(jwtauth.Claims{Subject: h.ids[u].String(), Username: u, PartyID: fw.NewUUID().String(), ExpiresAt: fw.Now().Add(time.Hour).Unix()})
		h.tokens[u] = "Bearer " + tok
	}
	ac, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "setup", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
	ac.GlobalAdmin = true
	h.adminCtx = authz.WithContext(ctx, ac)

	mux := http.NewServeMux()
	fac.RegisterRoutes(mux)
	pm.HTTP.RegisterRoutes(mux)
	geo.HTTP.RegisterRoutes(mux)
	h.srv = httptest.NewServer(distribution.Chain(mux, distribution.Authorize(jwt, authorization.NewResolver(dir, authorization.Options{}))))
	t.Cleanup(h.srv.Close)
	t.Cleanup(func() { _ = sw.Close(ctx) })
	return h
}

func (h *host) grant(user string, orgs ...string) {
	s, _, _ := h.dir.Subject(context.Background(), h.ids[user])
	for _, o := range orgs {
		s.Grants = append(s.Grants, authz.Grant{OrganizationID: fw.MustParseUUID(o), Level: authz.Full})
	}
	h.dir.Put(h.ids[user], s)
}

func (h *host) do(method, path, user string, body, out any) int {
	h.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest(method, h.srv.URL+path, &buf)
	req.Header.Set("Authorization", h.tokens[user])
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer res.Body.Close()
	if out != nil && res.StatusCode < 300 {
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			h.t.Fatal(err)
		}
	}
	return res.StatusCode
}

func (h *host) must(got, want int, what string) {
	h.t.Helper()
	if got != want {
		h.t.Fatalf("%s: status %d, want %d", what, got, want)
	}
}

func (h *host) scenario(tag string) {
	t := h.t
	ctx := h.adminCtx
	// Setup in Parties: two internal organizations and an employee of Acme.
	acme, err := h.parties.Service.RegisterOrganization.Handle(ctx, papp.RegisterOrganization{LegalName: "Acme " + tag,
		Roles: []string{pdomain.RoleInternalOrganization.String()}})
	if err != nil {
		t.Fatal(err)
	}
	globex, _ := h.parties.Service.RegisterOrganization.Handle(ctx, papp.RegisterOrganization{LegalName: "Globex " + tag,
		Roles: []string{pdomain.RoleInternalOrganization.String()}})
	h.grant("clerk", acme.ID)
	h.grant("outsider", globex.ID)
	var ana papp.PartyDTO
	h.must(h.do("POST", "/api/persons", "clerk", map[string]any{"givenName": "Ana", "firstSurname": "García " + tag,
		"affiliation": map[string]any{"organization": acme.ID, "relationshipType": pdomain.RelEmployment.String()}}, &ana), 201, "employee")

	towns, err := h.geo.Service.SearchBoundaries.Handle(ctx, gapp.SearchBoundaries{Text: "madrid", Type: gdomain.TypeMunicipality.String()})
	if err != nil {
		t.Fatal(err)
	}
	var madrid string
	for _, b := range towns.Items {
		if b.Name == "Madrid" {
			madrid = b.ID
		}
	}
	office := map[string]any{"organization": acme.ID, "type": fdomain.TypeOffice.String(), "name": "Oficina Sol " + tag,
		"location": map[string]any{"line1": "Puerta del Sol 1", "postalCode": "28013", "locality": "Madrid", "country": "ES", "geoBoundary": madrid}}
	h.must(h.do("POST", "/api/facilities", "clerk", office, nil), 422, "an office needs a phone")
	office["location"].(map[string]any)["phone"] = "+34 910 000 000"
	office["location"].(map[string]any)["postalCode"] = "08001"
	h.must(h.do("POST", "/api/facilities", "clerk", office, nil), 400, "08001 is not in Madrid")
	office["location"].(map[string]any)["postalCode"] = "28013"
	var sol fapp.FacilityDTO
	h.must(h.do("POST", "/api/facilities", "clerk", office, &sol), 201, "register the office")
	if sol.Location.GeoPostalCode == "" || sol.Location.GeoBoundary != madrid || sol.TypeName != "Office" || sol.Location.Phone != "+34910000000" {
		t.Fatalf("office: %+v", sol)
	}
	h.must(h.do("POST", "/api/facilities", "clerk", map[string]any{"organization": globex.ID, "type": fdomain.TypeWarehouse.String(), "name": "X"}, nil),
		404, "globex is out of the clerk's scope")

	// Hierarchy: room in floor in building, same organization, no cycles.
	var building, floor, room fapp.FacilityDTO
	h.must(h.do("POST", "/api/facilities", "clerk", map[string]any{"organization": acme.ID, "type": fdomain.TypeBuilding.String(), "name": "Torre " + tag, "areaM2": "12000.5"}, &building), 201, "building")
	h.must(h.do("POST", "/api/facilities", "clerk", map[string]any{"organization": acme.ID, "type": fdomain.TypeFloor.String(), "name": "Planta 3", "partOf": building.ID}, &floor), 201, "floor")
	h.must(h.do("POST", "/api/facilities", "clerk", map[string]any{"organization": acme.ID, "type": fdomain.TypeRoom.String(), "name": "Sala 301", "partOf": floor.ID}, &room), 201, "room")
	h.must(h.do("PUT", "/api/facilities/"+building.ID+"/parent", "clerk", map[string]any{"partOf": room.ID}, nil), 422, "no cycles")
	if building.AreaM2 != "12000.5" {
		t.Fatalf("area: %q", building.AreaM2)
	}
	var parts fw.Page[fapp.FacilityDTO]
	h.must(h.do("GET", "/api/facilities?partOf="+building.ID, "clerk", nil, &parts), 200, "parts of the building")
	if parts.Total != 1 || parts.Items[0].ID != floor.ID {
		t.Fatalf("parts: %+v", parts)
	}

	// Scope: the outsider sees nothing of Acme.
	h.must(h.do("GET", "/api/facilities/"+sol.ID, "outsider", nil, nil), 404, "outsider")
	var mine fw.Page[fapp.FacilityDTO]
	h.must(h.do("GET", "/api/facilities?q="+tag, "outsider", nil, &mine), 200, "outsider search")
	if mine.Total != 0 {
		t.Fatalf("outsider sees %d facilities", mine.Total)
	}

	// Parties: Ana's work center is the office (PartyFacility stays in Parties).
	var got papp.PartyDTO
	h.must(h.do("POST", "/api/parties/"+ana.ID+"/facility-roles", "clerk", map[string]any{"facility": sol.ID,
		"roleType": pdomain.FacilityWorkCenter.String()}, &got), 200, "work center")
	if len(got.FacilityRoles) != 1 || got.FacilityRoles[0].Facility != sol.ID {
		t.Fatalf("facility roles: %+v", got.FacilityRoles)
	}
	var staff fw.Page[papp.PartyDTO]
	h.must(h.do("GET", "/api/parties?facility="+sol.ID, "clerk", nil, &staff), 200, "staff of the office")
	if staff.Total != 1 || staff.Items[0].ID != ana.ID {
		t.Fatalf("staff: %+v", staff)
	}
	h.must(h.do("POST", "/api/parties/"+globex.ID+"/facility-roles", "outsider", map[string]any{"facility": sol.ID,
		"roleType": pdomain.FacilityBranch.String()}, nil), 404, "another organization's facility does not exist for the outsider")
	h.must(h.do("PUT", "/api/facilities/"+room.ID+"/active", "clerk", map[string]any{"active": false}, nil), 200, "close the room")
	h.must(h.do("POST", "/api/parties/"+ana.ID+"/facility-roles", "clerk", map[string]any{"facility": room.ID,
		"roleType": pdomain.FacilityWorkCenter.String()}, nil), 422, "inactive facility")

	// Published Language: other contexts keep their name caches with facility events.
	h.must(h.do("PUT", "/api/facilities/"+sol.ID+"/name", "clerk", map[string]any{"name": "Oficina Puerta del Sol " + tag}, nil), 200, "rename")
	broker := inprocess.NewBroker()
	store := memory.NewStore("stock")
	product := messaging.NewConsumer("product", memory.NewInbox(store), store)
	names := map[string]string{}
	messaging.Handle(product, func(_ context.Context, e contracts.FacilityRegisteredV1, _ application.Envelope) error {
		names[e.FacilityID] = e.Name
		return nil
	})
	messaging.Handle(product, func(_ context.Context, e contracts.FacilityRenamedV1, _ application.Envelope) error {
		names[e.FacilityID] = e.Name
		return nil
	})
	broker.Subscribe("product", product)
	for {
		n, err := h.fac.Relay(broker).RelayOnce(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
	}
	if names[sol.ID] != "Oficina Puerta del Sol "+tag || len(names) < 4 {
		t.Fatalf("name cache: %v", names)
	}
	trail, err := h.fac.Audit.Trail(context.Background(), fdomain.FacilityKind, sol.ID)
	if err != nil || len(trail) != 2 || trail[1].Actor.Name != "clerk" {
		t.Fatalf("audit: %+v %v", trail, err)
	}
}

func TestFacilities_ThreeContexts_MemoryThenSQLite(t *testing.T) {
	h := compose(t)
	h.scenario("mem")

	ctx := context.Background()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "host.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{pinfra.Migrations(), ginfra.Migrations(), finfra.Migrations()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Verify(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.sw.Swap(ctx, db); err != nil {
		t.Fatal(err)
	}
	h.scenario("sql")
}

func TestFacilities_LayersRespectTheArchitecture(t *testing.T) {
	const m = "github.com/jhermoso/karpo-fw-go"
	archtest.AssertOnlyImports(t, "domain", false, archtest.Std, m+"/pkg/domain/...", m+"/contexts/facilities/domain")
	archtest.AssertOnlyImports(t, "contracts", false, archtest.Std)
	archtest.AssertTreeDoesNotImport(t, "application", []string{"/pkg/persistence", "/pkg/distribution", "database/sql", "net/http"})
}
