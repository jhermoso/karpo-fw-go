package hr_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jhermoso/karpo-fw-go/contexts/facilities"
	fapp "github.com/jhermoso/karpo-fw-go/contexts/facilities/application"
	fdomain "github.com/jhermoso/karpo-fw-go/contexts/facilities/domain"
	finfra "github.com/jhermoso/karpo-fw-go/contexts/facilities/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/geography"
	ginfra "github.com/jhermoso/karpo-fw-go/contexts/geography/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/hr"
	happ "github.com/jhermoso/karpo-fw-go/contexts/hr/application"
	"github.com/jhermoso/karpo-fw-go/contexts/hr/contracts"
	hdomain "github.com/jhermoso/karpo-fw-go/contexts/hr/domain"
	hinfra "github.com/jhermoso/karpo-fw-go/contexts/hr/infrastructure"
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
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/messaging/inprocess"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
	"github.com/jhermoso/karpo-fw-go/pkg/testing/archtest"
)

// host composes four contexts on one hot-swappable backend, as a modular monolith would:
// Parties (organizations, people, affiliations), Geography, Facilities and HR.
type host struct {
	t        *testing.T
	srv      *httptest.Server
	sw       *hotswap.Switch
	dir      *authorization.MemoryDirectory
	ids      map[string]fw.UUID
	tokens   map[string]string
	hr       *hr.Module
	fac      *facilities.Module
	parties  *parties.Module
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
	hm := hr.Compose(sw, hr.WithOrganizations(hinfra.PartiesOrganizations{Hierarchy: pm.Organizations, Membership: pm.Organizations}),
		hr.WithFacilities(hinfra.FacilitiesDirectory{Directory: fac.Directory}))

	jwt, _ := jwtauth.New(jwtauth.Config{Secret: []byte("hr-test")})
	dir := authorization.NewMemoryDirectory()
	h := &host{t: t, sw: sw, dir: dir, ids: map[string]fw.UUID{}, tokens: map[string]string{}, hr: hm, fac: fac, parties: pm}
	perms := []authz.Permission{happ.PermPositionRead, happ.PermPositionCreate, happ.PermPositionUpdate, happ.PermEmploymentRead,
		happ.PermEmploymentCreate, happ.PermEmploymentUpdate, happ.PermWorkCenterRead, happ.PermWorkCenterCreate, happ.PermWorkCenterUpdate,
		happ.PermCatalogRead, fapp.PermRead, fapp.PermCreate, papp.PermPartyRead, papp.PermPartyCreate, papp.PermPartyUpdate}
	for _, u := range []string{"clerk", "outsider", "reader"} {
		h.ids[u] = fw.NewUUID()
		ps := perms
		if u == "reader" {
			ps = []authz.Permission{happ.PermPositionRead, happ.PermEmploymentRead}
		}
		dir.Put(h.ids[u], authz.Subject{Active: true, Permissions: ps})
		tok, _ := jwt.Issue(jwtauth.Claims{Subject: h.ids[u].String(), Username: u, PartyID: fw.NewUUID().String(), ExpiresAt: fw.Now().Add(time.Hour).Unix()})
		h.tokens[u] = "Bearer " + tok
	}
	ac, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "setup", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
	ac.GlobalAdmin = true
	h.adminCtx = authz.WithContext(ctx, ac)

	mux := http.NewServeMux()
	hm.RegisterRoutes(mux)
	fac.RegisterRoutes(mux)
	pm.HTTP.RegisterRoutes(mux)
	h.srv = httptest.NewServer(distribution.Chain(mux, distribution.Authorize(jwt, authorization.NewResolver(dir, authorization.Options{}))))
	t.Cleanup(h.srv.Close)
	t.Cleanup(func() { _ = sw.Close(ctx) })
	return h
}

func (h *host) grant(user string, level authz.AccessLevel, orgs ...string) {
	s, _, _ := h.dir.Subject(context.Background(), h.ids[user])
	for _, o := range orgs {
		s.Grants = append(s.Grants, authz.Grant{OrganizationID: fw.MustParseUUID(o), Level: level})
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
	ps := h.parties.Service
	org := func(name string, role fw.UUID) papp.PartyDTO {
		o, err := ps.RegisterOrganization.Handle(ctx, papp.RegisterOrganization{LegalName: name + " " + tag, Roles: []string{role.String()}})
		if err != nil {
			t.Fatal(err)
		}
		return o
	}
	acme, globex := org("Acme", pdomain.RoleInternalOrganization.UUID), org("Globex", pdomain.RoleInternalOrganization.UUID)
	it := org("Acme IT", pdomain.RoleDepartment.UUID)
	if _, err := ps.EstablishRelationship.Handle(ctx, papp.EstablishRelationship{Type: pdomain.RelOrganizationRollup.String(), From: it.ID, To: acme.ID}); err != nil {
		t.Fatal(err)
	}
	h.grant("clerk", authz.Full, acme.ID)
	h.grant("outsider", authz.Full, globex.ID)
	h.grant("reader", authz.ReadOnly, acme.ID)
	person := func(user, name, employer string) papp.PartyDTO {
		var p papp.PartyDTO
		h.must(h.do("POST", "/api/persons", user, map[string]any{"givenName": name, "firstSurname": tag,
			"affiliation": map[string]any{"organization": employer, "relationshipType": pdomain.RelEmployment.String()}}, &p), 201, name)
		return p
	}
	ana, bea, carl := person("clerk", "Ana", acme.ID), person("clerk", "Bea", acme.ID), person("outsider", "Carl", globex.ID)
	facility := func(user, owner, name string) fapp.FacilityDTO {
		var f fapp.FacilityDTO
		h.must(h.do("POST", "/api/facilities", user, map[string]any{"organization": owner, "type": fdomain.TypeBuilding.String(), "name": name + " " + tag}, &f), 201, name)
		return f
	}
	tower, plant, depot := facility("clerk", acme.ID, "Torre"), facility("clerk", acme.ID, "Planta"), facility("outsider", globex.ID, "Depósito")

	// Work centers: a facility of the employer, unique code, one headquarters.
	var wc1, wc2 happ.WorkCenterDTO
	h.must(h.do("POST", "/api/hr/work-centers", "clerk", map[string]any{"employer": acme.ID, "facility": tower.ID, "code": "28/1/01",
		"headquarters": true, "opened": "2020-01-01"}, &wc1), 201, "work center")
	h.must(h.do("POST", "/api/hr/work-centers", "clerk", map[string]any{"employer": acme.ID, "facility": depot.ID, "code": "28/1/02",
		"opened": "2020-01-01"}, nil), 404, "another organization's facility")
	h.must(h.do("POST", "/api/hr/work-centers", "clerk", map[string]any{"employer": acme.ID, "facility": plant.ID, "code": "28/1/01",
		"opened": "2020-01-01"}, nil), 422, "duplicate code")
	h.must(h.do("POST", "/api/hr/work-centers", "clerk", map[string]any{"employer": acme.ID, "facility": plant.ID, "code": "28/1/02",
		"headquarters": true, "opened": "2021-01-01"}, &wc2), 201, "second work center")
	h.must(h.do("GET", "/api/hr/work-centers/"+wc1.ID, "clerk", nil, &wc1), 200, "get")
	if wc1.Headquarters || !wc2.Headquarters {
		t.Fatalf("one headquarters: %+v %+v", wc1, wc2)
	}

	// Employments: an affiliated person, once per employer, inside the scope.
	today := vocab.DateOf(fw.Now())
	hired := today.AddDays(-200)
	var anaJob, beaJob happ.EmploymentDTO
	h.must(h.do("POST", "/api/hr/employments", "clerk", map[string]any{"person": ana.ID, "employer": acme.ID, "number": "E-1",
		"hired": hired.String(), "jobCategory": "GP1"}, &anaJob), 201, "hire ana")
	h.must(h.do("POST", "/api/hr/employments", "clerk", map[string]any{"person": ana.ID, "employer": acme.ID, "hired": hired.String()}, nil),
		422, "already employed")
	h.must(h.do("POST", "/api/hr/employments", "clerk", map[string]any{"person": carl.ID, "employer": acme.ID, "hired": hired.String()}, nil),
		422, "carl is not affiliated with acme")
	h.must(h.do("POST", "/api/hr/employments", "outsider", map[string]any{"person": carl.ID, "employer": acme.ID, "hired": hired.String()}, nil),
		404, "acme is out of the outsider's scope")
	h.must(h.do("POST", "/api/hr/employments", "clerk", map[string]any{"person": bea.ID, "employer": acme.ID, "number": "E-1",
		"hired": hired.String()}, nil), 422, "the number belongs to ana")
	metal := "b3800000-0003-0000-0000-000000000005"
	h.must(h.do("POST", "/api/hr/employments/"+anaJob.ID+"/contracts", "clerk", map[string]any{"typeCode": "100", "start": hired.String(),
		"agreement": metal, "workCenter": wc1.ID, "weeklyHours": "40", "primary": true}, &anaJob), 200, "contract")
	h.must(h.do("POST", "/api/hr/employments/"+anaJob.ID+"/contracts", "clerk", map[string]any{"typeCode": "100", "start": "2023-06-01",
		"agreement": metal, "workCenter": wc1.ID}, nil), 422, "before hiring")
	h.must(h.do("POST", "/api/hr/employments/"+anaJob.ID+"/contracts", "reader", map[string]any{"typeCode": "100", "start": hired.String(),
		"agreement": metal, "workCenter": wc1.ID}, nil), 403, "the reader cannot write")
	if len(anaJob.Contracts) != 1 || anaJob.Contracts[0].WeeklyHours != "40" || !anaJob.Contracts[0].Primary {
		t.Fatalf("contracts: %+v", anaJob.Contracts)
	}

	// Positions: the organization of a unit comes from Parties; holders are employees.
	var types []hdomain.PositionType
	h.must(h.do("GET", "/api/hr/catalogs/position-types?active=true", "clerk", nil, &types), 200, "types")
	var ceo, cto, dev happ.PositionDTO
	from := hired.BaseTime()
	open := func(unit string, out *happ.PositionDTO) {
		h.must(h.do("POST", "/api/hr/positions", "clerk", map[string]any{"unit": unit, "type": types[0].ID.String(), "plannedFrom": from,
			"fullTime": true}, out), 201, "position")
	}
	open(acme.ID, &ceo)
	open(it.ID, &cto)
	open(it.ID, &dev)
	if cto.Organization != acme.ID || cto.Unit != it.ID || !cto.Vacant || cto.TypeTitle != types[0].Title {
		t.Fatalf("position: %+v", cto)
	}
	h.must(h.do("POST", "/api/hr/positions/"+ceo.ID+"/fill", "clerk", map[string]any{"person": ana.ID, "from": from}, &ceo), 200, "ana is the ceo")
	h.must(h.do("POST", "/api/hr/positions/"+cto.ID+"/fill", "clerk", map[string]any{"person": bea.ID, "from": from}, nil), 422, "bea is not hired")
	h.must(h.do("POST", "/api/hr/employments", "clerk", map[string]any{"person": bea.ID, "employer": acme.ID, "number": "E-2",
		"hired": hired.String()}, &beaJob), 201, "hire bea")
	h.must(h.do("POST", "/api/hr/positions/"+cto.ID+"/fill", "clerk", map[string]any{"person": bea.ID, "from": from}, nil), 200, "bea is the cto")
	h.must(h.do("POST", "/api/hr/positions/"+ceo.ID+"/fill", "clerk", map[string]any{"person": bea.ID, "from": from}, nil), 422, "one holder")
	h.must(h.do("POST", "/api/hr/positions/"+cto.ID+"/reports-to", "clerk", map[string]any{"supervisor": ceo.ID, "primary": true, "from": from}, nil), 200, "cto -> ceo")
	h.must(h.do("POST", "/api/hr/positions/"+dev.ID+"/reports-to", "clerk", map[string]any{"supervisor": cto.ID, "primary": true, "from": from}, nil), 200, "dev -> cto")
	h.must(h.do("POST", "/api/hr/positions/"+ceo.ID+"/reports-to", "clerk", map[string]any{"supervisor": dev.ID, "primary": true, "from": from}, nil), 422, "no cycles")
	var chart happ.ChartNode
	h.must(h.do("GET", "/api/hr/positions/"+ceo.ID+"/chart?depth=5", "clerk", nil, &chart), 200, "org chart")
	if len(chart.Reports) != 1 || chart.Reports[0].Position.ID != cto.ID || len(chart.Reports[0].Reports) != 1 ||
		chart.Reports[0].Reports[0].Position.ID != dev.ID || chart.Reports[0].Position.Holder != bea.ID {
		t.Fatalf("chart: %+v", chart)
	}
	var vacant fw.Page[happ.PositionDTO]
	h.must(h.do("GET", "/api/hr/positions?vacant=true&unit="+it.ID, "clerk", nil, &vacant), 200, "vacant")
	if vacant.Total != 1 || vacant.Items[0].ID != dev.ID {
		t.Fatalf("vacant: %+v", vacant)
	}
	var held fw.Page[happ.PositionDTO]
	h.must(h.do("GET", "/api/hr/positions?holder="+ana.ID, "clerk", nil, &held), 200, "held by ana")
	if held.Total != 1 || held.Items[0].ID != ceo.ID {
		t.Fatalf("held: %+v", held)
	}

	// Scope: the outsider sees nothing of Acme; the reader reads but does not write.
	h.must(h.do("GET", "/api/hr/employments/"+anaJob.ID, "outsider", nil, nil), 404, "outsider")
	h.must(h.do("GET", "/api/hr/positions/"+ceo.ID, "outsider", nil, nil), 404, "outsider")
	var none fw.Page[happ.EmploymentDTO]
	h.must(h.do("GET", "/api/hr/employments", "outsider", nil, &none), 200, "outsider search")
	if none.Total != 0 {
		t.Fatalf("outsider sees %d employments", none.Total)
	}
	h.must(h.do("GET", "/api/hr/positions/"+ceo.ID, "reader", nil, nil), 200, "reader")
	h.must(h.do("POST", "/api/hr/positions/"+ceo.ID+"/vacate", "reader", nil, nil), 403, "reader")

	// Payroll asks the primary contract.
	staff, err := h.hr.Staff.EmploymentsOn(context.Background(), []string{ana.ID, carl.ID}, today.String())
	if err != nil || len(staff[ana.ID]) != 1 || staff[ana.ID][0].Contract == nil || staff[ana.ID][0].Contract.WorkCenter != wc1.ID {
		t.Fatalf("staff: %+v %v", staff, err)
	}

	// A work center with contracts in force cannot close; termination ends contracts and positions.
	h.must(h.do("POST", "/api/hr/work-centers/"+wc1.ID+"/close", "clerk", map[string]any{"on": today.String()}, nil), 422, "in use")
	h.must(h.do("POST", "/api/hr/employments/"+anaJob.ID+"/terminate", "clerk", map[string]any{"on": today.AddDays(-1).String(),
		"reason": "baja voluntaria"}, &anaJob), 200, "terminate ana")
	if anaJob.Terminated == "" || anaJob.Contracts[0].End != today.AddDays(-1).String() {
		t.Fatalf("terminated: %+v", anaJob)
	}
	ceoID := ceo.ID
	ceo = happ.PositionDTO{}
	h.must(h.do("GET", "/api/hr/positions/"+ceoID, "clerk", nil, &ceo), 200, "ceo")
	if !ceo.Vacant || ceo.Holder != "" || len(ceo.Holders) != 1 || ceo.Holders[0].Thru == nil {
		t.Fatalf("the ceo position is vacant: %+v", ceo)
	}
	h.must(h.do("POST", "/api/hr/work-centers/"+wc1.ID+"/close", "clerk", map[string]any{"on": today.String()}, &wc1), 200, "close")
	if staff, _ := h.hr.Staff.EmploymentsOn(context.Background(), []string{ana.ID}, today.String()); len(staff[ana.ID]) != 0 {
		t.Fatalf("ana is no longer employed: %+v", staff)
	}
	refs, err := h.hr.Positions.Resolve(context.Background(), []string{cto.ID, ceo.ID})
	if err != nil || refs[cto.ID].Holder != bea.ID || refs[cto.ID].ReportsTo != ceo.ID || refs[ceo.ID].Holder != "" {
		t.Fatalf("positions: %+v %v", refs, err)
	}

	// Published Language.
	broker := inprocess.NewBroker()
	store := memory.NewStore("payroll")
	payroll := messaging.NewConsumer("payroll", memory.NewInbox(store), store)
	var seen []string
	record := func(kind string) { seen = append(seen, kind) }
	messaging.Handle(payroll, func(_ context.Context, e contracts.EmployeeHiredV1, _ application.Envelope) error {
		record("hired")
		return nil
	})
	messaging.Handle(payroll, func(_ context.Context, e contracts.ContractStartedV1, _ application.Envelope) error {
		record("started")
		return nil
	})
	messaging.Handle(payroll, func(_ context.Context, e contracts.ContractEndedV1, _ application.Envelope) error {
		record("ended")
		return nil
	})
	messaging.Handle(payroll, func(_ context.Context, e contracts.EmployeeTerminatedV1, _ application.Envelope) error {
		record("terminated")
		return nil
	})
	messaging.Handle(payroll, func(_ context.Context, e contracts.PositionFilledV1, _ application.Envelope) error {
		record("filled")
		return nil
	})
	messaging.Handle(payroll, func(_ context.Context, e contracts.PositionVacatedV1, _ application.Envelope) error {
		record("vacated")
		return nil
	})
	broker.Subscribe("payroll", payroll)
	broker.Subscribe("parties", h.parties.Consumer)
	for {
		n, err := h.hr.Relay(broker).RelayOnce(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
	}
	for _, k := range []string{"hired", "started", "ended", "terminated", "filled", "vacated"} {
		if !slices.Contains(seen, k) {
			t.Fatalf("published %v, missing %s", seen, k)
		}
	}
	// Parties reacted: the Employment affiliation of Ana with Acme ended, Bea's did not.
	orgs, err := h.parties.Organizations.InternalOrganizations(context.Background(), []string{ana.ID, bea.ID})
	if err != nil || slices.Contains(orgs[ana.ID], acme.ID) || !slices.Contains(orgs[bea.ID], acme.ID) {
		t.Fatalf("affiliations after the termination: %+v %v", orgs, err)
	}
	h.must(h.do("GET", "/api/parties/"+ana.ID, "clerk", nil, nil), 404, "ana is no longer visible to acme")
	trail, err := h.hr.Audit.Trail(context.Background(), hdomain.EmploymentKind, anaJob.ID)
	if err != nil || len(trail) != 3 || trail[2].Actor.Name != "clerk" {
		t.Fatalf("audit: %+v %v", trail, err)
	}
}

func TestHR_FourContexts_MemoryThenSQLite(t *testing.T) {
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
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{pinfra.Migrations(), ginfra.Migrations(), finfra.Migrations(), hinfra.Migrations()})
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

func TestHR_LayersRespectTheArchitecture(t *testing.T) {
	const m = "github.com/jhermoso/karpo-fw-go"
	archtest.AssertOnlyImports(t, "domain", false, archtest.Std, m+"/pkg/domain/...", m+"/contexts/hr/domain")
	archtest.AssertOnlyImports(t, "contracts", false, archtest.Std)
	archtest.AssertTreeDoesNotImport(t, "application", []string{"/pkg/persistence", "/pkg/distribution", "database/sql", "net/http"})
}
