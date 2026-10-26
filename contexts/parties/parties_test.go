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
	"strings"
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
	// clock es el reloj de dominio del anfitrión de prueba: do lo adelanta antes de cada petición.
	clock *fake.FakeClock
	// admin es un administrador global; los demás trabajan en el ámbito de una organización:
	// reader (sólo lectura), clerk (completo), viewer (acceso de sólo lectura, permisos de
	// escritura), outsider (completo en otra organización), seller (completo, todo menos conceder
	// pruebas).
	admin, reader, clerk, viewer, outsider, seller string
}

var allPerms = append(slices.Clone(sellerPerms), papp.PermRelationshipSetTrial, papp.PermRoleTypeDefine, papp.PermRelationshipTypeDefine, papp.PermRoleAnnul, papp.PermPartyUpdateRelated, papp.PermPartyMerge)

var readerPerms = []authz.Permission{papp.PermPartyRead, papp.PermRelationshipRead}

// sellerPerms son lo que recibe un usuario estándar del catálogo de Security (leer, crear,
// actualizar) más roles y finalizaciones: todo menos conceder pruebas.
var sellerPerms = []authz.Permission{papp.PermPartyRead, papp.PermPartyCreate, papp.PermPartyUpdate, papp.PermRoleAssign,
	papp.PermRelationshipRead, papp.PermRelationshipCreate, papp.PermRelationshipEnd, papp.PermRelationshipUpdate}

// profiles son los usuarios del anfitrión de prueba y sus nombres de usuario.
var profiles = map[string]string{"admin": "ana.admin", "reader": "rita.reader", "clerk": "carl.clerk", "viewer": "vera.viewer",
	"outsider": "otto.outsider", "seller": "sara.seller"}

// directory es la fuente de seguridad del anfitrión de prueba. Los casos de uso de Parties nunca
// la ven: sólo leen el contexto de autorización que el resolutor construye con ella en cada
// petición. El mismo escenario corre sobre el directorio en memoria del framework y sobre el
// contexto Security.
type directory interface {
	authz.Directory
	// register crea el sujeto de un perfil y devuelve su id y su party.
	register(t *testing.T, profile, username string) (subject, party fw.UUID)
	// grant da a un perfil acceso a organizaciones.
	grant(t *testing.T, profile string, level authz.AccessLevel, orgs ...string)
}

// memoryDirectory es el directorio en memoria del framework.
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

// securityDirectory es el directorio real: el contexto delimitado Security, con sus usuarios,
// roles, catálogo de permisos y accesos a organizaciones en su propio almacén.
type securityDirectory struct {
	authz.Directory
	sec   *security.Module
	setup context.Context // un administrador global, para la preparación
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

// grant da a un usuario acceso a organizaciones (el directorio de seguridad se lee en cada
// petición).
func (e *env) grant(user string, level authz.AccessLevel, orgs ...string) {
	e.t.Helper()
	e.dir.grant(e.t, user, level, orgs...)
}

// compose es la raíz de composición del anfitrión de prueba: autenticación JWT, el resolutor de
// autorización sobre el directorio de seguridad y el módulo de Parties sobre un switch intercambiable en caliente.
func compose(t *testing.T, newDirectory func(*testing.T) directory) *env {
	// El reloj del sistema avanza a saltos (de hasta 15,6 ms en Windows), más largos que una
	// petición en memoria, así que dos peticiones podrían compartir un instante o no según la
	// ejecución. El escenario corre sobre un reloj que cada petición adelanta: cada vigencia que
	// termina tiene una duración. Terminar en el instante del inicio se cubre en el dominio
	// (TestEndingAtTheInstantOfTheStart).
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

// scenario ejecuta el flujo de negocio completo sobre el backend actual y devuelve los ids que ha
// creado.
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

	// Las reglas de negocio vuelven como problemas.
	e.must(e.do("POST", "/api/parties/"+acme.ID+"/roles", e.admin, map[string]any{"roleType": domain.RoleEmployee.String()}, nil),
		422, "an organization cannot be an employee")
	e.must(e.do("POST", "/api/persons", e.admin, map[string]any{"givenName": "X", "firstSurname": "Y",
		"roles": []string{domain.RoleLegalCategory.String()}}, nil), 422, "categories cannot be assigned")

	// Employment exige Employee -> Internal Organization.
	var rel papp.RelationshipDTO
	body := map[string]any{"type": domain.RelEmployment.String(), "fromParty": ana.ID, "toParty": acme.ID, "remark": "hired"}
	e.must(e.do("POST", "/api/party-relationships", e.admin, body, &rel), 201, "establish employment")
	if rel.TypeName != "Employment" || !rel.Active || rel.FromRole != domain.RoleEmployee.String() {
		e.t.Fatalf("relationship: %+v", rel)
	}
	e.must(e.do("POST", "/api/party-relationships", e.admin, body, nil), 422, "duplicate relationship")
	// Una relación da a cada lado el rol que le falta (D-49), pero nunca hace organización interna
	// a nadie, ni da un rol a quien no puede desempeñarlo.
	e.must(e.do("POST", "/api/party-relationships", e.admin, map[string]any{"type": domain.RelEmployment.String(),
		"fromParty": acme.ID, "toParty": ana.ID}, nil), 422, "an organization is not made an employee")
	var boss papp.PartyDTO
	e.must(e.do("POST", "/api/persons", e.admin, map[string]any{"givenName": "Jefa", "firstSurname": prefix}, &boss), 201, "somebody who is no company")
	e.must(e.do("POST", "/api/party-relationships", e.admin, map[string]any{"type": domain.RelEmployment.String(),
		"fromParty": ana.ID, "toParty": boss.ID}, nil), 422, "nobody is made an internal organization by a relationship")
	e.must(e.do("GET", "/api/parties/"+boss.ID, e.admin, nil, &boss), 200, "the one who is no company")
	if len(boss.Roles) != 0 {
		e.t.Fatalf("a refused relationship left a role: %+v", boss.Roles)
	}
	e.must(e.do("POST", "/api/party-relationships", e.admin, map[string]any{"type": domain.RelCustomer.String(),
		"fromParty": ana.ID, "toParty": acme.ID}, nil), 201, "the customer relationship makes ana a customer")
	e.must(e.do("GET", "/api/parties/"+ana.ID, e.reader, nil, &ana), 200, "ana after the relationship")
	if !slices.ContainsFunc(ana.Roles, func(r papp.RoleDTO) bool { return r.RoleType == domain.RoleCustomer.String() }) {
		e.t.Fatalf("ana was not made a customer: %+v", ana.Roles)
	}

	// Ana pasa a ser también cliente de facturación: un Customer por herencia; una búsqueda de
	// clientes la encuentra una vez.
	e.must(e.do("POST", "/api/parties/"+ana.ID+"/roles", e.admin, map[string]any{"roleType": domain.RoleBillToCustomer.String()}, &ana), 200, "assign role")
	var customers fw.Page[papp.PartyDTO]
	e.must(e.do("GET", "/api/parties?role="+domain.RoleCustomer.String()+"&q="+prefix, e.reader, nil, &customers), 200, "search customers")
	if customers.Total != 1 || customers.Items[0].ID != ana.ID {
		e.t.Fatalf("customers: %+v", customers)
	}

	// Un rol que ha terminado no cuenta, salvo que la búsqueda pida también los terminados (D-48).
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

	// Un rol dado por error se retira (D-8): se quita cuando nada depende de él, y se termina y se
	// marca cuando ya se ha hecho una relación en él.
	roleOf := func(p papp.PartyDTO, rt domain.RoleTypeID) (papp.RoleDTO, bool) {
		i := slices.IndexFunc(p.Roles, func(r papp.RoleDTO) bool { return r.RoleType == rt.String() })
		if i < 0 {
			return papp.RoleDTO{}, false
		}
		return p.Roles[i], true
	}
	e.must(e.do("POST", "/api/parties/"+ana.ID+"/roles", e.admin, map[string]any{"roleType": domain.RoleFamilyMember.String()}, &ana), 200, "a role by mistake")
	mistake, _ := roleOf(ana, domain.RoleFamilyMember)
	e.must(e.do("POST", "/api/parties/"+ana.ID+"/roles/"+mistake.ID+"/annul", e.seller, nil, nil), 403, "annulling has its own permission: who assigns roles does not have it")
	e.must(e.do("POST", "/api/parties/"+ana.ID+"/roles/"+mistake.ID+"/annul", e.admin, nil, &ana), 200, "annul a role nothing depends on")
	if _, still := roleOf(ana, domain.RoleFamilyMember); still {
		e.t.Fatalf("the role was not removed: %+v", ana.Roles)
	}
	e.must(e.do("POST", "/api/parties/"+ana.ID+"/roles/"+mistake.ID+"/annul", e.admin, nil, nil), 404, "it is gone")
	employee, _ := roleOf(ana, domain.RoleEmployee)
	e.must(e.do("POST", "/api/parties/"+ana.ID+"/roles/"+employee.ID+"/annul", e.admin, nil, &ana), 200, "annul a role with a relationship")
	if employee, _ = roleOf(ana, domain.RoleEmployee); !employee.Annulled || employee.Active || employee.Until == nil {
		e.t.Fatalf("the role with a relationship: %+v", employee)
	}
	var annulled fw.Page[papp.PartyDTO]
	e.must(e.do("GET", "/api/parties?role="+domain.RoleEmployee.String()+"&includeEndedRoles=true&q="+prefix, e.reader, nil, &annulled), 200, "employees ever")
	if annulled.Total != 0 {
		e.t.Fatalf("an annulled role counted: %+v", annulled)
	}
	// Ni siquiera por los días que duró: el rol puede volver a darse desde antes.
	e.must(e.do("POST", "/api/parties/"+ana.ID+"/roles", e.admin, map[string]any{"roleType": domain.RoleEmployee.String(),
		"from": employee.From}, &ana), 200, "the role again, from when the annulled one started")

	// Un duplicado se encuentra y se lee, y no interviene en nada más (D-64); terminar la relación
	// Duplicate Of lo deshace.
	var dup, survivor papp.PartyDTO
	e.must(e.do("POST", "/api/persons", e.admin, map[string]any{"givenName": "Duplicada", "firstSurname": prefix}, &dup), 201, "a duplicate")
	e.must(e.do("POST", "/api/persons", e.admin, map[string]any{"givenName": "Buena", "firstSurname": prefix}, &survivor), 201, "the survivor")
	var dupRel papp.RelationshipDTO
	e.must(e.do("POST", "/api/party-relationships", e.admin, map[string]any{"type": domain.RelDuplicateOf.String(),
		"fromParty": dup.ID, "toParty": survivor.ID}, &dupRel), 201, "mark the duplicate")
	var found fw.Page[papp.PartyDTO]
	e.must(e.do("GET", "/api/parties?q=Duplicada", e.admin, nil, &found), 200, "search by default")
	if slices.ContainsFunc(found.Items, func(p papp.PartyDTO) bool { return p.ID == dup.ID }) {
		e.t.Fatalf("a duplicate in a search by default: %+v", found)
	}
	e.must(e.do("GET", "/api/parties?q=Duplicada&includeMergedParties=true", e.admin, nil, &found), 200, "search with the duplicates")
	if !slices.ContainsFunc(found.Items, func(p papp.PartyDTO) bool { return p.ID == dup.ID && p.Duplicate }) {
		e.t.Fatalf("the duplicate was not found on request: %+v", found)
	}
	e.must(e.do("GET", "/api/parties/"+dup.ID, e.admin, nil, nil), 200, "a duplicate is read")
	e.must(e.do("PUT", "/api/parties/"+dup.ID+"/name", e.admin, map[string]any{"givenName": "Otra", "firstSurname": prefix}, nil), 422, "nor renamed")
	e.must(e.do("POST", "/api/party-relationships", e.admin, map[string]any{"type": domain.RelCustomer.String(),
		"fromParty": dup.ID, "toParty": acme.ID}, nil), 422, "nor related")
	e.must(e.do("POST", "/api/party-relationships", e.admin, map[string]any{"type": domain.RelDuplicateOf.String(),
		"fromParty": survivor.ID, "toParty": dup.ID}, nil), 422, "nor the survivor of anybody")
	e.must(e.do("POST", "/api/party-relationships/"+dupRel.ID+"/terminate", e.admin, nil, nil), 200, "undo the duplicate")
	e.must(e.do("PUT", "/api/parties/"+dup.ID+"/name", e.admin, map[string]any{"givenName": "Otra", "firstSurname": prefix}, nil), 200, "operable again")

	// Una party con relaciones sin terminar no se desactiva (D-4); una sin ellas, sí.
	e.must(e.do("PUT", "/api/parties/"+ana.ID+"/active", e.admin, map[string]any{"active": false}, nil), 422, "ana works for acme and buys from it")
	e.must(e.do("PUT", "/api/parties/"+acme.ID+"/active", e.admin, map[string]any{"active": false}, nil), 422, "acme has an employee and a customer")
	var loner papp.PartyDTO
	e.must(e.do("POST", "/api/persons", e.admin, map[string]any{"givenName": "Sola", "firstSurname": prefix}, &loner), 201, "somebody unrelated")
	e.must(e.do("PUT", "/api/parties/"+loner.ID+"/active", e.admin, map[string]any{"active": false}, &loner), 200, "deactivate who nobody depends on")
	if loner.Active {
		e.t.Fatalf("still active: %+v", loner)
	}

	// El reader puede leer pero no escribir.
	e.must(e.do("PUT", "/api/parties/"+acme.ID+"/name", e.reader, map[string]any{"legalName": "Nope"}, nil), 403, "reader cannot rename")
	e.must(e.do("GET", "/api/parties/"+acme.ID, "", nil, nil), 401, "anonymous")

	// Renombrar, terminar, listar.
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

	// Directorio (Open Host Service).
	var refs []contracts.PartyRef
	e.must(e.do("POST", "/api/parties/directory/resolve", e.reader, map[string]any{"partyIds": []string{ana.ID, acme.ID, fw.NewUUID().String(), "junk"}}, &refs), 200, "resolve")
	if len(refs) != 2 {
		e.t.Fatalf("directory: %+v", refs)
	}
	e.phase2(acme, ana)
	e.phase3(acme, ana)
	e.prospects(acme, ana)
	e.selfEmployed(acme)
	e.dataQuality(prefix)
	return acme, ana
}

// selfEmployed cubre al autónomo (D-85): una persona con el rol Self Employed, identificada con su
// documento de identidad y con el perfil de actividad en su rol.
func (e *env) selfEmployed(acme papp.PartyDTO) {
	e.t.Helper()
	var eva papp.PartyDTO
	e.must(e.do("POST", "/api/persons", e.clerk, map[string]any{"givenName": "Eva", "firstSurname": "Autónoma " + acme.ID[:8],
		"roles":       []string{domain.RoleSelfEmployed.String()},
		"affiliation": map[string]any{"organization": acme.ID, "relationshipType": domain.RelCustomer.String()}}, &eva), 201, "a self-employed customer")
	ids := "/api/parties/" + eva.ID + "/identifications"
	// Acme ya tiene el CIF B12345674 (phase2): la clase se mira antes que el documento cogido.
	e.mustProblem(e.problem("POST", ids, e.clerk, map[string]any{"documentType": domain.DocTaxID.String(), "country": "ES", "number": "B12345674"}),
		422, "parties.document_not_for_kind", "a person is not identified by a CIF, even one somebody else holds")
	e.mustProblem(e.problem("POST", ids, e.clerk, map[string]any{"documentType": domain.DocTaxID.String(), "country": "es", "number": "A58818501"}),
		422, "parties.document_not_for_kind", "nor with the country in lower case")
	e.must(e.do("POST", ids, e.clerk, map[string]any{"documentType": domain.DocNationalID.String(), "country": "ES", "number": "00000001R"}, &eva),
		200, "her NIF is her DNI")
	e.mustProblem(e.problem("POST", "/api/parties/"+acme.ID+"/identifications", e.admin, map[string]any{"documentType": domain.DocNationalID.String(),
		"country": "ES", "number": "00000002W"}), 422, "parties.document_not_for_kind", "an organization is not identified by a DNI")

	roleID := func(p papp.PartyDTO, rt domain.RoleTypeID) string {
		return p.Roles[slices.IndexFunc(p.Roles, func(r papp.RoleDTO) bool { return r.RoleType == rt.String() })].ID
	}
	self := "/api/parties/" + eva.ID + "/roles/" + roleID(eva, domain.RoleSelfEmployed) + "/self-employed"
	profile := map[string]any{"tradeName": " Fontanería   Eva ", "activityCode": "504.1", "registeredOn": "2020-03-01"}
	e.must(e.do("PUT", self, e.reader, profile, nil), 403, "a reader changes no profile")
	e.must(e.do("PUT", self, e.viewer, profile, nil), 403, "nor who has the permission and sees her, but only to read")
	e.must(e.do("PUT", self, e.outsider, profile, nil), 404, "nor who does not see her")
	e.must(e.do("PUT", self, e.clerk, map[string]any{"tradeName": strings.Repeat("x", 121)}, nil), 400, "a trade name of 121 characters")
	e.must(e.do("PUT", self, e.clerk, map[string]any{"registeredOn": "2999-01-01"}, nil), 400, "a registration in the future")
	e.must(e.do("PUT", self, e.clerk, map[string]any{"registeredOn": "1 March"}, nil), 400, "a date that is not one")
	e.must(e.do("PUT", self, e.clerk, map[string]any{"activityCode": "12345678901"}, nil), 400, "an activity code too long")
	e.must(e.do("PUT", self, e.clerk, map[string]any{"nif": "x"}, nil), 400, "the NIF is the identity document, not the profile")
	e.must(e.do("PUT", "/api/parties/"+eva.ID+"/roles/"+roleID(eva, domain.RoleCustomer)+"/self-employed", e.clerk, profile, nil),
		422, "a customer role has no self-employed profile")
	e.must(e.do("PUT", self, e.clerk, profile, &eva), 200, "the profile")
	e.must(e.do("GET", "/api/parties/"+eva.ID, e.reader, nil, &eva), 200, "eva, read back")
	got := eva.Roles[slices.IndexFunc(eva.Roles, func(r papp.RoleDTO) bool { return r.RoleType == domain.RoleSelfEmployed.String() })].SelfEmployed
	if got == nil || *got != (papp.SelfEmployedDTO{TradeName: "Fontanería Eva", ActivityCode: "504.1", RegisteredOn: "2020-03-01"}) {
		e.t.Fatalf("self-employed profile: %+v", got)
	}
	if c := eva.Roles[slices.IndexFunc(eva.Roles, func(r papp.RoleDTO) bool { return r.RoleType == domain.RoleCustomer.String() })]; c.SelfEmployed != nil {
		e.t.Fatalf("the customer role shows a profile: %+v", c)
	}
	eva = papp.PartyDTO{} // decodificar deja lo que la respuesta no trae
	e.must(e.do("PUT", self, e.clerk, map[string]any{}, &eva), 200, "an empty profile")
	if r := eva.Roles[slices.IndexFunc(eva.Roles, func(r papp.RoleDTO) bool { return r.RoleType == domain.RoleSelfEmployed.String() })]; r.SelfEmployed != nil {
		e.t.Fatalf("an empty profile is not shown: %+v", r)
	}

	// Un duplicado no cambia su perfil (D-64); deshecho el duplicado, sí.
	var other papp.PartyDTO
	e.must(e.do("POST", "/api/persons", e.admin, map[string]any{"givenName": "Eva", "firstSurname": "Buena " + acme.ID[:8]}, &other), 201, "the survivor")
	var dupRel papp.RelationshipDTO
	e.must(e.do("POST", "/api/party-relationships", e.admin, map[string]any{"type": domain.RelDuplicateOf.String(),
		"fromParty": eva.ID, "toParty": other.ID}, &dupRel), 201, "eva is a duplicate")
	e.mustProblem(e.problem("PUT", self, e.admin, profile), 422, "parties.duplicate", "a duplicate changes no profile")
	e.must(e.do("POST", "/api/party-relationships/"+dupRel.ID+"/terminate", e.admin, nil, nil), 200, "not a duplicate any more")

	// El perfil de un rol terminado ya no cambia.
	e.must(e.do("POST", "/api/parties/"+eva.ID+"/roles/"+roleID(eva, domain.RoleSelfEmployed)+"/end", e.clerk, map[string]any{}, nil), 200,
		"no longer self-employed")
	e.mustProblem(e.problem("PUT", self, e.clerk, profile), 422, "parties.role_ended", "the profile of an ended role")
}

// problemResult es el estado y el código de problema de una respuesta.
type problemResult struct {
	status int
	code   string
}

// problem es do para una respuesta de error: devuelve el estado y el código del problema.
func (e *env) problem(method, path, auth string, body any) problemResult {
	e.t.Helper()
	var buf bytes.Buffer
	_ = json.NewEncoder(&buf).Encode(body)
	req, _ := http.NewRequest(method, e.srv.URL+path, &buf)
	req.Header.Set("Authorization", auth)
	e.clock.Advance(time.Millisecond)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	var p struct {
		Code string `json:"code"`
	}
	_ = json.NewDecoder(res.Body).Decode(&p)
	return problemResult{res.StatusCode, p.Code}
}

func (e *env) mustProblem(got problemResult, want int, wantCode, what string) {
	e.t.Helper()
	if got.status != want || got.code != wantCode {
		e.t.Fatalf("%s: %d %q, want %d %q", what, got.status, got.code, want, wantCode)
	}
}

// prospects cubre los detalles que lleva una relación por su tipo (docs/PARTIES-UDM.md): la
// relación de prospect con una organización interna y su prueba.
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

	// Un prospect se da de alta dentro del ámbito con su prueba: rol, relación, afiliación y
	// prueba en una sola petición.
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

	// Ampliar la prueba necesita el permiso y un ámbito con escritura.
	longer := until.Add(15 * 24 * time.Hour)
	var rel papp.RelationshipDTO
	e.must(e.do("PUT", trial, e.reader, map[string]any{"trialUntil": longer}, nil), 403, "the reader lacks the permission")

	// Conceder una prueba tiene su propio permiso: un seller crea y actualiza todo lo demás en la
	// organización, pero no regala tiempo, ni después ni con la relación.
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

	// Sólo las relaciones de prospect tienen prueba; los demás tipos siguen funcionando sin
	// detalles.
	var anas []papp.RelationshipDTO
	e.must(e.do("GET", "/api/parties/"+ana.ID+"/relationships?active=true", e.reader, nil, &anas), 200, "ana's relationships")
	if len(anas) != 1 || anas[0].Prospect != nil {
		e.t.Fatalf("a customer relationship carries no prospect details: %+v", anas)
	}
	e.must(e.do("PUT", "/api/party-relationships/"+anas[0].ID+"/trial", e.admin, map[string]any{"trialUntil": longer}, nil), 422, "a customer relationship has no trial")

	// Establecer la relación después, con o sin prueba.
	var bea papp.PartyDTO
	e.must(e.do("POST", "/api/persons", e.admin, map[string]any{"givenName": "Bea", "firstSurname": "Prueba " + acme.ID[:8],
		"roles": []string{domain.RoleProspect.String()}}, &bea), 201, "register bea")
	body := map[string]any{"type": domain.RelProspect.String(), "fromParty": bea.ID, "toParty": acme.ID}
	rel = papp.RelationshipDTO{} // decodificar sobre un valor usado conservaría los campos que la respuesta omite
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

	// Convertir el prospect en cliente: empieza la relación de cliente y termina la relación de
	// prospect, y con ella la prueba. La party sigue en el ámbito de la organización.
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

	// Los datos personales de una persona se pueden corregir después del alta.
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

	// Ownership: la participación de un accionista es un detalle de la relación de propiedad.
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

	// Código de promoción: el código de un colaborador es un detalle de su relación con la
	// empresa.
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

	// La marca de un documento de identidad rechazado: quien la tiene se encuentra por ella, y un
	// documento que pasa sus comprobaciones la resuelve.
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
	marked = papp.PartyDTO{} // decodificar deja lo que la respuesta no trae
	e.must(e.do("POST", "/api/parties/"+agency.ID+"/identifications", e.admin, map[string]any{"documentType": domain.DocOther.String(),
		"country": "ES", "number": "B12345678"}, &marked), 200, "the wrong number, kept as other document")
	if marked.DocumentIssue == nil {
		e.t.Fatal("an other document, which nobody checks, answers no mark")
	}
	// Una organización no lleva pasaporte (D-85): se rechaza y la marca sigue.
	e.must(e.do("POST", "/api/parties/"+agency.ID+"/identifications", e.admin, map[string]any{"documentType": domain.DocPassport.String(),
		"country": "ES", "number": "PAB765432", "issuedOn": "2021-02-03", "expiresOn": "2031-02-02", "issuingAuthority": "Policía Nacional"}, nil), 422,
		"an organization carries no passport")
	marked = papp.PartyDTO{}
	e.must(e.do("GET", "/api/parties/"+agency.ID, e.admin, nil, &marked), 200, "the agency")
	if marked.DocumentIssue == nil {
		e.t.Fatal("a refused passport answers no mark")
	}
	marked = papp.PartyDTO{} // decodificar deja lo que la respuesta no trae
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

	// Número de cliente: cada organización numera a sus propios clientes, así que es un detalle de
	// la relación de cliente. Es de un cliente para siempre.
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

	// Un número al que se renuncia queda libre otra vez: sólo está ocupado un número que lleva
	// alguna relación.
	e.must(e.do("PUT", "/api/party-relationships/"+buysToo.ID+"/customer-number", e.clerk, map[string]any{"customerNumber": "C-4300003"}, nil), 200,
		"another number")
	e.must(e.do("PUT", number, e.clerk, map[string]any{"customerNumber": "C-4300002"}, nil), 200, "the number the other gave up")
	e.must(e.do("PUT", number, e.clerk, map[string]any{"customerNumber": "C-4300001"}, nil), 200, "and its own again")

	// El número se queda con el cliente cuando termina la relación: no se le da a otro, y el
	// cliente lo recupera si vuelve.
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

	// Quién cambia a un tercero (D-65, D-66): la organización que lo dio de alta; una que sólo se
	// relaciona con él, con PermPartyUpdateRelated (el clerk lo tiene, el seller no); y una
	// organización interna de otros, nadie de Acme.
	var third, mine papp.PartyDTO
	e.must(e.do("POST", "/api/persons", e.admin, map[string]any{"givenName": "Tercera", "firstSurname": acme.ID[:8]}, &third), 201, "a party from elsewhere")
	e.must(e.do("POST", "/api/party-relationships", e.admin, map[string]any{"type": domain.RelCustomer.String(),
		"fromParty": third.ID, "toParty": acme.ID}, nil), 201, "which becomes a customer of acme")
	rename := func(who, id string) int {
		return e.do("PUT", "/api/parties/"+id+"/name", who, map[string]any{"givenName": "Tercera", "firstSurname": acme.ID[:8] + " bis"}, nil)
	}
	e.must(rename(e.seller, third.ID), 403, "a related party is only seen by who lacks the permission")
	e.must(rename(e.clerk, third.ID), 200, "and changed by who has it")
	e.must(e.do("POST", "/api/persons", e.seller, map[string]any{"givenName": "Mía", "firstSurname": acme.ID[:8],
		"affiliation": map[string]any{"organization": acme.ID, "relationshipType": domain.RelCustomer.String()}}, &mine), 201, "registered from acme")
	e.must(rename(e.seller, mine.ID), 200, "what acme registered, acme changes")
	// Salvo que se diera de alta antes de que existiera la regla (D-77).
	since := papp.RegistrationRuleSince
	papp.RegistrationRuleSince = time.Now().Add(time.Hour)
	e.must(rename(e.seller, mine.ID), 403, "a party registered before the rule is only related")
	papp.RegistrationRuleSince = since
	// Sólo el administrador global hace de una party una organización interna (D-78).
	e.must(e.do("POST", "/api/parties/"+mine.ID+"/roles", e.clerk, map[string]any{"roleType": domain.RoleInternalOrganization.String()}, nil), 403,
		"a clerk does not make a company of the group")
	e.must(e.do("POST", "/api/organizations", e.clerk, map[string]any{"legalName": "Filial " + acme.ID[:8], "roles": []string{domain.RoleInternalOrganization.String()},
		"affiliation": map[string]any{"organization": acme.ID, "relationshipType": domain.RelCustomer.String()}}, nil), 403, "nor registers one")
	var relative papp.PartyDTO
	e.must(e.do("POST", "/api/persons", e.admin, map[string]any{"givenName": "Pariente", "firstSurname": acme.ID[:8],
		"roles": []string{domain.RoleFamilyMember.String()}}, &relative), 201, "a relative")
	e.must(e.do("POST", "/api/parties/"+mine.ID+"/roles", e.admin, map[string]any{"roleType": domain.RoleFamilyMember.String()}, nil), 200, "mía in a family")
	e.must(e.do("POST", "/api/party-relationships", e.admin, map[string]any{"type": domain.RelFamily.String(),
		"fromParty": relative.ID, "toParty": mine.ID}, nil), 201, "related as family")
	e.must(e.do("GET", "/api/parties/"+relative.ID, e.clerk, nil, nil), 404, "a family relationship shows nobody (D-67)")
	var other papp.PartyDTO
	e.must(e.do("POST", "/api/organizations", e.admin, map[string]any{"legalName": "Otra Empresa " + acme.ID[:8],
		"roles": []string{domain.RoleInternalOrganization.String()}}, &other), 201, "an internal organization of others")
	e.must(e.do("POST", "/api/party-relationships", e.admin, map[string]any{"type": domain.RelSupplier.String(),
		"fromParty": acme.ID, "toParty": other.ID}, nil), 201, "acme buys from it")
	e.must(e.do("GET", "/api/parties/"+other.ID, e.clerk, nil, nil), 200, "acme sees it")
	e.must(e.do("PUT", "/api/parties/"+other.ID+"/name", e.clerk, map[string]any{"legalName": "Nope"}, nil), 403,
		"but nobody of acme changes an internal organization of others, permission or not")

	// Marcar un duplicado es fusionarlo: tiene su propio permiso (D-95), que el usuario estándar
	// no tiene; pide poder cambiar las dos parties, la que cede y la superviviente (D-96); nadie
	// más que el administrador global bloquea una empresa, y quien la marcó puede deshacerlo.
	e.must(e.do("POST", "/api/party-relationships", e.seller, map[string]any{"type": domain.RelDuplicateOf.String(),
		"fromParty": mine.ID, "toParty": third.ID}, nil), 403, "merging has its own permission: the seller cannot, though it changes both")
	e.must(e.do("POST", "/api/party-relationships", e.clerk, map[string]any{"type": domain.RelDuplicateOf.String(),
		"fromParty": acme.ID, "toParty": other.ID}, nil), 422, "an internal organization does not cede in a merge (D-99)")
	e.must(e.do("POST", "/api/party-relationships", e.admin, map[string]any{"type": domain.RelDuplicateOf.String(),
		"fromParty": acme.ID, "toParty": other.ID}, nil), 422, "not even for the global administrator")
	e.must(e.do("POST", "/api/party-relationships", e.clerk, map[string]any{"type": domain.RelDuplicateOf.String(),
		"fromParty": mine.ID, "toParty": other.ID}, nil), 403, "nor into a survivor the clerk cannot change")
	var dupMark papp.RelationshipDTO
	e.must(e.do("POST", "/api/party-relationships", e.clerk, map[string]any{"type": domain.RelDuplicateOf.String(),
		"fromParty": mine.ID, "toParty": third.ID}, &dupMark), 201, "the clerk merges what acme registered into a party it can change")
	e.must(e.do("POST", "/api/party-relationships/"+dupMark.ID+"/terminate", e.seller, nil, nil), 403, "undoing a merge needs the permission too")
	e.must(e.do("POST", "/api/party-relationships/"+dupMark.ID+"/terminate", e.clerk, nil, nil), 200, "and the clerk undoes it")
	e.must(e.do("POST", "/api/persons", e.seller, map[string]any{"givenName": "Nadie", "firstSurname": acme.ID[:8],
		"affiliation": map[string]any{"organization": acme.ID, "relationshipType": domain.RelFamily.String()}}, nil), 400,
		"a family relationship affiliates nobody")
	e.merges(acme)
}

// merges cubre lo que pasa al fusionar con las relaciones de negocio de la ficha que cede (D-93):
// las que la superviviente no tiene con esa empresa pasan a ella con su número de cliente; las que
// ya tiene se quedan en la que cede, para revisarlas; deshacer la fusión devuelve las que pasaron.
func (e *env) merges(acme papp.PartyDTO) {
	e.t.Helper()
	tag := acme.ID[:8]
	var bilbao, cede, survivor papp.PartyDTO
	e.must(e.do("POST", "/api/organizations", e.admin, map[string]any{"legalName": "Bilbao " + tag,
		"roles": []string{domain.RoleInternalOrganization.String()}}, &bilbao), 201, "another company of the group")
	e.must(e.do("POST", "/api/persons", e.seller, map[string]any{"givenName": "Cede", "firstSurname": tag,
		"affiliation": map[string]any{"organization": acme.ID, "relationshipType": domain.RelCustomer.String()}}, &cede), 201, "acme registers a customer")
	relationships := func(p papp.PartyDTO) []papp.RelationshipDTO {
		var rs []papp.RelationshipDTO
		e.must(e.do("GET", "/api/parties/"+p.ID+"/relationships", e.admin, nil, &rs), 200, "relationships")
		return rs
	}
	find := func(p papp.PartyDTO, typ domain.RelationshipTypeID, org string, active bool) (papp.RelationshipDTO, bool) {
		for _, r := range relationships(p) {
			if r.Type == typ.String() && (r.FromParty == org || r.ToParty == org) && r.Active == active {
				return r, true
			}
		}
		return papp.RelationshipDTO{}, false
	}
	buys, _ := find(cede, domain.RelCustomer, acme.ID, true)
	e.must(e.do("PUT", "/api/party-relationships/"+buys.ID+"/customer-number", e.admin, map[string]any{"customerNumber": "M-77"}, nil), 200, "its number")
	e.must(e.do("POST", "/api/party-relationships", e.admin, map[string]any{"type": domain.RelSupplier.String(),
		"fromParty": acme.ID, "toParty": cede.ID}, nil), 201, "acme also buys from it")
	e.must(e.do("POST", "/api/party-relationships", e.admin, map[string]any{"type": domain.RelCustomer.String(),
		"fromParty": cede.ID, "toParty": bilbao.ID}, nil), 201, "and it buys from bilbao")
	e.must(e.do("POST", "/api/persons", e.admin, map[string]any{"givenName": "Superviviente", "firstSurname": tag}, &survivor), 201, "the survivor")
	e.must(e.do("POST", "/api/party-relationships", e.admin, map[string]any{"type": domain.RelCustomer.String(),
		"fromParty": survivor.ID, "toParty": bilbao.ID}, nil), 201, "which already buys from bilbao")
	e.must(e.do("GET", "/api/parties/"+survivor.ID, e.seller, nil, nil), 404, "acme does not see the survivor yet")

	var mark papp.RelationshipDTO
	e.must(e.do("POST", "/api/party-relationships", e.admin, map[string]any{"type": domain.RelDuplicateOf.String(),
		"fromParty": cede.ID, "toParty": survivor.ID}, &mark), 201, "merge")
	if m := mark.Merge; m == nil || len(m.Transferred) != 2 || len(m.Kept) != 1 || m.Kept[0].Counterparty != bilbao.ID {
		e.t.Fatalf("what the merge did: %+v", mark.Merge)
	}
	moved, ok := find(survivor, domain.RelCustomer, acme.ID, true)
	if !ok || moved.Customer == nil || moved.Customer.Number != "M-77" || moved.TransferredFrom != buys.ID || !moved.Since.Equal(mark.Since) {
		e.t.Fatalf("the customer relationship of the survivor: %+v", moved)
	}
	if _, ok := find(survivor, domain.RelSupplier, acme.ID, true); !ok {
		e.t.Fatalf("the supplier relationship did not pass: %+v", relationships(survivor))
	}
	if _, ok := find(cede, domain.RelCustomer, acme.ID, true); ok {
		e.t.Fatalf("the merged party still buys from acme: %+v", relationships(cede))
	}
	if _, ok := find(cede, domain.RelCustomer, bilbao.ID, true); !ok {
		e.t.Fatalf("what the survivor already had stays with the merged party: %+v", relationships(cede))
	}
	e.must(e.do("GET", "/api/parties/"+survivor.ID, e.seller, nil, nil), 200, "acme sees the survivor now")
	e.must(e.do("GET", "/api/parties/"+cede.ID, e.seller, nil, nil), 200, "and still finds the merged party")

	e.must(e.do("POST", "/api/party-relationships/"+mark.ID+"/terminate", e.admin, nil, nil), 200, "undo the merge")
	if _, ok := find(survivor, domain.RelCustomer, acme.ID, true); ok {
		e.t.Fatalf("the survivor kept the customer relationship: %+v", relationships(survivor))
	}
	back, ok := find(cede, domain.RelCustomer, acme.ID, true)
	if !ok || back.Customer == nil || back.Customer.Number != "M-77" || back.TransferredFrom != moved.ID {
		e.t.Fatalf("the customer relationship that came back: %+v", back)
	}
	if _, ok := find(cede, domain.RelSupplier, acme.ID, true); !ok {
		e.t.Fatalf("the supplier relationship did not come back: %+v", relationships(cede))
	}
	e.must(e.do("GET", "/api/parties/"+survivor.ID, e.seller, nil, nil), 404, "acme no longer sees the survivor")
	e.must(e.do("PUT", "/api/parties/"+cede.ID+"/name", e.seller, map[string]any{"givenName": "Cedida", "firstSurname": tag}, nil), 200,
		"what acme registered, acme still changes after the merge is undone")

	// Una fusión se deshace una sola vez: terminar otra vez la primera no desbloquea a la que cede
	// cuando hay una segunda vigente.
	merge := func(from, to string) papp.RelationshipDTO {
		var m papp.RelationshipDTO
		e.must(e.do("POST", "/api/party-relationships", e.admin, map[string]any{"type": domain.RelDuplicateOf.String(),
			"fromParty": from, "toParty": to}, &m), 201, "merge")
		return m
	}
	undo := func(m papp.RelationshipDTO) papp.RelationshipDTO {
		var out papp.RelationshipDTO
		e.must(e.do("POST", "/api/party-relationships/"+m.ID+"/terminate", e.admin, nil, &out), 200, "undo the merge")
		return out
	}
	e.must(e.do("POST", "/api/party-relationships/"+mark.ID+"/terminate", e.admin, nil, nil), 422, "a merge is undone once")
	second := merge(cede.ID, survivor.ID)
	e.must(e.do("POST", "/api/party-relationships/"+mark.ID+"/terminate", e.admin, nil, nil), 422, "nor the first one again while the second holds")
	e.must(e.do("PUT", "/api/parties/"+cede.ID+"/name", e.admin, map[string]any{"givenName": "Otra", "firstSurname": tag}, nil), 422, "still blocked")
	// Ni se fusiona ni se deshace con una fecha pasada.
	e.must(e.do("POST", "/api/party-relationships/"+second.ID+"/terminate", e.admin, map[string]any{"at": time.Now().Add(-time.Hour)}, nil), 422,
		"undoing in the past")
	e.must(e.do("POST", "/api/party-relationships/"+second.ID+"/terminate", e.admin, map[string]any{"at": time.Now().Add(time.Hour)}, nil), 422,
		"nor in the future")
	// No se encadenan fusiones (D-97): la superviviente de una vigente no se fusiona en otra.
	var third papp.PartyDTO
	e.must(e.do("POST", "/api/persons", e.admin, map[string]any{"givenName": "Tercera", "firstSurname": tag}, &third), 201, "a third party")
	e.must(e.do("POST", "/api/party-relationships", e.admin, map[string]any{"type": domain.RelDuplicateOf.String(),
		"fromParty": survivor.ID, "toParty": third.ID}, nil), 422, "the survivor of a merge is not merged into another")
	undo(second)
	e.must(e.do("POST", "/api/party-relationships", e.admin, map[string]any{"type": domain.RelDuplicateOf.String(),
		"fromParty": cede.ID, "toParty": survivor.ID, "since": time.Now().Add(-time.Hour)}, nil), 422, "merging in the past")
	e.must(e.do("POST", "/api/party-relationships", e.admin, map[string]any{"type": domain.RelDuplicateOf.String(),
		"fromParty": cede.ID, "toParty": survivor.ID, "since": time.Now().Add(time.Hour)}, nil), 422, "nor in the future")
	// Muchas fusiones y vueltas: lo que Acme dio de alta lo sigue cambiando Acme (D-74), por larga
	// que sea la cadena de relaciones que se continúan.
	for range 6 {
		undo(merge(cede.ID, survivor.ID))
	}
	e.must(e.do("PUT", "/api/parties/"+cede.ID+"/name", e.seller, map[string]any{"givenName": "Cedida", "firstSurname": tag + " bis"}, nil), 200,
		"after six merges undone")
	if p, ok := e.party(cede.ID); !ok || len(p.Organizations) != 2 {
		e.t.Fatalf("the party is affiliated to acme and bilbao: %+v", p)
	}
	e.must(e.do("GET", "/api/parties/"+survivor.ID, e.seller, nil, nil), 404, "and acme does not see the survivor")

	// La relación que pasa conserva su fin; una renovación que empieza después se queda en la que
	// cede, y al deshacer, si la que cede ya la tiene, la de la superviviente no vuelve.
	var renewing, heir papp.PartyDTO
	e.must(e.do("POST", "/api/persons", e.admin, map[string]any{"givenName": "Renueva", "firstSurname": tag}, &renewing), 201, "a customer that renews")
	end := time.Now().Add(48 * time.Hour).UTC().Truncate(time.Second)
	var current papp.RelationshipDTO
	e.must(e.do("POST", "/api/party-relationships", e.admin, map[string]any{"type": domain.RelCustomer.String(),
		"fromParty": renewing.ID, "toParty": acme.ID}, &current), 201, "buys until the day after tomorrow")
	e.must(e.do("POST", "/api/party-relationships/"+current.ID+"/terminate", e.admin, map[string]any{"at": end}, nil), 200, "ends then")
	var renewal papp.RelationshipDTO
	e.must(e.do("POST", "/api/party-relationships", e.admin, map[string]any{"type": domain.RelCustomer.String(),
		"fromParty": renewing.ID, "toParty": acme.ID, "since": end}, &renewal), 201, "and renews from then")
	e.must(e.do("POST", "/api/persons", e.admin, map[string]any{"givenName": "Hereda", "firstSurname": tag}, &heir), 201, "the survivor")
	m := merge(renewing.ID, heir.ID)
	if len(m.Merge.Transferred) != 1 || len(m.Merge.Kept) != 1 || m.Merge.Kept[0].Relationship != renewal.ID {
		e.t.Fatalf("a merge with a renewal ahead: %+v", m.Merge)
	}
	took, ok := find(heir, domain.RelCustomer, acme.ID, true)
	if !ok || took.Until == nil || !took.Until.Equal(end) {
		e.t.Fatalf("the relationship that passed keeps its end: %+v", took)
	}
	back = undo(m)
	if back.Merge == nil || len(back.Merge.Transferred) != 0 || len(back.Merge.Kept) != 1 || back.Merge.Kept[0].Relationship != took.ID {
		e.t.Fatalf("undoing, the merged party has the renewal: %+v", back.Merge)
	}
	if _, ok := find(heir, domain.RelCustomer, acme.ID, true); !ok {
		e.t.Fatalf("the survivor keeps what did not come back: %+v", relationships(heir))
	}

	// Si la superviviente terminó durante la fusión la relación que recibió, no vuelve, y la que
	// cedió deja de estar afiliada a la empresa al deshacer.
	var gone, gets papp.PartyDTO
	e.must(e.do("POST", "/api/persons", e.seller, map[string]any{"givenName": "Se Va", "firstSurname": tag,
		"affiliation": map[string]any{"organization": acme.ID, "relationshipType": domain.RelCustomer.String()}}, &gone), 201, "acme's customer")
	leaving, _ := find(gone, domain.RelCustomer, acme.ID, true)
	e.must(e.do("POST", "/api/party-relationships/"+leaving.ID+"/terminate", e.admin, map[string]any{"at": time.Now().Add(30 * 24 * time.Hour)}, nil), 200,
		"it will stop buying in a month")
	e.must(e.do("POST", "/api/persons", e.admin, map[string]any{"givenName": "Recibe", "firstSurname": tag}, &gets), 201, "the survivor")
	m = merge(gone.ID, gets.ID)
	e.must(e.do("POST", "/api/party-relationships/"+m.Merge.Transferred[0].Relationship+"/terminate", e.admin, nil, nil), 200,
		"the survivor stops buying from acme")
	if back = undo(m); len(back.Merge.Transferred) != 0 {
		e.t.Fatalf("an ended relationship came back: %+v", back.Merge)
	}
	e.must(e.do("GET", "/api/parties/"+gone.ID, e.seller, nil, nil), 404, "acme no longer sees the party that left")

	// Una empresa del grupo no cede en una fusión (D-99): ni siquiera el administrador global.
	var branch, branchHeir, client papp.PartyDTO
	e.must(e.do("POST", "/api/organizations", e.admin, map[string]any{"legalName": "Sucursal " + tag,
		"roles": []string{domain.RoleInternalOrganization.String()}}, &branch), 201, "a company of the group")
	e.must(e.do("POST", "/api/organizations", e.admin, map[string]any{"legalName": "Sucursal Buena " + tag,
		"roles": []string{domain.RoleInternalOrganization.String()}}, &branchHeir), 201, "the company it is merged into")
	e.must(e.do("POST", "/api/persons", e.admin, map[string]any{"givenName": "Clienta", "firstSurname": tag,
		"affiliation": map[string]any{"organization": branch.ID, "relationshipType": domain.RelCustomer.String()}}, &client), 201, "its customer")
	e.must(e.do("POST", "/api/party-relationships", e.admin, map[string]any{"type": domain.RelDuplicateOf.String(),
		"fromParty": branch.ID, "toParty": branchHeir.ID}, nil), 422, "a company of the group does not cede in a merge (D-99)")

	// Quien fusiona sólo mueve las relaciones con empresas en las que escribe: el clerk de Acme
	// fusiona dos fichas que puede cambiar, y la relación con Bilbao se queda en la que cede.
	var both, kin papp.PartyDTO
	e.must(e.do("POST", "/api/persons", e.seller, map[string]any{"givenName": "Ambas", "firstSurname": tag,
		"affiliation": map[string]any{"organization": acme.ID, "relationshipType": domain.RelCustomer.String()}}, &both), 201, "a customer of acme")
	e.must(e.do("POST", "/api/party-relationships", e.admin, map[string]any{"type": domain.RelCustomer.String(),
		"fromParty": both.ID, "toParty": bilbao.ID}, nil), 201, "and of bilbao")
	e.must(e.do("POST", "/api/persons", e.seller, map[string]any{"givenName": "Pariente", "firstSurname": tag,
		"affiliation": map[string]any{"organization": acme.ID, "relationshipType": domain.RelSupplier.String()}}, &kin), 201, "acme's supplier")
	var scoped papp.RelationshipDTO
	e.must(e.do("POST", "/api/party-relationships", e.clerk, map[string]any{"type": domain.RelDuplicateOf.String(),
		"fromParty": both.ID, "toParty": kin.ID}, &scoped), 201, "the clerk merges")
	if len(scoped.Merge.Transferred) != 1 || scoped.Merge.Transferred[0].Counterparty != acme.ID || len(scoped.Merge.Kept) != 1 ||
		scoped.Merge.Kept[0].Counterparty != bilbao.ID {
		e.t.Fatalf("a merge limited to the clerk's companies: %+v", scoped.Merge)
	}
	if _, ok := find(kin, domain.RelCustomer, bilbao.ID, true); ok {
		e.t.Fatalf("bilbao's customer moved by somebody of acme: %+v", relationships(kin))
	}

	// Y quien deshace tiene que poder escribir en las empresas de lo que vuelve: el clerk no deshace
	// una fusión que movió una relación con Bilbao.
	var wide, wider papp.PartyDTO
	e.must(e.do("POST", "/api/persons", e.seller, map[string]any{"givenName": "Amplia", "firstSurname": tag,
		"affiliation": map[string]any{"organization": acme.ID, "relationshipType": domain.RelCustomer.String()}}, &wide), 201, "a customer of acme")
	e.must(e.do("POST", "/api/party-relationships", e.admin, map[string]any{"type": domain.RelCustomer.String(),
		"fromParty": wide.ID, "toParty": bilbao.ID}, nil), 201, "and of bilbao")
	e.must(e.do("POST", "/api/persons", e.seller, map[string]any{"givenName": "Más Amplia", "firstSurname": tag,
		"affiliation": map[string]any{"organization": acme.ID, "relationshipType": domain.RelSupplier.String()}}, &wider), 201, "acme's supplier")
	m = merge(wide.ID, wider.ID)
	if len(m.Merge.Transferred) != 2 {
		e.t.Fatalf("the global administrator moves both: %+v", m.Merge)
	}
	e.must(e.do("POST", "/api/party-relationships/"+m.ID+"/terminate", e.clerk, nil, nil), 403, "the clerk cannot give back bilbao's customer")
	if p, _ := e.party(wide.ID); !p.Duplicate {
		e.t.Fatalf("a refused undo changed nothing: %+v", p)
	}
	undo(m)
}

// party lee una party como el administrador.
func (e *env) party(id string) (papp.PartyDTO, bool) {
	var p papp.PartyDTO
	return p, e.do("GET", "/api/parties/"+id, e.admin, nil, &p) == 200
}

// phase3 cubre el ámbito de organización (decisión P1), el alta dentro del ámbito, la jerarquía
// de organizaciones y los puertos para otros contextos.
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

	// Alta dentro del ámbito: el rol de cliente viene con la relación.
	e.must(e.do("POST", "/api/persons", e.clerk, map[string]any{"givenName": "Pedro", "firstSurname": "Ruiz"}, nil), 400, "affiliation required")
	e.must(e.do("POST", "/api/persons", e.clerk, map[string]any{"givenName": "Pedro", "firstSurname": "Ruiz",
		"affiliation": map[string]any{"organization": globex.ID, "relationshipType": domain.RelCustomer.String()}}, nil), 404, "globex is out of scope")
	e.must(e.do("POST", "/api/persons", e.clerk, map[string]any{"givenName": "Pedro", "firstSurname": "Ruiz",
		"affiliation": map[string]any{"organization": acme.ID, "relationshipType": domain.RelCustomer.String()}}, &pedro), 201, "register in scope")
	if len(pedro.Organizations) != 1 || pedro.Organizations[0] != acme.ID || len(pedro.Roles) != 1 || pedro.Roles[0].Name != "Customer" {
		e.t.Fatalf("pedro: %+v", pedro)
	}

	// Visibilidad: 404 fuera del ámbito, 403 sólo lectura.
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
	if page.Total != 2 { // ana (empleada, cliente) y pedro
		e.t.Fatalf("acme members: %d", page.Total)
	}

	// Una party sin relaciones es sólo para los administradores globales, salvo que esté
	// compartida.
	var loner papp.PartyDTO
	e.must(e.do("POST", "/api/organizations", e.admin, map[string]any{"legalName": "Banco Común " + acme.ID[:8],
		"roles": []string{domain.RoleFinancialInstitution.String()}}, &loner), 201, "register a bank")
	e.must(e.do("GET", "/api/parties/"+loner.ID, e.reader, nil, nil), 404, "unrelated party")
	e.must(e.do("PUT", "/api/parties/"+loner.ID+"/shared", e.clerk, map[string]any{"shared": true}, nil), 404, "only admins share")
	e.must(e.do("PUT", "/api/parties/"+loner.ID+"/shared", e.admin, map[string]any{"shared": true}, nil), 200, "share the bank")
	e.must(e.do("GET", "/api/parties/"+loner.ID, e.outsider, nil, nil), 200, "a shared party is visible to everyone")
	e.must(e.do("PUT", "/api/parties/"+loner.ID+"/name", e.clerk, map[string]any{"legalName": "Nope"}, nil), 403, "shared parties are read-only")
	e.must(e.do("POST", "/api/party-relationships", e.clerk, map[string]any{"type": domain.RelSupplier.String(),
		"fromParty": acme.ID, "toParty": loner.ID}, nil), 422, "a relationship gives no role to a party the caller cannot write")
	e.must(e.do("PUT", "/api/parties/"+loner.ID+"/active", e.clerk, map[string]any{"active": false}, nil), 403, "nor is it deactivated by who only reads it")

	// Jerarquía: oficina de Madrid -> división de ventas -> Acme.
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
	// Los nombres de las empresas del grupo son públicos en la instalación (D-101): el clerk ve el
	// nombre de una empresa que no es suya, pero no su ficha.
	var foreign papp.PartyDTO
	e.must(e.do("POST", "/api/organizations", e.admin, map[string]any{"legalName": "Grupo Ajeno " + acme.ID[:8],
		"roles": []string{domain.RoleInternalOrganization.String()}}, &foreign), 201, "a company of the group the clerk does not see")
	var names []papp.InternalOrganizationName
	e.must(e.do("GET", "/api/internal-organizations/names", e.clerk, nil, &names), 200, "the names of the companies of the group")
	if !slices.ContainsFunc(names, func(n papp.InternalOrganizationName) bool { return n.ID == foreign.ID && n.Name == foreign.Name }) ||
		!slices.ContainsFunc(names, func(n papp.InternalOrganizationName) bool { return n.ID == acme.ID }) {
		e.t.Fatalf("the clerk sees the name of every company of the group: %+v", names)
	}
	e.must(e.do("GET", "/api/parties/"+foreign.ID, e.clerk, nil, nil), 404, "but not the record of a company that is not its own")
	e.must(e.do("GET", "/api/internal-organizations/names", "", nil, nil), 401, "anonymous callers do not")

	// Una unidad se da de alta bajo su empresa con el rollup: «una organización» es lo que es
	// Acme, desempeñe los roles que desempeñe (D-53).
	var north papp.PartyDTO
	e.must(e.do("POST", "/api/organizations", e.clerk, map[string]any{"legalName": "Acme Norte " + acme.ID[:8],
		"roles":       []string{domain.RoleDepartment.String()},
		"affiliation": map[string]any{"organization": acme.ID, "relationshipType": domain.RelOrganizationRollup.String()}}, &north),
		201, "a department registered under its company")

	// Terminar la relación termina la visibilidad.
	var rels []papp.RelationshipDTO
	e.must(e.do("GET", "/api/parties/"+pedro.ID+"/relationships", e.clerk, nil, &rels), 200, "pedro's relationships")
	if len(rels) != 1 {
		e.t.Fatalf("relationships: %+v", rels)
	}
	e.must(e.do("POST", "/api/party-relationships/"+rels[0].ID+"/terminate", e.clerk, nil, nil), 200, "end the customer relationship")
	e.must(e.do("GET", "/api/parties/"+pedro.ID, e.clerk, nil, nil), 404, "pedro left acme's scope")
}

// phase2 cubre identificaciones, contactos y clasificaciones por HTTP.
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
	// Dónde en la calle, pieza a pieza: se guarda y se vuelve a leer tal como se dio, cada pieza
	// opcional.
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

// tickingClock es un reloj que avanza un milisegundo cada vez que se lee: dentro de una misma
// petición, cada lectura del reloj da un instante posterior, como el reloj real cuando salta.
type tickingClock struct{ *fake.FakeClock }

func (c tickingClock) Now() time.Time {
	c.Advance(time.Millisecond)
	return c.FakeClock.Now()
}

// Lo que hizo una fusión se cuenta en el instante en que se movieron las relaciones, no en la
// fecha de la marca. Sin fecha, la marca toma el reloj al llegar la petición y la fusión lo lee
// otra vez después: si el reloj avanzó entre las dos lecturas, las relaciones que pasaron empiezan
// después de la marca y, contadas en la marca, ni las nuevas habrían empezado ni las que cedieron
// habrían terminado (salían todas en Kept; fallaba a veces en host/pairs_test.go).
func TestParties_MergeReportsWhatMovedWhenTheClockAdvances(t *testing.T) {
	e := compose(t, newMemoryDirectory)
	var acme, cede, survivor papp.PartyDTO
	e.must(e.do("POST", "/api/organizations", e.admin, map[string]any{"legalName": "Acme reloj",
		"roles": []string{domain.RoleInternalOrganization.String()}}, &acme), 201, "a company")
	e.must(e.do("POST", "/api/persons", e.admin, map[string]any{"givenName": "Cede", "firstSurname": "Reloj"}, &cede), 201, "the duplicate")
	e.must(e.do("POST", "/api/party-relationships", e.admin, map[string]any{"type": domain.RelCustomer.String(),
		"fromParty": cede.ID, "toParty": acme.ID}, nil), 201, "it buys from acme")
	e.must(e.do("POST", "/api/party-relationships", e.admin, map[string]any{"type": domain.RelSupplier.String(),
		"fromParty": acme.ID, "toParty": cede.ID}, nil), 201, "and acme buys from it")
	e.must(e.do("POST", "/api/persons", e.admin, map[string]any{"givenName": "Superviviente", "firstSurname": "Reloj"}, &survivor), 201, "the survivor")

	t.Cleanup(fw.SetClock(tickingClock{e.clock}))
	var mark papp.RelationshipDTO
	e.must(e.do("POST", "/api/party-relationships", e.admin, map[string]any{"type": domain.RelDuplicateOf.String(),
		"fromParty": cede.ID, "toParty": survivor.ID}, &mark), 201, "merge")
	if m := mark.Merge; m == nil || len(m.Transferred) != 2 || len(m.Kept) != 0 {
		t.Fatalf("what the merge did: %+v", mark.Merge)
	}
}

// El escenario completo corre dos veces: sobre el directorio de seguridad en memoria del framework
// y sobre el contexto delimitado Security. Nada de Parties cambia entre los dos: sus casos de uso
// leen el contexto de autorización, no de dónde viene.
func TestParties_EndToEnd_MemoryThenSQLite(t *testing.T) {
	t.Run("in-memory directory", func(t *testing.T) { endToEnd(t, newMemoryDirectory) })
	t.Run("Security directory", func(t *testing.T) { endToEnd(t, newSecurityDirectory) })
}

// roleTypes añade tipos de rol al catálogo: un rol sin datos propios es una fila colocada bajo su
// padre, y a partir de ahí se asigna como cualquier otro.
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

	// Lo que no cabe en el catálogo.
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

	// Se asigna como cualquier otro, a quien dice su lugar en la jerarquía.
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

	// Quién lo añadió, y lo que dijo, queda en el registro de auditoría.
	trail, err := e.mod.Audit.Trail(context.Background(), domain.RoleTypeKind, added.ID)
	if err != nil || len(trail) != 1 || trail[0].Operation != "created" || trail[0].Actor.Name != "ana.admin" || len(trail[0].Changes) != 4 {
		t.Fatalf("audit of the new role type: %+v %v", trail, err)
	}

	// Un tipo de relación se añade igual: qué rol desempeña cada lado.
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

	// Y relaciona parties como cualquier otro: al lado al que le falta su rol se le da.
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

	// Se cambia a una base SQLite migrada mientras el servicio sigue funcionando.
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
	if n != 5 { // empleada (anulado), cliente por la relación, cliente de facturación, contratista (terminado), empleada otra vez
		t.Fatalf("ana's roles in SQL: %d", n)
	}

	// Rastro de auditoría y Published Language, ambos confirmados con los cambios.
	trail, err := e.mod.Audit.Trail(ctx, domain.PartyKind, acme.ID)
	if err != nil || len(trail) != 6 || trail[1].Actor.Name != "ana.admin" || len(trail[3].Changes) != 1 || trail[3].Changes[0].Field != "contacts" ||
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
	if len(seen) != 21 || roles != 6 {
		t.Fatalf("published: %v, roles %d", seen, roles)
	}
	if len(shares) != 3 || shares[0] != "30.00" || shares[1] != "45.50" || shares[2] != "" {
		t.Fatalf("share events: %v", shares)
	}
	// Concedida en el alta, ampliada, concedida después y retirada.
	if len(trials) != 4 || trials[0].Organization != acme.ID || trials[0].TrialUntil == nil || trials[3].TrialUntil != nil ||
		!trials[1].TrialUntil.After(*trials[0].TrialUntil) || trials[2].Prospect != trials[3].Prospect {
		t.Fatalf("trial events: %+v", trials)
	}

	// Al final, para que la party que da de alta no esté entre las contadas arriba.
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
