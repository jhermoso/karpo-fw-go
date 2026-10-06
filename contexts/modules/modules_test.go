package modules_test

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

	"github.com/jhermoso/karpo-fw-go/contexts/modules"
	mapp "github.com/jhermoso/karpo-fw-go/contexts/modules/application"
	"github.com/jhermoso/karpo-fw-go/contexts/modules/domain"
	minfra "github.com/jhermoso/karpo-fw-go/contexts/modules/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authorization"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
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

func TestModules_Rules(t *testing.T) {
	for code, ok := range map[string]bool{"ventas": true, "tpv-2": true, "": false, "TPV": false, "con espacio": false, "-x": false, "x-": false,
		"abcdefghijklmnopqrstuvwxyzabcde": false} {
		if domain.ValidCode(code) != ok {
			t.Fatalf("code %q", code)
		}
	}
	for i, s := range []domain.FeatureState{{Kind: "plugin", Code: "x", Name: "X"}, {Kind: domain.Module, Code: "con espacio", Name: "X"},
		{Kind: domain.Module, Code: "x", Name: " "}} {
		if _, err := domain.ReconstituteFeature(domain.NewFeatureID(), s); !errors.Is(err, fw.ErrValidation) {
			t.Fatalf("feature %d: %v", i, err)
		}
	}
	f, err := domain.ReconstituteFeature(domain.NewFeatureID(), domain.FeatureState{Kind: domain.Module, Code: " TPV ", Name: " Punto de venta "})
	if err != nil || f.State().Code != "tpv" || f.State().Name != "Punto de venta" {
		t.Fatalf("feature: %+v %v", f, err)
	}
	if err := f.Change("", "", true); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("name: %v", err)
	}
	if err := f.Change("TPV", "Caja", true); err != nil || !f.State().Retired || f.State().Code != "tpv" {
		t.Fatalf("change: %v", err)
	}
	a, err := domain.NewActivation(domain.NewActivationID(), domain.OrganizationID{UUID: fw.NewUUID()}, f)
	if err != nil || a.State().Active {
		t.Fatalf("activation: %v", err)
	}
	at := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	if changed, err := a.Deactivate("ana", at, ""); err != nil || changed {
		t.Fatalf("off already: %v %v", changed, err)
	}
	if changed, err := a.Activate("ana", at, "Contratado en mayo"); err != nil || !changed || !a.State().Active || a.State().ActivatedBy != "ana" {
		t.Fatalf("on: %v %v", changed, err)
	}
	if changed, _ := a.Activate("luis", at.Add(time.Hour), ""); changed || a.State().ActivatedBy != "ana" {
		t.Fatal("switching on what is on changes nothing")
	}
	if changed, _ := a.Activate("luis", at.Add(time.Hour), "Renovado"); !changed || a.State().Notes != "Renovado" || a.State().ActivatedBy != "ana" {
		t.Fatal("only the notes change")
	}
	if changed, err := a.Deactivate("luis", at.Add(2*time.Hour), ""); err != nil || !changed || a.State().Active || a.State().DeactivatedBy != "luis" ||
		a.State().Notes != "Renovado" {
		t.Fatalf("off: %v %v", changed, err)
	}
	if changed, _ := a.Activate("ana", at.Add(3*time.Hour), ""); !changed || !a.State().DeactivatedAt.IsZero() || len(a.PendingEvents()) != 3 {
		t.Fatalf("on again: %+v", a.State())
	}
	if !domain.Sector.Exclusive() || domain.Module.Exclusive() {
		t.Fatal("a company has one sector and many modules")
	}
}

type host struct {
	t        *testing.T
	srv      *httptest.Server
	sw       *hotswap.Switch
	dir      *authorization.MemoryDirectory
	ids      map[string]fw.UUID
	tokens   map[string]string
	mod      *modules.Module
	adminCtx context.Context
}

func compose(t *testing.T) *host {
	ctx := context.Background()
	sw := hotswap.New(memory.NewStore("memory"))
	mm := modules.Compose(sw)
	jwt, _ := jwtauth.New(jwtauth.Config{Secret: []byte("modules-test")})
	dir := authorization.NewMemoryDirectory()
	h := &host{t: t, sw: sw, dir: dir, ids: map[string]fw.UUID{}, tokens: map[string]string{}, mod: mm}
	users := map[string][]authz.Permission{
		"manager":  {mapp.PermActivationRead, mapp.PermActivationUpdate, mapp.PermCatalogRead},
		"editor":   {mapp.PermCatalogRead, mapp.PermCatalogUpdate},
		"viewer":   {mapp.PermActivationRead, mapp.PermCatalogRead},
		"nobody":   nil,
		"loner":    mapp.Permissions(),
		"outsider": mapp.Permissions(),
	}
	for u, perms := range users {
		h.ids[u] = fw.NewUUID()
		dir.Put(h.ids[u], authz.Subject{Active: true, Permissions: perms})
		tok, _ := jwt.Issue(jwtauth.Claims{Subject: h.ids[u].String(), Username: u, PartyID: fw.NewUUID().String(), ExpiresAt: fw.Now().Add(time.Hour).Unix()})
		h.tokens[u] = "Bearer " + tok
	}
	ac, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "setup", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
	ac.GlobalAdmin = true
	h.adminCtx = authz.WithContext(ctx, ac)
	mux := http.NewServeMux()
	mm.RegisterRoutes(mux)
	h.srv = httptest.NewServer(distribution.Chain(mux, distribution.Authorize(jwt, authorization.NewResolver(dir, authorization.Options{}))))
	t.Cleanup(h.srv.Close)
	t.Cleanup(func() { _ = sw.Close(ctx) })
	return h
}

func (h *host) grant(user, org string) {
	s, _, _ := h.dir.Subject(context.Background(), h.ids[user])
	s.Grants = append(s.Grants, authz.Grant{OrganizationID: fw.MustParseUUID(org), Level: authz.Full})
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

func (h *host) scenario() {
	t := h.t
	ctx := h.adminCtx
	svc := h.mod.Service
	acme, globex := fw.NewUUID().String(), fw.NewUUID().String()
	for _, u := range []string{"manager", "editor", "viewer", "nobody"} {
		h.grant(u, acme)
	}
	h.grant("outsider", globex)
	ok := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	// The catalog the product starts with, added once.
	if n, err := h.mod.EnsureCatalog(context.Background()); err != nil || n != len(domain.Seed) {
		t.Fatalf("seed: %d %v", n, err)
	}
	if n, err := h.mod.EnsureCatalog(context.Background()); err != nil || n != 0 {
		t.Fatalf("seed again: %d %v", n, err)
	}
	var catalog []mapp.FeatureDTO
	h.must(h.do("GET", "/api/modules/catalog", "nobody", nil, nil), 403, "no permission")
	h.must(h.do("GET", "/api/modules/catalog?kind=plugin", "viewer", nil, nil), 400, "kind")
	h.must(h.do("GET", "/api/modules/catalog", "viewer", nil, &catalog), 200, "catalog")
	if len(catalog) != 12 || catalog[0].Kind != "capability" || catalog[0].Code != "consulting" || catalog[4].Code != "compras" {
		t.Fatalf("catalog: %+v", catalog)
	}
	// Only a global administrator changes it.
	tpvBody := map[string]any{"kind": "module", "code": "TPV", "name": "Punto de venta"}
	h.must(h.do("POST", "/api/modules/catalog", "viewer", tpvBody, nil), 403, "viewer")
	h.must(h.do("POST", "/api/modules/catalog", "editor", tpvBody, nil), 403, "the catalog is not of a company")
	tpv, err := svc.Define.Handle(ctx, mapp.DefineFeature{Kind: "module", Code: "TPV", Name: "Punto de venta"})
	ok(err)
	if _, err := svc.Define.Handle(ctx, mapp.DefineFeature{Kind: "module", Code: "tpv", Name: "Otro"}); !errors.Is(err, fw.ErrRuleViolation) {
		t.Fatalf("defined once: %v", err)
	}
	if _, err := svc.Define.Handle(ctx, mapp.DefineFeature{Kind: "module", Code: "punto de venta", Name: "X"}); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("code: %v", err)
	}
	for _, s := range []string{"retail", "hospitality"} {
		_, err := svc.Define.Handle(ctx, mapp.DefineFeature{Kind: "sector", Code: s, Name: s})
		ok(err)
	}

	// What Acme has on.
	on := func(kind, code, notes string) map[string]any {
		return map[string]any{"organization": acme, "kind": kind, "code": code, "notes": notes}
	}
	var act mapp.ActivationDTO
	h.must(h.do("POST", "/api/modules/activate", "viewer", on("module", "ventas", ""), nil), 403, "viewer")
	h.must(h.do("POST", "/api/modules/activate", "outsider", on("module", "ventas", ""), nil), 404, "outsider")
	h.must(h.do("POST", "/api/modules/activate", "manager", on("plugin", "ventas", ""), nil), 400, "kind")
	h.must(h.do("POST", "/api/modules/activate", "manager", on("module", "nominas", ""), nil), 422, "not in the catalog")
	h.must(h.do("POST", "/api/modules/activate", "manager", on("module", "Ventas", ""), &act), 200, "sales")
	if !act.Active || act.Code != "ventas" || act.ActivatedBy == "" || act.ActivatedAt == "" || act.Version != 1 {
		t.Fatalf("on: %+v", act)
	}
	h.must(h.do("POST", "/api/modules/activate", "manager", on("module", "ventas", ""), &act), 200, "sales again")
	if act.Version != 1 {
		t.Fatalf("switching on what is on changes nothing: %+v", act)
	}
	h.must(h.do("POST", "/api/modules/activate", "manager", on("module", "ventas", "Contrato 2026"), &act), 200, "notes")
	if act.Version != 2 || act.Notes != "Contrato 2026" {
		t.Fatalf("notes: %+v", act)
	}
	h.must(h.do("POST", "/api/modules/activate", "manager", on("capability", "logistics", ""), nil), 200, "logistics")
	h.must(h.do("POST", "/api/modules/activate", "manager", on("module", "inventario", ""), nil), 200, "inventory")
	// One sector: the new one replaces the old one.
	h.must(h.do("POST", "/api/modules/activate", "manager", on("sector", "retail", ""), nil), 200, "retail")
	h.must(h.do("POST", "/api/modules/activate", "manager", on("sector", "hospitality", ""), nil), 200, "hospitality")

	var cur mapp.CurrentDTO
	h.must(h.do("GET", "/api/modules/current", "nobody", nil, &cur), 200, "what my companies have on")
	if !slices.Equal(cur.Modules, []string{"inventario", "ventas"}) || !slices.Equal(cur.Capabilities, []string{"logistics"}) ||
		!slices.Equal(cur.Sectors, []string{"hospitality"}) {
		t.Fatalf("current: %+v", cur)
	}
	h.must(h.do("GET", "/api/modules/current", "loner", nil, &cur), 200, "no companies")
	if len(cur.Modules)+len(cur.Capabilities)+len(cur.Sectors) != 0 {
		t.Fatalf("nothing without companies: %+v", cur)
	}
	all, err := svc.Current.Handle(ctx, mapp.GetCurrent{})
	if err != nil || len(all.Modules) != 9 || len(all.Capabilities) != 4 || len(all.Sectors) != 2 {
		t.Fatalf("a global administrator sees everything offered: %+v %v", all, err)
	}

	// Off, and on again; a retired feature stays where it is on but is not switched on anew.
	h.must(h.do("POST", "/api/modules/deactivate", "viewer", on("module", "ventas", ""), nil), 403, "viewer")
	h.must(h.do("POST", "/api/modules/deactivate", "manager", on("module", "crm", ""), nil), 422, "never had it")
	h.must(h.do("POST", "/api/modules/deactivate", "manager", on("module", "ventas", ""), &act), 200, "off")
	if act.Active || act.DeactivatedBy == "" || act.Version != 3 {
		t.Fatalf("off: %+v", act)
	}
	h.must(h.do("POST", "/api/modules/deactivate", "manager", on("module", "ventas", ""), &act), 200, "off again")
	if act.Version != 3 {
		t.Fatalf("switching off what is off changes nothing: %+v", act)
	}
	act = mapp.ActivationDTO{}
	h.must(h.do("POST", "/api/modules/activate", "manager", on("module", "ventas", ""), &act), 200, "on again")
	if !act.Active || act.DeactivatedAt != "" || act.Notes != "Contrato 2026" {
		t.Fatalf("on again: %+v", act)
	}
	inventory := catalog[slices.IndexFunc(catalog, func(f mapp.FeatureDTO) bool { return f.Code == "inventario" })]
	iid, _ := domain.ParseFeatureID(inventory.ID)
	tid, _ := domain.ParseFeatureID(tpv.ID)
	for _, id := range []domain.FeatureID{iid, tid} {
		_, err := svc.Change.Handle(ctx, mapp.ChangeFeature{ID: id, Name: "Retirado", Retired: true})
		ok(err)
	}
	h.must(h.do("PUT", "/api/modules/catalog/"+tpv.ID, "editor", map[string]any{"name": "TPV"}, nil), 403, "the catalog is not of a company")
	h.must(h.do("POST", "/api/modules/activate", "manager", on("module", "tpv", ""), nil), 422, "no longer offered")
	h.must(h.do("GET", "/api/modules/current", "nobody", nil, &cur), 200, "current")
	if !slices.Equal(cur.Modules, []string{"inventario", "ventas"}) {
		t.Fatalf("a retired module stays on: %+v", cur)
	}
	h.must(h.do("POST", "/api/modules/deactivate", "manager", on("module", "inventario", ""), nil), 200, "inventory off")
	h.must(h.do("POST", "/api/modules/activate", "manager", on("module", "inventario", ""), nil), 422, "and it does not come back")
	h.must(h.do("GET", "/api/modules/catalog?kind=module", "viewer", nil, &catalog), 200, "modules offered")
	if len(catalog) != 7 {
		t.Fatalf("modules offered: %+v", catalog)
	}
	h.must(h.do("GET", "/api/modules/catalog?kind=module&retired=true", "viewer", nil, &catalog), 200, "all modules")
	if len(catalog) != 9 {
		t.Fatalf("all modules: %+v", catalog)
	}

	var acts []mapp.ActivationDTO
	h.must(h.do("GET", "/api/modules/activations?organization="+acme, "outsider", nil, nil), 404, "outsider")
	h.must(h.do("GET", "/api/modules/activations?organization="+acme, "viewer", nil, &acts), 200, "activations")
	if len(acts) != 5 || acts[0].Kind != "capability" || acts[1].Code != "inventario" || acts[1].Active || acts[3].Code != "hospitality" || !acts[3].Active ||
		acts[4].Code != "retail" || acts[4].Active {
		t.Fatalf("activations: %+v", acts)
	}
	var orgs []string
	h.must(h.do("GET", "/api/modules/organizations?kind=capability&code=logistics", "viewer", nil, &orgs), 200, "who has logistics")
	if !slices.Equal(orgs, []string{acme}) {
		t.Fatalf("organizations: %v", orgs)
	}
	h.must(h.do("GET", "/api/modules/organizations?kind=capability&code=logistics", "outsider", nil, &orgs), 200, "outsider")
	if len(orgs) != 0 {
		t.Fatalf("outsider: %v", orgs)
	}

	// The port other contexts ask.
	if has, err := h.mod.Features.Has(context.Background(), acme, "module", "VENTAS"); err != nil || !has {
		t.Fatalf("port: %v %v", has, err)
	}
	if has, _ := h.mod.Features.Has(context.Background(), acme, "module", "inventario"); has {
		t.Fatal("port: inventory is off")
	}
	if has, _ := h.mod.Features.Has(context.Background(), globex, "module", "ventas"); has {
		t.Fatal("port: another company")
	}
	if of, err := h.mod.Features.Of(context.Background(), acme, "sector"); err != nil || !slices.Equal(of, []string{"hospitality"}) {
		t.Fatalf("port: %v %v", of, err)
	}
	// ventas on, logistics on, inventario on, retail on, hospitality on + retail off, ventas off,
	// ventas on, inventario off.
	if n, err := h.mod.Relay(inprocess.NewBroker()).RelayOnce(context.Background()); err != nil || n != 9 {
		t.Fatalf("published: %d %v", n, err)
	}
}

func TestModules_SwitchesFeaturesPerCompany_MemoryThenSQLite(t *testing.T) {
	h := compose(t)
	h.scenario()

	ctx := context.Background()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "host.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{minfra.Migrations()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.sw.Swap(ctx, db); err != nil {
		t.Fatal(err)
	}
	h.scenario()
}

func TestModules_LayersRespectTheArchitecture(t *testing.T) {
	const m = "github.com/jhermoso/karpo-fw-go"
	archtest.AssertOnlyImports(t, "domain", false, archtest.Std, m+"/pkg/domain/...", m+"/contexts/modules/domain")
	archtest.AssertOnlyImports(t, "contracts", false, archtest.Std)
	archtest.AssertTreeDoesNotImport(t, "application", []string{"/pkg/persistence", "/pkg/distribution", "database/sql", "net/http"})
}
