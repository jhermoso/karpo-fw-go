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
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jhermoso/karpo-fw-go/contexts/parties"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	"github.com/jhermoso/karpo-fw-go/contexts/parties/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/parties/infrastructure"
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

type env struct {
	t             *testing.T
	srv           *httptest.Server
	sw            *hotswap.Switch
	mod           *parties.Module
	admin, reader string
}

// compose is the composition root of the test host: JWT authentication, the authorization
// resolver over an in-memory security directory, and the Parties module on a hot-swap switch.
func compose(t *testing.T) *env {
	jwt, _ := jwtauth.New(jwtauth.Config{Secret: []byte("parties-test")})
	dir := authorization.NewMemoryDirectory()
	adminID, readerID := fw.NewUUID(), fw.NewUUID()
	dir.Put(adminID, authz.Subject{Active: true, Permissions: []authz.Permission{papp.PermPartyRead, papp.PermPartyCreate,
		papp.PermPartyUpdate, papp.PermRoleAssign, papp.PermRelationshipRead, papp.PermRelationshipCreate, papp.PermRelationshipEnd}})
	dir.Put(readerID, authz.Subject{Active: true, Permissions: []authz.Permission{papp.PermPartyRead, papp.PermRelationshipRead}})
	token := func(sub fw.UUID, name string) string {
		s, _ := jwt.Issue(jwtauth.Claims{Subject: sub.String(), Username: name, PartyID: fw.NewUUID().String(),
			ExpiresAt: fw.Now().Add(time.Hour).Unix()})
		return "Bearer " + s
	}

	sw := hotswap.New(memory.NewStore("memory"))
	mod := parties.Compose(sw, memory.NewIdempotencyStore())
	mux := http.NewServeMux()
	mod.HTTP.RegisterRoutes(mux)
	srv := httptest.NewServer(distribution.Chain(mux, distribution.Correlation(),
		distribution.Authorize(jwt, authorization.NewResolver(dir, authorization.Options{}))))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { _ = sw.Close(context.Background()) })
	return &env{t: t, srv: srv, sw: sw, mod: mod, admin: token(adminID, "ana.admin"), reader: token(readerID, "rita.reader")}
}

func (e *env) do(method, path, auth string, body any, out any) int {
	e.t.Helper()
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
	e.must(e.do("POST", "/api/party-relationships", e.admin, map[string]any{"type": domain.RelCustomer.String(),
		"fromParty": ana.ID, "toParty": acme.ID}, nil), 422, "ana is not a customer yet")

	// Ana becomes a bill-to customer: a Customer by inheritance, so the relationship is allowed
	// and a search for customers finds her.
	e.must(e.do("POST", "/api/parties/"+ana.ID+"/roles", e.admin, map[string]any{"roleType": domain.RoleBillToCustomer.String()}, &ana), 200, "assign role")
	e.must(e.do("POST", "/api/party-relationships", e.admin, map[string]any{"type": domain.RelCustomer.String(),
		"fromParty": ana.ID, "toParty": acme.ID}, nil), 201, "customer relationship")
	var customers fw.Page[papp.PartyDTO]
	e.must(e.do("GET", "/api/parties?role="+domain.RoleCustomer.String()+"&q="+prefix, e.reader, nil, &customers), 200, "search customers")
	if customers.Total != 1 || customers.Items[0].ID != ana.ID {
		e.t.Fatalf("customers: %+v", customers)
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
	return acme, ana
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

func TestParties_EndToEnd_MemoryThenSQLite(t *testing.T) {
	e := compose(t)
	ctx := context.Background()
	e.scenario("mem")

	var roleTypes []papp.RoleTypeDTO
	e.must(e.do("GET", "/api/catalogs/party-role-types", e.reader, nil, &roleTypes), 200, "role types")
	if len(roleTypes) != len(domain.WellKnownRoleTypes()) {
		t.Fatalf("role types: %d", len(roleTypes))
	}

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
	_ = raw.QueryRow("SELECT COUNT(*) FROM party_roles WHERE party_id = (SELECT id FROM parties WHERE name LIKE 'Ana sql%')").Scan(&n)
	if n != 2 {
		t.Fatalf("ana's roles in SQL: %d", n)
	}

	// Audit trail and Published Language, both committed with the changes.
	trail, err := e.mod.Audit.Trail(ctx, domain.PartyKind, acme.ID)
	if err != nil || len(trail) != 4 || trail[1].Actor.Name != "ana.admin" || len(trail[3].Changes) != 1 || trail[3].Changes[0].Field != "classifications" {
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
	broker.Subscribe("crm", crm)
	if _, err := e.mod.Relay(broker).RelayOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 3 || roles != 2 {
		t.Fatalf("published: %v, roles %d", seen, roles)
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
