package parties_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jhermoso/karpo-fw-go/contexts/parties"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	"github.com/jhermoso/karpo-fw-go/contexts/parties/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/parties/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/security"
	sapp "github.com/jhermoso/karpo-fw-go/contexts/security/application"
	sdomain "github.com/jhermoso/karpo-fw-go/contexts/security/domain"
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
	"github.com/jhermoso/karpo-fw-go/pkg/time/fake"
)

type env struct {
	t   *testing.T
	srv *httptest.Server
	sw  *hotswap.Switch
	mod *parties.Module
	dir directory
	// clock is the domain clock of the test host: do moves it forward before every request.
	clock *fake.FakeClock
	// admin is a global administrator; the others work in an organization scope: reader
	// (read-only), clerk (full), viewer (read-only grant, write permissions), outsider (full
	// on another organization).
	// on another organization), seller (full, everything but granting trials).
	admin, reader, clerk, viewer, outsider, seller string
}

var allPerms = append(slices.Clone(sellerPerms), papp.PermRelationshipSetTrial, papp.PermRoleTypeDefine, papp.PermRelationshipTypeDefine)

var readerPerms = []authz.Permission{papp.PermPartyRead, papp.PermRelationshipRead}

// sellerPerms are what a standard user gets from the Security catalog (read, create, update)
// plus roles and terminations: everything but granting trials.
var sellerPerms = []authz.Permission{papp.PermPartyRead, papp.PermPartyCreate, papp.PermPartyUpdate, papp.PermRoleAssign,
	papp.PermRelationshipRead, papp.PermRelationshipCreate, papp.PermRelationshipEnd, papp.PermRelationshipUpdate}

// profiles are the users of the test host and their user names.
var profiles = map[string]string{"admin": "ana.admin", "reader": "rita.reader", "clerk": "carl.clerk", "viewer": "vera.viewer",
	"outsider": "otto.outsider", "seller": "sara.seller"}

// directory is the security source of the test host. The Parties use cases never see it: they
// only read the authorization context the resolver builds from it on every request. The same
// scenario runs over the in-memory directory of the framework and over the Security context.
type directory interface {
	authz.Directory
	// register creates the subject of a profile and returns its id and its party.
	register(t *testing.T, profile, username string) (subject, party fw.UUID)
	// grant gives a profile access to organizations.
	grant(t *testing.T, profile string, level authz.AccessLevel, orgs ...string)
}

// memoryDirectory is the in-memory directory of the framework.
type memoryDirectory struct {
	*authorization.MemoryDirectory
	ids map[string]fw.UUID
}

func newMemoryDirectory(*testing.T) directory {
	return &memoryDirectory{MemoryDirectory: authorization.NewMemoryDirectory(), ids: map[string]fw.UUID{}}
}

func (d *memoryDirectory) register(_ *testing.T, profile, _ string) (fw.UUID, fw.UUID) {
	s := authz.Subject{Active: true, Permissions: allPerms}
	switch profile {
	case "admin":
		s = authz.Subject{Active: true, Roles: []string{authorization.DefaultGlobalAdminRole}}
	case "reader":
		s.Permissions = readerPerms
	case "seller":
		s.Permissions = sellerPerms
	}
	d.ids[profile] = fw.NewUUID()
	d.Put(d.ids[profile], s)
	return d.ids[profile], fw.NewUUID()
}

func (d *memoryDirectory) grant(_ *testing.T, profile string, level authz.AccessLevel, orgs ...string) {
	s, _, _ := d.Subject(context.Background(), d.ids[profile])
	for _, o := range orgs {
		id, _ := fw.ParseUUID(o)
		s.Grants = append(s.Grants, authz.Grant{OrganizationID: id, Level: level})
	}
	d.Put(d.ids[profile], s)
}

// securityDirectory is the real directory: the Security bounded context, with its users, roles,
// permission catalog and organization accesses in its own store.
type securityDirectory struct {
	authz.Directory
	sec   *security.Module
	setup context.Context // a global administrator, for the setup
	roles map[string]sdomain.RoleID
	ids   map[string]sdomain.UserID
}

func newSecurityDirectory(t *testing.T) directory {
	ctx := context.Background()
	sw := hotswap.New(memory.NewStore("security"))
	t.Cleanup(func() { _ = sw.Close(ctx) })
	sec := security.Compose(sw)
	if _, err := sec.SyncCatalog(ctx, papp.Permissions()...); err != nil {
		t.Fatal(err)
	}
	ac, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "setup", Kind: authz.Service})
	ac.GlobalAdmin = true
	d := &securityDirectory{Directory: sec.Directory, sec: sec, setup: authz.WithContext(ctx, ac),
		roles: map[string]sdomain.RoleID{"admin": sdomain.RoleGlobalSuperAdmin}, ids: map[string]sdomain.UserID{}}
	codes := func(perms []authz.Permission) (out []string) {
		for _, p := range perms {
			out = append(out, string(p))
		}
		return out
	}
	for name, perms := range map[string][]authz.Permission{"reader": readerPerms, "worker": allPerms, "seller": sellerPerms} {
		r, err := sec.Service.DefineRole.Handle(d.setup, sapp.DefineRole{Name: name, Permissions: codes(perms)})
		if err != nil {
			t.Fatal(err)
		}
		d.roles[name], _ = sdomain.ParseRoleID(r.ID)
	}
	return d
}

func (d *securityDirectory) register(t *testing.T, profile, username string) (fw.UUID, fw.UUID) {
	party := fw.NewUUID()
	u, err := d.sec.Service.RegisterUser.Handle(d.setup, sapp.RegisterUser{Username: username, Party: party.String()})
	if err != nil {
		t.Fatal(err)
	}
	id, _ := sdomain.ParseUserID(u.ID)
	role, ok := d.roles[profile]
	if !ok {
		role = d.roles["worker"]
	}
	if _, err := d.sec.Service.AssignRole.Handle(d.setup, sapp.AssignRole{ID: id, Role: role}); err != nil {
		t.Fatal(err)
	}
	d.ids[profile] = id
	return id.UUID, party
}

func (d *securityDirectory) grant(t *testing.T, profile string, level authz.AccessLevel, orgs ...string) {
	for _, o := range orgs {
		org, _ := sdomain.ParseOrganizationID(o)
		if _, err := d.sec.Service.GrantAccess.Handle(d.setup, sapp.GrantAccess{ID: d.ids[profile], Organization: org, Level: string(level)}); err != nil {
			t.Fatal(err)
		}
	}
}

// grant gives a user access to organizations (the security directory is read on every request).
func (e *env) grant(user string, level authz.AccessLevel, orgs ...string) {
	e.t.Helper()
	e.dir.grant(e.t, user, level, orgs...)
}

// compose is the composition root of the test host: JWT authentication, the authorization
// resolver over the security directory, and the Parties module on a hot-swap switch.
func compose(t *testing.T, newDirectory func(*testing.T) directory) *env {
	// The system clock advances in ticks (up to 15.6 ms on Windows), longer than an in-memory
	// request, so two requests could share an instant or not depending on the run. The scenario
	// runs on a clock that every request advances: each validity it ends has a length. Ending at
	// the instant of the start is covered in the domain (TestEndingAtTheInstantOfTheStart).
	clock := fake.New(time.Now())
	t.Cleanup(fw.SetClock(clock))
	jwt, _ := jwtauth.New(jwtauth.Config{Secret: []byte("parties-test")})
	dir := newDirectory(t)
	tokens := map[string]string{}
	for profile, username := range profiles {
		subject, party := dir.register(t, profile, username)
		s, _ := jwt.Issue(jwtauth.Claims{Subject: subject.String(), Username: username, PartyID: party.String(),
			ExpiresAt: fw.Now().Add(time.Hour).Unix()})
		tokens[profile] = "Bearer " + s
	}

	sw := hotswap.New(memory.NewStore("memory"))
	mod := parties.Compose(sw, memory.NewIdempotencyStore())
	mux := http.NewServeMux()
	mod.HTTP.RegisterRoutes(mux)
	srv := httptest.NewServer(distribution.Chain(mux, distribution.Correlation(),
		distribution.Authorize(jwt, authorization.NewResolver(dir, authorization.Options{}))))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { _ = sw.Close(context.Background()) })
	return &env{t: t, srv: srv, sw: sw, mod: mod, dir: dir, clock: clock, admin: tokens["admin"], reader: tokens["reader"],
		clerk: tokens["clerk"], viewer: tokens["viewer"], outsider: tokens["outsider"], seller: tokens["seller"]}
}

func (e *env) do(method, path, auth string, body any, out any) int {
	e.t.Helper()
	e.clock.Advance(time.Millisecond)
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, &buf)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	var raw bytes.Buffer
	_, _ = raw.ReadFrom(res.Body)
	if out != nil && res.StatusCode < 300 {
		if err := json.Unmarshal(raw.Bytes(), out); err != nil {
			e.t.Fatalf("%s %s: %v: %s", method, path, err, raw.String())
		}
	}
	return res.StatusCode
}

func (e *env) must(status, want int, what string) {
	e.t.Helper()
	if status != want {
		e.t.Fatalf("%s: status %d, want %d", what, status, want)
	}
}

// scenario runs the whole business flow on the current backend and returns the ids it created.
func (e *env) scenario(prefix string) (acme, ana papp.PartyDTO) {
	e.t.Helper()
	e.must(e.do("POST", "/api/organizations", e.admin, map[string]any{"legalName": prefix + " Acme Sociedad Anónima",
		"tradeName": prefix + " Acme", "roles": []string{domain.RoleInternalOrganization.String()}}, &acme), 201, "register acme")
	e.must(e.do("POST", "/api/persons", e.admin, map[string]any{"givenName": "Ana", "firstSurname": prefix + " García",
		"secondSurname": "López", "gender": "female", "birthDate": "1990-05-17", "roles": []string{domain.RoleEmployee.String()}}, &ana), 201, "register ana")
	e.grant("reader", authz.ReadOnly, acme.ID)
	if ana.Name != "Ana "+prefix+" García López" || acme.Name != prefix+" Acme" || len(ana.Roles) != 1 || ana.Roles[0].Name != "Employee" {
		e.t.Fatalf("registered: %+v %+v", ana, acme)
	}

	// Business rules come back as problems.
	e.must(e.do("POST", "/api/parties/"+acme.ID+"/roles", e.admin, map[string]any{"roleType": domain.RoleEmployee.String()}, nil),
		422, "an organization cannot be an employee")
	e.must(e.do("POST", "/api/persons", e.admin, map[string]any{"givenName": "X", "firstSurname": "Y",
		"roles": []string{domain.RoleLegalCategory.String()}}, nil), 422, "categories cannot be assigned")

	// Employment requires Employee -> Internal Organization.
	var rel papp.RelationshipDTO
	body := map[string]any{"type": domain.RelEmployment.String(), "fromParty": ana.ID, "toParty": acme.ID, "remark": "hired"}
	e.must(e.do("POST", "/api/party-relationships", e.admin, body, &rel), 201, "establish employment")
	if rel.TypeName != "Employment" || !rel.Active || rel.FromRole != domain.RoleEmployee.String() {
		e.t.Fatalf("relationship: %+v", rel)
	}
	e.must(e.do("POST", "/api/party-relationships", e.admin, body, nil), 422, "duplicate relationship")
	// A relationship gives each side the role it lacks (D-49), but never makes an internal
	// organization of anybody, nor gives a role to who cannot play it.
	e.must(e.do("POST", "/api/party-relationships", e.admin, map[string]any{"type": domain.RelEmployment.String(),
		"fromParty": acme.ID, "toParty": ana.ID}, nil), 422, "an organization is not made an employee, nor Ana an internal organization")
	e.must(e.do("POST", "/api/party-relationships", e.admin, map[string]any{"type": domain.RelCustomer.String(),
		"fromParty": ana.ID, "toParty": acme.ID}, nil), 201, "the customer relationship makes ana a customer")
	e.must(e.do("GET", "/api/parties/"+ana.ID, e.reader, nil, &ana), 200, "ana after the relationship")
	if !slices.ContainsFunc(ana.Roles, func(r papp.RoleDTO) bool { return r.RoleType == domain.RoleCustomer.String() }) {
		e.t.Fatalf("ana was not made a customer: %+v", ana.Roles)
	}

	// Ana becomes a bill-to customer too: a Customer by inheritance; a search for customers finds
	// her once.
	e.must(e.do("POST", "/api/parties/"+ana.ID+"/roles", e.admin, map[string]any{"roleType": domain.RoleBillToCustomer.String()}, &ana), 200, "assign role")
	var customers fw.Page[papp.PartyDTO]
	e.must(e.do("GET", "/api/parties?role="+domain.RoleCustomer.String()+"&q="+prefix, e.reader, nil, &customers), 200, "search customers")
	if customers.Total != 1 || customers.Items[0].ID != ana.ID {
		e.t.Fatalf("customers: %+v", customers)
	}

	// A role that ended does not count, unless the search asks for the ended ones too (D-48).
	e.must(e.do("POST", "/api/parties/"+ana.ID+"/roles", e.admin, map[string]any{"roleType": domain.RoleContractor.String()}, &ana), 200, "ana, contractor")
	contract := ana.Roles[slices.IndexFunc(ana.Roles, func(r papp.RoleDTO) bool { return r.RoleType == domain.RoleContractor.String() })].ID
	e.must(e.do("POST", "/api/parties/"+ana.ID+"/roles/"+contract+"/end", e.admin, map[string]any{}, &ana), 200, "no longer a contractor")
	var contractors fw.Page[papp.PartyDTO]
	byRole := "/api/parties?role=" + domain.RoleContractor.String() + "&q=" + prefix
	e.must(e.do("GET", byRole, e.reader, nil, &contractors), 200, "contractors now")
	if contractors.Total != 0 {
		e.t.Fatalf("an ended role counted: %+v", contractors)
	}
	e.must(e.do("GET", byRole+"&includeEndedRoles=true", e.reader, nil, &contractors), 200, "contractors ever")
	if contractors.Total != 1 || contractors.Items[0].ID != ana.ID {
		e.t.Fatalf("contractors ever: %+v", contractors)
	}

	// The reader may read but not write.
	e.must(e.do("PUT", "/api/parties/"+acme.ID+"/name", e.reader, map[string]any{"legalName": "Nope"}, nil), 403, "reader cannot rename")
	e.must(e.do("GET", "/api/parties/"+acme.ID, "", nil, nil), 401, "anonymous")

	// Rename, terminate, list.
	var renamed papp.PartyDTO
	e.must(e.do("PUT", "/api/parties/"+acme.ID+"/name", e.admin, map[string]any{"legalName": prefix + " Acme Sociedad Anónima",
		"tradeName": prefix + " Acme Group"}, &renamed), 200, "rename")
	if renamed.Name != prefix+" Acme Group" || renamed.ModifiedBy != "ana.admin" {
		e.t.Fatalf("renamed: %+v", renamed)
	}
	e.must(e.do("POST", "/api/party-relationships/"+rel.ID+"/terminate", e.admin, nil, &rel), 200, "terminate")
	var active []papp.RelationshipDTO
	e.must(e.do("GET", "/api/parties/"+ana.ID+"/relationships?active=true", e.reader, nil, &active), 200, "relationships")
	if len(active) != 1 || active[0].TypeName != "Customer Relationship" {
		e.t.Fatalf("active relationships: %+v", active)
	}

	// Directory (Open Host Service).
	var refs []contracts.PartyRef
	e.must(e.do("POST", "/api/parties/directory/resolve", e.reader, map[string]any{"partyIds": []string{ana.ID, acme.ID, fw.NewUUID().String(), "junk"}}, &refs), 200, "resolve")
	if len(refs) != 2 {
		e.t.Fatalf("directory: %+v", refs)
	}
	e.phase2(acme, ana)
	e.phase3(acme, ana)
	e.prospects(acme, ana)
	return acme, ana
}

// prospects covers the details a relationship carries because of its type (docs/PARTIES-UDM.md):
// the prospect relationship with an internal organization and its trial.
func (e *env) prospects(acme, ana papp.PartyDTO) {
	e.t.Helper()
	ctx := context.Background()
	var types []papp.RelationshipTypeDTO
	e.must(e.do("GET", "/api/catalogs/party-relationship-types", e.reader, nil, &types), 200, "relationship types")
	codes := map[string]string{}
	for _, t := range types {
		codes[t.Code] = t.ID
	}
	if len(types) != len(domain.WellKnownRelationshipTypes()) || codes["prospect"] != domain.RelProspect.String() || codes["customer"] != domain.RelCustomer.String() {
		e.t.Fatalf("relationship type codes: %+v", codes)
	}

	// A prospect is registered inside the scope with its trial: role, relationship, affiliation
	// and trial in one request.
	until := fw.Now().Add(30 * 24 * time.Hour).Truncate(time.Second)
	var flotas papp.PartyDTO
	e.must(e.do("POST", "/api/organizations", e.clerk, map[string]any{"legalName": "Flotas " + acme.ID[:8],
		"affiliation": map[string]any{"organization": acme.ID, "relationshipType": domain.RelProspect.String(),
			"prospect": map[string]any{"trialUntil": until}}}, &flotas), 201, "register a prospect with a trial")
	if len(flotas.Roles) != 1 || flotas.Roles[0].Name != "Prospect" || len(flotas.Organizations) != 1 || flotas.Organizations[0] != acme.ID {
		e.t.Fatalf("prospect: %+v", flotas)
	}
	var rels []papp.RelationshipDTO
	e.must(e.do("GET", "/api/parties/"+flotas.ID+"/relationships", e.reader, nil, &rels), 200, "the prospect's relationships")
	if len(rels) != 1 || rels[0].TypeName != "Prospect Relationship" || rels[0].Prospect == nil || !rels[0].Prospect.InTrial ||
		rels[0].Prospect.TrialUntil == nil || !rels[0].Prospect.TrialUntil.Equal(until) {
		e.t.Fatalf("prospect relationship: %+v", rels)
	}
	trial := "/api/party-relationships/" + rels[0].ID + "/trial"

	// Extending the trial needs the permission and a writable scope.
	longer := until.Add(15 * 24 * time.Hour)
	var rel papp.RelationshipDTO
	e.must(e.do("PUT", trial, e.reader, map[string]any{"trialUntil": longer}, nil), 403, "the reader lacks the permission")

	// Granting a trial is its own permission: a seller creates and updates everything else in the
	// organization, but gives no time away, neither afterwards nor with the relationship.
	e.grant("seller", authz.Full, acme.ID)
	e.must(e.do("PUT", trial, e.seller, map[string]any{"trialUntil": longer}, nil), 403, "a seller cannot extend a trial")
	lead := map[string]any{"organization": acme.ID, "relationshipType": domain.RelProspect.String(), "prospect": map[string]any{"trialUntil": until}}
	e.must(e.do("POST", "/api/organizations", e.seller, map[string]any{"legalName": "Lead " + acme.ID[:8], "affiliation": lead}, nil),
		403, "nor register a prospect with a trial")
	delete(lead, "prospect")
	e.must(e.do("POST", "/api/organizations", e.seller, map[string]any{"legalName": "Lead " + acme.ID[:8], "affiliation": lead}, nil),
		201, "a seller registers a prospect without trial")
	e.must(e.do("PUT", trial, e.viewer, map[string]any{"trialUntil": longer}, nil), 403, "a read-only grant cannot extend")
	e.must(e.do("PUT", trial, e.outsider, map[string]any{"trialUntil": longer}, nil), 404, "out of scope")
	e.must(e.do("PUT", trial, e.clerk, map[string]any{"trialUntil": fw.Now().Add(-365 * 24 * time.Hour)}, nil), 400, "before the relationship starts")
	e.must(e.do("PUT", trial, e.clerk, map[string]any{"trialUntil": longer}, &rel), 200, "extend the trial")
	if rel.Prospect == nil || !rel.Prospect.TrialUntil.Equal(longer) || !rel.Prospect.InTrial || rel.Version != rels[0].Version+1 {
		e.t.Fatalf("extended: %+v", rel)
	}
	trials, err := e.mod.Trials.Trials(ctx, acme.ID, []string{flotas.ID, ana.ID, "junk"})
	if err != nil || len(trials) != 1 || !trials[flotas.ID].InForce || !trials[flotas.ID].Until.Equal(longer) || trials[flotas.ID].RelationshipID != rel.ID {
		e.t.Fatalf("trials port: %+v %v", trials, err)
	}

	// Only prospect relationships have a trial; the other types keep working without details.
	var anas []papp.RelationshipDTO
	e.must(e.do("GET", "/api/parties/"+ana.ID+"/relationships?active=true", e.reader, nil, &anas), 200, "ana's relationships")
	if len(anas) != 1 || anas[0].Prospect != nil {
		e.t.Fatalf("a customer relationship carries no prospect details: %+v", anas)
	}
	e.must(e.do("PUT", "/api/party-relationships/"+anas[0].ID+"/trial", e.admin, map[string]any{"trialUntil": longer}, nil), 422, "a customer relationship has no trial")

	// Establishing the relationship afterwards, with or without trial.
	var bea papp.PartyDTO
	e.must(e.do("POST", "/api/persons", e.admin, map[string]any{"givenName": "Bea", "firstSurname": "Prueba " + acme.ID[:8],
		"roles": []string{domain.RoleProspect.String()}}, &bea), 201, "register bea")
	body := map[string]any{"type": domain.RelProspect.String(), "fromParty": bea.ID, "toParty": acme.ID}
	rel = papp.RelationshipDTO{} // decoding into a used value would keep the fields the response omits
	e.must(e.do("POST", "/api/party-relationships", e.admin, body, &rel), 201, "a prospect without trial")
	if rel.Prospect == nil || rel.Prospect.InTrial || rel.Prospect.TrialUntil != nil {
		e.t.Fatalf("no trial: %+v", rel.Prospect)
	}
	e.must(e.do("POST", "/api/party-relationships", e.admin, map[string]any{"type": domain.RelEmployment.String(), "fromParty": ana.ID,
		"toParty": acme.ID, "prospect": map[string]any{"trialUntil": until}}, nil), 422, "employment has no trial")
	e.must(e.do("PUT", "/api/party-relationships/"+rel.ID+"/trial", e.clerk, map[string]any{"trialUntil": until}, &rel), 200, "grant the trial later")
	withdraw := "/api/party-relationships/" + rel.ID + "/trial"
	rel = papp.RelationshipDTO{}
	e.must(e.do("PUT", withdraw, e.clerk, map[string]any{"trialUntil": nil}, &rel), 200, "withdraw it")
	if rel.Prospect.InTrial || rel.Prospect.TrialUntil != nil {
		e.t.Fatalf("withdrawn: %+v", rel.Prospect)
	}

	// Converting the prospect into a customer: the customer relationship starts and the prospect
	// relationship ends, and with it the trial. The party stays in the organization's scope.
	e.must(e.do("POST", "/api/parties/"+flotas.ID+"/roles", e.clerk, map[string]any{"roleType": domain.RoleCustomer.String()}, nil), 200, "customer role")
	e.must(e.do("POST", "/api/party-relationships", e.clerk, map[string]any{"type": domain.RelCustomer.String(),
		"fromParty": flotas.ID, "toParty": acme.ID}, nil), 201, "customer relationship")
	rel = papp.RelationshipDTO{}
	e.must(e.do("POST", "/api/party-relationships/"+rels[0].ID+"/terminate", e.clerk, nil, &rel), 200, "end the prospect relationship")
	if rel.Prospect == nil || rel.Prospect.InTrial || !rel.Prospect.TrialUntil.Equal(longer) {
		e.t.Fatalf("an ended prospect relationship keeps its trial but is not in trial: %+v", rel.Prospect)
	}
	e.must(e.do("PUT", trial, e.clerk, map[string]any{"trialUntil": longer.Add(time.Hour)}, nil), 422, "an ended relationship")
	e.must(e.do("GET", "/api/parties/"+flotas.ID, e.clerk, nil, nil), 200, "still visible as a customer")
	if trials, err = e.mod.Trials.Trials(ctx, acme.ID, []string{flotas.ID, bea.ID}); err != nil || len(trials) != 1 || trials[bea.ID].InForce || trials[bea.ID].Until != nil {
		e.t.Fatalf("trials after the conversion: %+v %v", trials, err)
	}

	// The personal details of a person can be corrected after registration.
	var edited papp.PartyDTO
	person := "/api/parties/" + bea.ID + "/person"
	e.must(e.do("PUT", person, e.admin, map[string]any{"gender": "female", "birthDate": "1988-02-29", "maritalStatus": "married"}, &edited), 200, "edit the person")
	if edited.Person.Gender != "female" || edited.Person.BirthDate != "1988-02-29" || edited.Person.MaritalStatus != "married" || edited.Name != bea.Name {
		e.t.Fatalf("edited person: %+v", edited.Person)
	}
	e.must(e.do("PUT", person, e.admin, map[string]any{"birthDate": "2999-01-01"}, nil), 400, "born in the future")
	e.must(e.do("PUT", person, e.admin, map[string]any{"gender": "x"}, nil), 400, "unknown gender")
	e.must(e.do("PUT", person, e.viewer, map[string]any{"gender": "male"}, nil), 403, "a read-only grant cannot edit")
	e.must(e.do("PUT", person, e.outsider, map[string]any{"gender": "male"}, nil), 404, "out of scope")
	e.must(e.do("PUT", "/api/parties/"+acme.ID+"/person", e.admin, map[string]any{"gender": "female"}, nil), 422, "an organization has no personal details")
	edited = papp.PartyDTO{}
	e.must(e.do("PUT", person, e.clerk, map[string]any{"maritalStatus": "single"}, &edited), 200, "the fields left out become unknown")
	if edited.Person.Gender != "" || edited.Person.BirthDate != "" || edited.Person.MaritalStatus != "single" {
		e.t.Fatalf("replaced person details: %+v", edited.Person)
	}

	// Ownership: the stake of a shareholder is a detail of the ownership relationship.
	var sara papp.PartyDTO
	e.must(e.do("POST", "/api/persons", e.admin, map[string]any{"givenName": "Sara", "firstSurname": "Socia " + acme.ID[:8],
		"roles": []string{domain.RoleShareholder.String()}}, &sara), 201, "register sara")
	owns := map[string]any{"type": domain.RelOwnership.String(), "fromParty": sara.ID, "toParty": acme.ID}
	owns["ownership"] = map[string]any{"share": "130"}
	e.must(e.do("POST", "/api/party-relationships", e.admin, owns, nil), 400, "more than the whole company")
	owns["ownership"] = map[string]any{"share": "30"}
	var own papp.RelationshipDTO
	e.must(e.do("POST", "/api/party-relationships", e.admin, owns, &own), 201, "sara owns 30 % of acme")
	if own.Ownership == nil || own.Ownership.Share != "30.00" || own.Prospect != nil {
		e.t.Fatalf("ownership: %+v", own)
	}
	share := "/api/party-relationships/" + own.ID + "/ownership"
	e.must(e.do("PUT", share, e.viewer, map[string]any{"share": "45.5"}, nil), 403, "a read-only grant cannot change the share")
	e.must(e.do("PUT", share, e.clerk, map[string]any{"share": "a third"}, nil), 400, "not a number")
	e.must(e.do("PUT", share, e.clerk, map[string]any{"share": "33.333"}, nil), 400, "two decimals at most")
	e.must(e.do("PUT", "/api/party-relationships/"+own.ID+"/trial", e.clerk, map[string]any{"trialUntil": until}, nil), 422, "an ownership has no trial")
	own = papp.RelationshipDTO{}
	e.must(e.do("PUT", share, e.seller, map[string]any{"share": "45.5"}, &own), 200, "the share is an update: a seller corrects it")
	if own.Ownership.Share != "45.50" {
		e.t.Fatalf("corrected share: %+v", own.Ownership)
	}
	var saras []papp.RelationshipDTO
	e.must(e.do("GET", "/api/parties/"+sara.ID+"/relationships", e.reader, nil, &saras), 200, "sara's relationships")
	if len(saras) != 1 || saras[0].Ownership == nil || saras[0].Ownership.Share != "45.50" {
		e.t.Fatalf("share round trip: %+v", saras)
	}
	own = papp.RelationshipDTO{}
	e.must(e.do("PUT", share, e.clerk, map[string]any{"share": nil}, &own), 200, "clear the share")
	if own.Ownership == nil || own.Ownership.Share != "" {
		e.t.Fatalf("cleared share: %+v", own.Ownership)
	}

	// Promotion code: the code of a collaborator is a detail of its relationship with the company.
	var agency papp.PartyDTO
	e.must(e.do("POST", "/api/organizations", e.admin, map[string]any{"legalName": "Agencia " + acme.ID[:8],
		"roles": []string{domain.RoleCollaborator.String()}}, &agency), 201, "register the agency")
	var refers papp.RelationshipDTO
	e.must(e.do("POST", "/api/party-relationships", e.admin, map[string]any{"type": domain.RelCollaborator.String(), "fromParty": agency.ID,
		"toParty": acme.ID}, &refers), 201, "the agency refers business for acme")
	if refers.Collaborator == nil || refers.Collaborator.PromotionCode != "" || refers.Ownership != nil {
		e.t.Fatalf("collaborator: %+v", refers)
	}
	promo := "/api/party-relationships/" + refers.ID + "/promotion-code"
	e.must(e.do("PUT", promo, e.viewer, map[string]any{"promotionCode": "SOL-1"}, nil), 403, "a read-only grant cannot give a code")
	e.must(e.do("PUT", promo, e.clerk, map[string]any{"promotionCode": "sol 1"}, nil), 400, "no spaces")
	e.must(e.do("PUT", promo, e.clerk, map[string]any{"promotionCode": "ABCDEFGHIJKLMNOP"}, nil), 400, "fifteen characters at most")
	e.must(e.do("PUT", "/api/party-relationships/"+own.ID+"/promotion-code", e.clerk, map[string]any{"promotionCode": "X1"}, nil), 422, "an ownership has no promotion code")
	refers = papp.RelationshipDTO{}
	e.must(e.do("PUT", promo, e.clerk, map[string]any{"promotionCode": " sol-1 "}, &refers), 200, "the code, kept in capitals")
	if refers.Collaborator.PromotionCode != "SOL-1" {
		e.t.Fatalf("promotion code: %+v", refers.Collaborator)
	}
	who, found, err := e.mod.Collaborators.ByPromotionCode(context.Background(), acme.ID, "Sol-1")
	if err != nil || !found || who.PartyID != agency.ID || who.RelationshipID != refers.ID {
		e.t.Fatalf("whose the code is: %+v %v %v", who, found, err)
	}
	if _, found, _ := e.mod.Collaborators.ByPromotionCode(context.Background(), agency.ID, "SOL-1"); found {
		e.t.Fatal("the code is of the collaborators of acme, of nobody else's")
	}
	refers = papp.RelationshipDTO{}
	e.must(e.do("PUT", promo, e.clerk, map[string]any{"promotionCode": ""}, &refers), 200, "take the code away")
	if _, found, _ := e.mod.Collaborators.ByPromotionCode(context.Background(), acme.ID, "SOL-1"); found || refers.Collaborator.PromotionCode != "" {
		e.t.Fatalf("a code taken away is nobody's: %+v", refers.Collaborator)
	}

	// The mark of a refused identity document: who has it is found by it, and a document that
	// passes its checks answers it.
	var marked papp.PartyDTO
	issue := "/api/parties/" + agency.ID + "/document-issue"
	e.must(e.do("PUT", issue, e.reader, map[string]any{"number": "B12345678", "reason": "wrong check digit"}, nil), 403, "a reader marks nothing")
	e.must(e.do("PUT", issue, e.admin, map[string]any{"number": "B12345678"}, nil), 400, "a mark says why")
	e.must(e.do("PUT", issue, e.admin, map[string]any{"number": " B12345678 ", "reason": "the CIF does not pass its check digits"}, &marked), 200, "mark")
	if marked.DocumentIssue == nil || marked.DocumentIssue.Number != "B12345678" || marked.DocumentIssue.Reason == "" {
		e.t.Fatalf("marked: %+v", marked.DocumentIssue)
	}
	var withIssue fw.Page[papp.PartyDTO]
	e.must(e.do("GET", "/api/parties?documentIssue=true", e.admin, nil, &withIssue), 200, "the parties with a refused document")
	if withIssue.Total != 1 || withIssue.Items[0].ID != agency.ID {
		e.t.Fatalf("parties with a refused document: %+v", withIssue.Items)
	}
	e.must(e.do("PUT", issue, e.outsider, map[string]any{"reason": "x"}, nil), 404, "nobody marks what it does not see")
	e.must(e.do("DELETE", issue, e.outsider, nil, nil), 404, "nor takes its mark away")
	marked = papp.PartyDTO{} // decoding leaves what the answer does not bring
	e.must(e.do("POST", "/api/parties/"+agency.ID+"/identifications", e.admin, map[string]any{"documentType": domain.DocOther.String(),
		"country": "ES", "number": "B12345678"}, &marked), 200, "the wrong number, kept as other document")
	if marked.DocumentIssue == nil {
		e.t.Fatal("an other document, which nobody checks, answers no mark")
	}
	marked = papp.PartyDTO{}
	e.must(e.do("POST", "/api/parties/"+agency.ID+"/identifications", e.admin, map[string]any{"documentType": domain.DocPassport.String(),
		"country": "ES", "number": "PAB765432", "issuedOn": "2021-02-03", "expiresOn": "2031-02-02", "issuingAuthority": "Policía Nacional"}, &marked), 200, "a passport")
	if marked.DocumentIssue == nil {
		e.t.Fatal("a passport is not the document that was wrong: it answers no mark")
	}
	marked = papp.PartyDTO{} // decoding leaves what the answer does not bring
	e.must(e.do("POST", "/api/parties/"+agency.ID+"/identifications", e.admin, map[string]any{"documentType": domain.DocTaxID.String(),
		"country": "ES", "number": "A58818501"}, &marked), 200, "the right tax id")
	if marked.DocumentIssue != nil {
		e.t.Fatalf("a document that passes its checks answers the mark: %+v", marked.DocumentIssue)
	}
	e.must(e.do("PUT", issue, e.admin, map[string]any{"reason": "looked at by hand"}, &marked), 200, "mark again")
	if marked.DocumentIssue == nil || marked.DocumentIssue.Number != "" {
		e.t.Fatalf("marked again, without a number: %+v", marked.DocumentIssue)
	}
	marked = papp.PartyDTO{}
	e.must(e.do("DELETE", issue, e.admin, nil, &marked), 200, "take the mark away")
	e.must(e.do("GET", "/api/parties?documentIssue=true", e.admin, nil, &withIssue), 200, "nobody is marked now")
	if marked.DocumentIssue != nil || withIssue.Total != 0 {
		e.t.Fatalf("mark taken away: %+v, %d still marked", marked.DocumentIssue, withIssue.Total)
	}

	// Customer number: each organization numbers its own customers, so it is a detail of the
	// customer relationship. It is of one customer for good.
	customerOf := func(name string) (papp.PartyDTO, papp.RelationshipDTO) {
		var p papp.PartyDTO
		e.must(e.do("POST", "/api/organizations", e.admin, map[string]any{"legalName": name + " " + acme.ID[:8],
			"roles": []string{domain.RoleCustomer.String()}}, &p), 201, "register "+name)
		var r papp.RelationshipDTO
		e.must(e.do("POST", "/api/party-relationships", e.admin, map[string]any{"type": domain.RelCustomer.String(), "fromParty": p.ID,
			"toParty": acme.ID}, &r), 201, name+" buys from acme")
		return p, r
	}
	talleres, buys := customerOf("Talleres")
	_, buysToo := customerOf("Ferreteria")
	if buys.Customer == nil || buys.Customer.Number != "" || buys.Collaborator != nil {
		e.t.Fatalf("customer: %+v", buys)
	}
	number := "/api/party-relationships/" + buys.ID + "/customer-number"
	e.must(e.do("PUT", number, e.viewer, map[string]any{"customerNumber": "4300001"}, nil), 403, "a read-only grant cannot number a customer")
	e.must(e.do("PUT", number, e.outsider, map[string]any{"customerNumber": "4300001"}, nil), 404, "nobody numbers a customer it does not see")
	e.must(e.do("PUT", number, e.clerk, map[string]any{"customerNumber": "43 00001"}, nil), 400, "no spaces")
	e.must(e.do("PUT", number, e.clerk, map[string]any{"customerNumber": "430000000000000000001"}, nil), 400, "twenty characters at most")
	e.must(e.do("PUT", "/api/party-relationships/"+refers.ID+"/customer-number", e.clerk, map[string]any{"customerNumber": "1"}, nil), 422,
		"a collaborator has no customer number")
	buys = papp.RelationshipDTO{}
	e.must(e.do("PUT", number, e.clerk, map[string]any{"customerNumber": " c-4300001 "}, &buys), 200, "the number, kept in capitals")
	if buys.Customer.Number != "C-4300001" {
		e.t.Fatalf("customer number: %+v", buys.Customer)
	}
	e.must(e.do("PUT", number, e.clerk, map[string]any{"customerNumber": "C-4300001"}, nil), 200, "the same number again")
	e.must(e.do("PUT", "/api/party-relationships/"+buysToo.ID+"/customer-number", e.clerk, map[string]any{"customerNumber": "c-4300001"}, nil), 422,
		"the number of another customer of acme")
	e.must(e.do("PUT", "/api/party-relationships/"+buysToo.ID+"/customer-number", e.clerk, map[string]any{"customerNumber": "C-4300002"}, nil), 200,
		"its own number")

	// A number given up is free again: only a number some relationship carries is taken.
	e.must(e.do("PUT", "/api/party-relationships/"+buysToo.ID+"/customer-number", e.clerk, map[string]any{"customerNumber": "C-4300003"}, nil), 200,
		"another number")
	e.must(e.do("PUT", number, e.clerk, map[string]any{"customerNumber": "C-4300002"}, nil), 200, "the number the other gave up")
	e.must(e.do("PUT", number, e.clerk, map[string]any{"customerNumber": "C-4300001"}, nil), 200, "and its own again")

	// The number stays with the customer when the relationship ends: it is not handed to another
	// one, and the customer gets it again if it comes back.
	e.must(e.do("POST", "/api/party-relationships/"+buys.ID+"/terminate", e.admin, map[string]any{}, nil), 200, "talleres stops buying")
	e.must(e.do("PUT", number, e.clerk, map[string]any{"customerNumber": "C-9"}, nil), 422, "an ended relationship keeps its number")
	e.must(e.do("PUT", "/api/party-relationships/"+buysToo.ID+"/customer-number", e.clerk, map[string]any{"customerNumber": "C-4300001"}, nil), 422,
		"the number of a customer that left is still its own")
	var back papp.RelationshipDTO
	e.must(e.do("POST", "/api/party-relationships", e.admin, map[string]any{"type": domain.RelCustomer.String(), "fromParty": talleres.ID,
		"toParty": acme.ID}, &back), 201, "talleres comes back")
	e.must(e.do("PUT", "/api/party-relationships/"+back.ID+"/customer-number", e.clerk, map[string]any{"customerNumber": "C-4300001"}, &back), 200,
		"and has its number again")
	if back.Customer.Number != "C-4300001" {
		e.t.Fatalf("the number of the customer that came back: %+v", back.Customer)
	}
}

// phase3 covers organization scope (decision P1), registration inside the scope, the
// organization hierarchy and the ports for other contexts.
func (e *env) phase3(acme, ana papp.PartyDTO) {
	e.t.Helper()
	ctx := context.Background()
	var globex, pedro, got papp.PartyDTO
	e.must(e.do("POST", "/api/organizations", e.admin, map[string]any{"legalName": "Globex " + acme.ID[:8], "legalForm": "corporation",
		"roles": []string{domain.RoleInternalOrganization.String()}}, &globex), 201, "register globex")
	if globex.Organization.LegalForm != "corporation" {
		e.t.Fatalf("legal form: %+v", globex.Organization)
	}
	e.grant("clerk", authz.Full, acme.ID)
	e.grant("viewer", authz.ReadOnly, acme.ID)
	e.grant("outsider", authz.Full, globex.ID)

	// Registration inside the scope: the customer role comes with the relationship.
	e.must(e.do("POST", "/api/persons", e.clerk, map[string]any{"givenName": "Pedro", "firstSurname": "Ruiz"}, nil), 400, "affiliation required")
	e.must(e.do("POST", "/api/persons", e.clerk, map[string]any{"givenName": "Pedro", "firstSurname": "Ruiz",
		"affiliation": map[string]any{"organization": globex.ID, "relationshipType": domain.RelCustomer.String()}}, nil), 404, "globex is out of scope")
	e.must(e.do("POST", "/api/persons", e.clerk, map[string]any{"givenName": "Pedro", "firstSurname": "Ruiz",
		"affiliation": map[string]any{"organization": acme.ID, "relationshipType": domain.RelCustomer.String()}}, &pedro), 201, "register in scope")
	if len(pedro.Organizations) != 1 || pedro.Organizations[0] != acme.ID || len(pedro.Roles) != 1 || pedro.Roles[0].Name != "Customer" {
		e.t.Fatalf("pedro: %+v", pedro)
	}

	// Visibility: 404 out of scope, 403 read-only.
	e.must(e.do("GET", "/api/parties/"+pedro.ID, e.outsider, nil, nil), 404, "outsider cannot see pedro")
	e.must(e.do("GET", "/api/parties/"+pedro.ID, e.reader, nil, &got), 200, "reader sees pedro through acme")
	e.must(e.do("PUT", "/api/parties/"+pedro.ID+"/name", e.viewer, map[string]any{"givenName": "Pedro", "firstSurname": "Ruiz", "secondSurname": "Gil"}, nil),
		403, "a read-only grant cannot write")
	e.must(e.do("PUT", "/api/parties/"+pedro.ID+"/name", e.clerk, map[string]any{"givenName": "Pedro", "firstSurname": "Ruiz", "secondSurname": "Gil"}, nil),
		200, "a full grant writes")
	var page fw.Page[papp.PartyDTO]
	e.must(e.do("GET", "/api/parties?q=Pedro", e.outsider, nil, &page), 200, "outsider search")
	if page.Total != 0 {
		e.t.Fatalf("the outsider must not find pedro: %+v", page)
	}
	e.must(e.do("GET", "/api/parties?organization="+acme.ID+"&kind=person", e.clerk, nil, &page), 200, "members of acme")
	if page.Total != 2 { // ana (employee, customer) and pedro
		e.t.Fatalf("acme members: %d", page.Total)
	}

	// A party without relationships is only for global administrators, unless it is shared.
	var loner papp.PartyDTO
	e.must(e.do("POST", "/api/organizations", e.admin, map[string]any{"legalName": "Banco Común " + acme.ID[:8],
		"roles": []string{domain.RoleFinancialInstitution.String()}}, &loner), 201, "register a bank")
	e.must(e.do("GET", "/api/parties/"+loner.ID, e.reader, nil, nil), 404, "unrelated party")
	e.must(e.do("PUT", "/api/parties/"+loner.ID+"/shared", e.clerk, map[string]any{"shared": true}, nil), 404, "only admins share")
	e.must(e.do("PUT", "/api/parties/"+loner.ID+"/shared", e.admin, map[string]any{"shared": true}, nil), 200, "share the bank")
	e.must(e.do("GET", "/api/parties/"+loner.ID, e.outsider, nil, nil), 200, "a shared party is visible to everyone")
	e.must(e.do("PUT", "/api/parties/"+loner.ID+"/name", e.clerk, map[string]any{"legalName": "Nope"}, nil), 403, "shared parties are read-only")

	// Hierarchy: Madrid office -> Sales division -> Acme.
	var sales, madrid papp.PartyDTO
	e.must(e.do("POST", "/api/organizations", e.admin, map[string]any{"legalName": "Acme Sales " + acme.ID[:8],
		"roles": []string{domain.RoleDivision.String()}}, &sales), 201, "sales")
	e.must(e.do("POST", "/api/organizations", e.admin, map[string]any{"legalName": "Acme Madrid " + acme.ID[:8],
		"roles": []string{domain.RoleDepartment.String()}}, &madrid), 201, "madrid")
	rollup := func(child, parent string) int {
		return e.do("POST", "/api/party-relationships", e.admin, map[string]any{"type": domain.RelOrganizationRollup.String(),
			"fromParty": child, "toParty": parent}, nil)
	}
	e.must(rollup(sales.ID, acme.ID), 201, "sales under acme")
	e.must(rollup(madrid.ID, sales.ID), 201, "madrid under sales")
	e.must(rollup(madrid.ID, acme.ID), 422, "one parent")
	e.must(rollup(sales.ID, madrid.ID), 422, "no cycles")
	e.must(rollup(ana.ID, acme.ID), 422, "a person is not an organization unit")

	orgs := e.mod.Organizations
	desc, err := orgs.Descendants(ctx, []string{acme.ID})
	if err != nil || len(desc) != 3 {
		e.t.Fatalf("descendants: %v %v", desc, err)
	}
	legal, err := orgs.InternalOrganizationOf(ctx, []string{madrid.ID, pedro.ID})
	if err != nil || legal[madrid.ID].ID != acme.ID || len(legal) != 1 {
		e.t.Fatalf("internal organization of: %+v %v", legal, err)
	}
	member, err := orgs.InternalOrganizations(ctx, []string{pedro.ID, loner.ID})
	if err != nil || len(member[pedro.ID]) != 1 || member[pedro.ID][0] != acme.ID || len(member[loner.ID]) != 0 {
		e.t.Fatalf("membership: %+v %v", member, err)
	}
	var internal []papp.PartyDTO
	e.must(e.do("GET", "/api/internal-organizations", e.clerk, nil, &internal), 200, "internal organizations")
	if len(internal) != 1 || internal[0].ID != acme.ID {
		e.t.Fatalf("the clerk's internal organizations: %+v", internal)
	}

	// Ending the relationship ends the visibility.
	var rels []papp.RelationshipDTO
	e.must(e.do("GET", "/api/parties/"+pedro.ID+"/relationships", e.clerk, nil, &rels), 200, "pedro's relationships")
	if len(rels) != 1 {
		e.t.Fatalf("relationships: %+v", rels)
	}
	e.must(e.do("POST", "/api/party-relationships/"+rels[0].ID+"/terminate", e.clerk, nil, nil), 200, "end the customer relationship")
	e.must(e.do("GET", "/api/parties/"+pedro.ID, e.clerk, nil, nil), 404, "pedro left acme's scope")
}

// phase2 covers identifications, contacts and classifications through HTTP.
func (e *env) phase2(acme, ana papp.PartyDTO) {
	e.t.Helper()
	var opts []papp.DocumentOptionDTO
	e.must(e.do("GET", "/api/catalogs/document-types?country=ES", e.reader, nil, &opts), 200, "document options")
	if len(opts) != 5 || opts[0].Code != "NIDN" || !opts[0].Default {
		e.t.Fatalf("ES options: %+v", opts)
	}
	var got papp.PartyDTO
	e.must(e.do("POST", "/api/parties/"+ana.ID+"/identifications", e.admin, map[string]any{"documentType": domain.DocNationalID.String(),
		"country": "ES", "number": "12345678-z"}, &got), 200, "add DNI")
	if len(got.Identifications) != 1 || got.Identifications[0].Number != "12345678Z" || !got.Identifications[0].Primary || got.Identifications[0].Code != "NIDN" {
		e.t.Fatalf("identification: %+v", got.Identifications)
	}
	e.must(e.do("POST", "/api/parties/"+ana.ID+"/identifications", e.admin, map[string]any{"documentType": domain.DocNationalID.String(),
		"country": "ES", "number": "12345678A"}, nil), 400, "wrong check letter")
	var luis papp.PartyDTO
	e.must(e.do("POST", "/api/persons", e.admin, map[string]any{"givenName": "Luis", "firstSurname": "Pérez"}, &luis), 201, "register luis")
	e.must(e.do("POST", "/api/parties/"+luis.ID+"/identifications", e.admin, map[string]any{"documentType": domain.DocNationalID.String(),
		"country": "ES", "number": "12345678Z"}, nil), 422, "a document identifies one party")
	e.must(e.do("POST", "/api/parties/"+acme.ID+"/identifications", e.admin, map[string]any{"documentType": domain.DocTaxID.String(),
		"country": "ES", "number": "B12345674"}, nil), 200, "acme tax id")

	e.must(e.do("POST", "/api/parties/"+ana.ID+"/contacts", e.admin, map[string]any{"kind": "email", "value": "Ana@Example.com",
		"purposes": []string{"default"}}, &got), 200, "email")
	e.must(e.do("POST", "/api/parties/"+ana.ID+"/contacts", e.admin, map[string]any{"kind": "postal", "purposes": []string{"billing", "home"},
		"address": map[string]any{"streetType": "CL", "line1": "Mayor 1", "postalCode": "28013", "locality": "Madrid", "country": "ES"}}, &got), 200, "address")
	e.must(e.do("POST", "/api/parties/"+ana.ID+"/contacts", e.admin, map[string]any{"kind": "email", "value": "ana@example.com"}, nil), 422, "duplicate e-mail")
	if len(got.Contacts) != 2 || got.Contacts[0].Value != "ana@example.com" || got.Contacts[1].Address == nil || got.Contacts[1].Address.Locality != "Madrid" {
		e.t.Fatalf("contacts: %+v", got.Contacts)
	}
	// Where in the street, piece by piece: stored and read back as given, each piece optional.
	var withPieces papp.PartyDTO
	e.must(e.do("POST", "/api/parties/"+acme.ID+"/contacts", e.admin, map[string]any{"kind": "postal", "purposes": []string{"shipping"},
		"address": map[string]any{"streetType": "CL", "line1": "Mayor", "number": " 5 ", "numberQualifier": "bis", "block": "2", "portal": "A",
			"staircase": "B", "floor": "3", "door": "izquierda", "postalCode": "28013", "locality": "Madrid", "country": "ES"}}, &withPieces), 200, "address with its pieces")
	var pieces *papp.AddressDTO
	for _, c := range withPieces.Contacts {
		if c.Address != nil && c.Address.Line1 == "Mayor" {
			pieces = c.Address
		}
	}
	if pieces == nil || pieces.Number != "5" || pieces.NumberQualifier != "bis" || pieces.Block != "2" || pieces.Portal != "A" ||
		pieces.Staircase != "B" || pieces.Floor != "3" || pieces.Door != "izquierda" {
		e.t.Fatalf("address with its pieces: %+v", pieces)
	}
	e.must(e.do("POST", "/api/parties/"+acme.ID+"/contacts", e.admin, map[string]any{"kind": "postal",
		"address": map[string]any{"line1": "Mayor", "floor": "the third one", "country": "ES"}}, nil), 400, "a piece too long")
	if got.Contacts[1].Address.Number != "" || got.Contacts[1].Address.Door != "" {
		e.t.Fatalf("an address without pieces has none: %+v", got.Contacts[1].Address)
	}

	e.must(e.do("POST", "/api/parties/"+ana.ID+"/contacts/"+got.Contacts[0].ID+"/end", e.admin, nil, &got), 200, "end e-mail")
	if got.Contacts[0].Active || len(got.Contacts[0].Purposes) != 0 {
		e.t.Fatalf("ended contact: %+v", got.Contacts[0])
	}

	e.must(e.do("POST", "/api/parties/"+acme.ID+"/classifications", e.admin, map[string]any{"classification": domain.ClassCorporate.String()}, &got), 200, "segment")
	e.must(e.do("POST", "/api/parties/"+acme.ID+"/classifications", e.admin, map[string]any{"classification": domain.ClassRetail.String()}, nil), 422, "one segment")
	e.must(e.do("POST", "/api/parties/"+ana.ID+"/classifications", e.admin, map[string]any{"classification": domain.ClassSizeMicro.String()}, nil), 422, "persons have no size")
	if len(got.Classifications) != 1 || got.Classifications[0].Name != "Corporate" {
		e.t.Fatalf("classifications: %+v", got.Classifications)
	}

	var page fw.Page[papp.PartyDTO]
	e.must(e.do("GET", "/api/parties?document=12345678-Z", e.reader, nil, &page), 200, "search by document")
	if page.Total != 1 || page.Items[0].ID != ana.ID {
		e.t.Fatalf("by document: %+v", page)
	}
	e.must(e.do("GET", "/api/parties?classification="+domain.ClassCorporate.String(), e.reader, nil, &page), 200, "search by classification")
	if page.Total != 1 || page.Items[0].ID != acme.ID {
		e.t.Fatalf("by classification: %+v", page)
	}
}

// The whole scenario runs twice: over the in-memory security directory of the framework and over
// the Security bounded context. Nothing in Parties changes between the two: its use cases read
// the authorization context, not where it comes from.
func TestParties_EndToEnd_MemoryThenSQLite(t *testing.T) {
	t.Run("in-memory directory", func(t *testing.T) { endToEnd(t, newMemoryDirectory) })
	t.Run("Security directory", func(t *testing.T) { endToEnd(t, newSecurityDirectory) })
}

// roleTypes adds role types to the catalog: a role without data of its own is a row placed under
// its parent, and from then on it is assigned like any other.
func (e *env) roleTypes(tag string) {
	t := e.t
	t.Helper()
	var all []papp.RoleTypeDTO
	e.must(e.do("GET", "/api/catalogs/party-role-types", e.reader, nil, &all), 200, "role types")
	seeded := len(all)
	customer := papp.RoleTypeDTO{}
	for _, rt := range all {
		if rt.ID == domain.RoleCustomer.String() {
			customer = rt
		}
		if rt.Code == "" || !rt.WellKnown {
			t.Fatalf("a role type of the program without its code: %+v", rt)
		}
	}
	if customer.Code != "customer" {
		t.Fatalf("the code of Customer: %+v", customer)
	}

	guarantor := map[string]any{"name": "  Guarantor ", "description": "A person who answers for the debt of another", "code": "guarantor",
		"parent": domain.RolePersonCategory.String()}
	e.must(e.do("POST", "/api/catalogs/party-role-types", e.reader, guarantor, nil), 403, "without the permission")
	e.must(e.do("POST", "/api/catalogs/party-role-types", e.clerk, guarantor, nil), 403, "the catalog is of the whole installation")
	var added papp.RoleTypeDTO
	e.must(e.do("POST", "/api/catalogs/party-role-types", e.admin, guarantor, &added), 201, "a role type under Person")
	if added.Name != "Guarantor" || added.Code != "guarantor" || added.Parent != domain.RolePersonCategory.String() || added.Category ||
		added.WellKnown || added.Applicability != "person" {
		t.Fatalf("added: %+v", added)
	}
	e.must(e.do("GET", "/api/catalogs/party-role-types", e.reader, nil, &all), 200, "role types again")
	if len(all) != seeded+1 {
		t.Fatalf("role types after adding one: %d, were %d", len(all), seeded)
	}

	// What does not fit the catalog.
	for what, body := range map[string]map[string]any{
		"the same name, whatever the case":  {"name": "GUARANTOR", "parent": domain.RolePersonCategory.String()},
		"the name of a role of the program": {"name": "customer", "parent": domain.RolePartyRoot.String()},
		"the same code":                     {"name": "Avalista", "code": "guarantor", "parent": domain.RolePersonCategory.String()},
	} {
		e.must(e.do("POST", "/api/catalogs/party-role-types", e.admin, body, nil), 422, what)
	}
	for what, body := range map[string]map[string]any{
		"no parent":           {"name": "Orphan"},
		"a parent nobody has": {"name": "Orphan", "parent": fw.NewUUID().String()},
		"no name":             {"name": " ", "parent": domain.RolePersonCategory.String()},
		"a code with spaces":  {"name": "Orphan", "code": "Not A Code", "parent": domain.RolePersonCategory.String()},
	} {
		e.must(e.do("POST", "/api/catalogs/party-role-types", e.admin, body, nil), 400, what)
	}

	// It is assigned like any other, to whom its place in the hierarchy says.
	var person, company papp.PartyDTO
	e.must(e.do("POST", "/api/persons", e.admin, map[string]any{"givenName": "Gus", "firstSurname": "Avalista " + tag,
		"roles": []string{added.ID}}, &person), 201, "a person with the new role")
	if len(person.Roles) != 1 || person.Roles[0].Name != "Guarantor" {
		t.Fatalf("the new role on a person: %+v", person.Roles)
	}
	e.must(e.do("POST", "/api/organizations", e.admin, map[string]any{"legalName": "Avales " + tag, "roles": []string{added.ID}}, &company),
		422, "a role under Person is not for an organization")
	var guarantors fw.Page[papp.PartyDTO]
	e.must(e.do("GET", "/api/parties?role="+added.ID, e.admin, nil, &guarantors), 200, "who plays the new role")
	if guarantors.Total != 1 || guarantors.Items[0].ID != person.ID {
		t.Fatalf("parties playing the new role: %+v", guarantors.Items)
	}

	// Who added it, and what it said, stays in the audit log.
	trail, err := e.mod.Audit.Trail(context.Background(), domain.RoleTypeKind, added.ID)
	if err != nil || len(trail) != 1 || trail[0].Operation != "created" || trail[0].Actor.Name != "ana.admin" || len(trail[0].Changes) != 4 {
		t.Fatalf("audit of the new role type: %+v %v", trail, err)
	}

	// A relationship type is added the same way: which role each side plays.
	var relTypes []papp.RelationshipTypeDTO
	e.must(e.do("GET", "/api/catalogs/party-relationship-types", e.reader, nil, &relTypes), 200, "relationship types")
	for _, rt := range relTypes {
		if !rt.WellKnown {
			t.Fatalf("a relationship type of the program not marked as such: %+v", rt)
		}
	}
	guarantee := map[string]any{"name": "Guarantee", "description": "A guarantor answers for a customer", "code": "guarantee",
		"fromRole": added.ID, "toRole": domain.RoleCustomer.String()}
	e.must(e.do("POST", "/api/catalogs/party-relationship-types", e.reader, guarantee, nil), 403, "without the permission")
	e.must(e.do("POST", "/api/catalogs/party-relationship-types", e.clerk, guarantee, nil), 403, "the catalog is of the whole installation")
	var relType papp.RelationshipTypeDTO
	e.must(e.do("POST", "/api/catalogs/party-relationship-types", e.admin, guarantee, &relType), 201, "a relationship type")
	if relType.Name != "Guarantee" || relType.Code != "guarantee" || relType.FromRole != added.ID || relType.Hierarchical || relType.WellKnown {
		t.Fatalf("added relationship type: %+v", relType)
	}
	for what, body := range map[string]map[string]any{
		"the same name":                  {"name": "guarantee", "fromRole": added.ID, "toRole": domain.RoleCustomer.String()},
		"the name of one of the program": {"name": "EMPLOYMENT", "fromRole": added.ID, "toRole": domain.RoleCustomer.String()},
		"the same code":                  {"name": "Aval", "code": "guarantee", "fromRole": added.ID, "toRole": domain.RoleCustomer.String()},
		"the code of one of the program": {"name": "Aval", "code": "prospect", "fromRole": added.ID, "toRole": domain.RoleCustomer.String()},
	} {
		e.must(e.do("POST", "/api/catalogs/party-relationship-types", e.admin, body, nil), 422, what)
	}
	for what, body := range map[string]map[string]any{
		"no roles":                 {"name": "Aval"},
		"a role nobody has":        {"name": "Aval", "fromRole": added.ID, "toRole": fw.NewUUID().String()},
		"a role that is not an id": {"name": "Aval", "fromRole": "guarantor", "toRole": domain.RoleCustomer.String()},
	} {
		e.must(e.do("POST", "/api/catalogs/party-relationship-types", e.admin, body, nil), 400, what)
	}
	e.must(e.do("GET", "/api/catalogs/party-relationship-types", e.reader, nil, &relTypes), 200, "relationship types again")
	if len(relTypes) != len(domain.WellKnownRelationshipTypes())+1 {
		t.Fatalf("relationship types after adding one: %d", len(relTypes))
	}
	if trail, err = e.mod.Audit.Trail(context.Background(), domain.RelationshipTypeKind, relType.ID); err != nil || len(trail) != 1 {
		t.Fatalf("audit of the new relationship type: %+v %v", trail, err)
	}

	// And relates parties like any other: a side that lacks its role is given it.
	var debtor papp.PartyDTO
	e.must(e.do("POST", "/api/persons", e.admin, map[string]any{"givenName": "Dora", "firstSurname": "Deudora " + tag,
		"roles": []string{domain.RoleCustomer.String()}}, &debtor), 201, "a customer")
	var rel papp.RelationshipDTO
	e.must(e.do("POST", "/api/party-relationships", e.admin, map[string]any{"type": relType.ID, "fromParty": person.ID, "toParty": debtor.ID}, &rel),
		201, "a relationship of the new type")
	if rel.TypeName != "Guarantee" || !rel.Active || rel.FromRole != added.ID {
		t.Fatalf("relationship of the new type: %+v", rel)
	}
	e.must(e.do("POST", "/api/party-relationships", e.admin, map[string]any{"type": relType.ID, "fromParty": debtor.ID, "toParty": person.ID}, &rel),
		201, "the other way round, each side is given the role it lacks")
	e.must(e.do("GET", "/api/parties/"+debtor.ID, e.admin, nil, &debtor), 200, "the customer, now a guarantor too")
	if !slices.ContainsFunc(debtor.Roles, func(r papp.RoleDTO) bool { return r.RoleType == added.ID }) {
		t.Fatalf("the relationship did not give the role of the new type: %+v", debtor.Roles)
	}
}

func endToEnd(t *testing.T, newDirectory func(*testing.T) directory) {
	e := compose(t, newDirectory)
	ctx := context.Background()
	e.scenario("mem")

	var roleTypes []papp.RoleTypeDTO
	e.must(e.do("GET", "/api/catalogs/party-role-types", e.reader, nil, &roleTypes), 200, "role types")
	if len(roleTypes) != len(domain.WellKnownRoleTypes()) {
		t.Fatalf("role types: %d", len(roleTypes))
	}
	e.roleTypes("mem")

	// A migrated SQLite database is swapped in while the service keeps running.
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "parties.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
	m, _ := infrastructure.Migrator(db)
	if err := m.Verify(ctx); !errors.Is(err, application.ErrSchemaOutdated) {
		t.Fatalf("an empty database must be refused: %v", err)
	}
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Verify(ctx); err != nil {
		t.Fatal(err)
	}
	if err := e.sw.Swap(ctx, db); err != nil {
		t.Fatal(err)
	}

	acme, ana := e.scenario("sql")
	var n int
	_ = raw.QueryRow("SELECT COUNT(*) FROM role_types").Scan(&n)
	if n != len(domain.WellKnownRoleTypes()) {
		t.Fatalf("seeded role types: %d", n)
	}
	_ = raw.QueryRow("SELECT COUNT(*) FROM role_types WHERE code IS NULL OR code = ''").Scan(&n)
	if n != 0 {
		t.Fatalf("seeded role types without a code: %d", n)
	}
	_ = raw.QueryRow("SELECT COUNT(*) FROM party_roles WHERE party_id = (SELECT id FROM parties WHERE name LIKE 'Ana sql%')").Scan(&n)
	if n != 4 { // employee, customer by the relationship, bill-to customer, contractor (ended)
		t.Fatalf("ana's roles in SQL: %d", n)
	}

	// Audit trail and Published Language, both committed with the changes.
	trail, err := e.mod.Audit.Trail(ctx, domain.PartyKind, acme.ID)
	if err != nil || len(trail) != 5 || trail[1].Actor.Name != "ana.admin" || len(trail[3].Changes) != 1 || trail[3].Changes[0].Field != "contacts" ||
		len(trail[4].Changes) != 1 || trail[4].Changes[0].Field != "classifications" {
		t.Fatalf("audit: %+v %v", trail, err)
	}
	broker := inprocess.NewBroker()
	store := memory.NewStore("crm")
	crm := messaging.NewConsumer("crm", memory.NewInbox(store), store)
	var seen []string
	messaging.Handle(crm, func(_ context.Context, e contracts.PartyRegisteredV1, _ application.Envelope) error {
		seen = append(seen, e.Name)
		return nil
	})
	var roles int
	messaging.Handle(crm, func(_ context.Context, e contracts.PartyRoleAssignedV1, _ application.Envelope) error {
		if e.PartyID == ana.ID {
			roles++
		}
		return nil
	})
	var trials []contracts.ProspectTrialChangedV1
	messaging.Handle(crm, func(_ context.Context, e contracts.ProspectTrialChangedV1, _ application.Envelope) error {
		trials = append(trials, e)
		return nil
	})
	var shares []string
	messaging.Handle(crm, func(_ context.Context, e contracts.OwnershipShareChangedV1, _ application.Envelope) error {
		shares = append(shares, e.Share)
		return nil
	})
	broker.Subscribe("crm", crm)
	if _, err := e.mod.Relay(broker).RelayOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 15 || roles != 4 {
		t.Fatalf("published: %v, roles %d", seen, roles)
	}
	if len(shares) != 3 || shares[0] != "30.00" || shares[1] != "45.50" || shares[2] != "" {
		t.Fatalf("share events: %v", shares)
	}
	// Granted at registration, extended, granted later and withdrawn.
	if len(trials) != 4 || trials[0].Organization != acme.ID || trials[0].TrialUntil == nil || trials[3].TrialUntil != nil ||
		!trials[1].TrialUntil.After(*trials[0].TrialUntil) || trials[2].Prospect != trials[3].Prospect {
		t.Fatalf("trial events: %+v", trials)
	}

	// Last, so that the party it registers is not among the ones counted above.
	e.roleTypes("sql")
	_ = raw.QueryRow("SELECT COUNT(*) FROM role_types WHERE code = 'guarantor' AND category = 0").Scan(&n)
	if n != 1 {
		t.Fatalf("the role type added to the catalog, in SQL: %d", n)
	}
}

func TestParties_LayersRespectTheArchitecture(t *testing.T) {
	const m = "github.com/jhermoso/karpo-fw-go"
	archtest.AssertOnlyImports(t, "domain", false, archtest.Std, m+"/pkg/domain/...", m+"/contexts/parties/domain")
	archtest.AssertOnlyImports(t, "contracts", false, archtest.Std)
	archtest.AssertOnlyImports(t, "application", false, archtest.Std, m+"/pkg/domain/...", m+"/pkg/application", m+"/pkg/application/...",
		m+"/contexts/parties/domain", m+"/contexts/parties/contracts")
	archtest.AssertTreeDoesNotImport(t, "application", []string{"/pkg/persistence", "/pkg/distribution", "database/sql", "net/http"})
	archtest.AssertTreeDoesNotImport(t, "distribution", []string{"/pkg/persistence", "/contexts/parties/infrastructure"})
}
