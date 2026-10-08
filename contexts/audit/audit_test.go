package audit_test

import (
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

	"github.com/jhermoso/karpo-fw-go/contexts/assets"
	sapp "github.com/jhermoso/karpo-fw-go/contexts/assets/application"
	sdomain "github.com/jhermoso/karpo-fw-go/contexts/assets/domain"
	sinfra "github.com/jhermoso/karpo-fw-go/contexts/assets/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/audit"
	aapp "github.com/jhermoso/karpo-fw-go/contexts/audit/application"
	"github.com/jhermoso/karpo-fw-go/contexts/modules"
	mapp "github.com/jhermoso/karpo-fw-go/contexts/modules/application"
	mdomain "github.com/jhermoso/karpo-fw-go/contexts/modules/domain"
	minfra "github.com/jhermoso/karpo-fw-go/contexts/modules/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authorization"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution/jwtauth"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
	"github.com/jhermoso/karpo-fw-go/pkg/testing/archtest"
)

// host composes Assets, Modules and Audit: the history of an asset is guarded by the query that
// loads it; the catalog of Modules has no company, so its history is for global administrators.
type host struct {
	t        *testing.T
	srv      *httptest.Server
	sw       *hotswap.Switch
	dir      *authorization.MemoryDirectory
	ids      map[string]fw.UUID
	tokens   map[string]string
	assets   *assets.Module
	modules  *modules.Module
	audit    *audit.Module
	adminCtx context.Context
}

func compose(t *testing.T) *host {
	ctx := context.Background()
	sw := hotswap.New(memory.NewStore("memory"))
	am := assets.Compose(sw)
	mm := modules.Compose(sw)
	hm := audit.Compose().
		Register(sdomain.AssetKind, am.Audit, aapp.Seeing(sdomain.ParseAssetID, func(id sdomain.AssetID) sapp.GetAsset { return sapp.GetAsset{ID: id} }, am.Service.GetAsset)).
		Register(mdomain.FeatureKind, mm.Audit, nil)

	jwt, _ := jwtauth.New(jwtauth.Config{Secret: []byte("audit-test")})
	dir := authorization.NewMemoryDirectory()
	h := &host{t: t, sw: sw, dir: dir, ids: map[string]fw.UUID{}, tokens: map[string]string{}, assets: am, modules: mm, audit: hm}
	users := map[string][]authz.Permission{
		"auditor":  {aapp.PermTrailRead, sapp.PermAssetRead},
		"keeper":   {sapp.PermAssetRead, sapp.PermAssetUpdate},
		"blind":    {aapp.PermTrailRead},
		"outsider": {aapp.PermTrailRead, sapp.PermAssetRead},
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
	hm.RegisterRoutes(mux)
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

func (h *host) get(path, user string, out any) int {
	h.t.Helper()
	req, _ := http.NewRequest("GET", h.srv.URL+path, nil)
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
	acme, globex := fw.NewUUID().String(), fw.NewUUID().String()
	for _, u := range []string{"auditor", "keeper", "blind"} {
		h.grant(u, acme)
	}
	h.grant("outsider", globex)
	ok := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	// An asset with a life: registered, depreciated a quarter, sold.
	van, err := h.assets.Service.Register.Handle(ctx, sapp.RegisterAsset{Company: acme, Code: "FUR-" + tag, Name: "Furgoneta", Class: "vehicles",
		Acquired: vocab.MustDate(2026, 1, 10), InService: vocab.MustDate(2026, 1, 16), Cost: "12000", Residual: "2000", LifeMonths: 48})
	ok(err)
	_, err = h.assets.Service.Depreciate.Handle(ctx, sapp.RunDepreciation{Company: acme, Year: 2026, Month: 3})
	ok(err)
	vid, _ := sdomain.ParseAssetID(van.ID)
	_, err = h.assets.Service.Dispose.Handle(ctx, sapp.DisposeAsset{ID: vid, Date: vocab.MustDate(2026, 7, 10), Kind: "sale", Proceeds: "11000"})
	ok(err)

	path := "/api/audit/trail/assets.asset/" + van.ID
	var trail []aapp.EntryDTO
	h.must(h.get(path, "keeper", nil), 403, "seeing the asset is not reading its history")
	h.must(h.get(path, "blind", nil), 403, "nor is reading histories seeing the asset")
	h.must(h.get(path, "outsider", nil), 404, "outsider")
	h.must(h.get("/api/audit/trail/assets.boat/"+van.ID, "auditor", nil), 400, "unknown kind")
	h.must(h.get("/api/audit/trail/assets.asset/not-an-id", "auditor", nil), 400, "id")
	h.must(h.get("/api/audit/trail/assets.asset/"+fw.NewUUID().String(), "auditor", nil), 404, "no such asset")
	h.must(h.get(path, "auditor", &trail), 200, "history")
	if len(trail) != 3 || trail[0].Operation != "created" || trail[0].Version != 1 || trail[0].Actor == "" || trail[0].At == "" ||
		trail[1].Operation != "updated" || trail[1].Version != 2 || trail[2].Version != 3 {
		t.Fatalf("history: %+v", trail)
	}
	change := func(e aapp.EntryDTO, field string) (aapp.ChangeDTO, bool) {
		i := slices.IndexFunc(e.Changes, func(c aapp.ChangeDTO) bool { return c.Field == field })
		if i < 0 {
			return aapp.ChangeDTO{}, false
		}
		return e.Changes[i], true
	}
	if c, found := change(trail[1], "accumulated"); !found || c.Old != "0" || c.New != "524.19" || len(trail[1].Changes) != 1 ||
		!slices.Contains(trail[1].Events, "assets.depreciation_charged") {
		t.Fatalf("the quarter: %+v", trail[1])
	}
	if c, found := change(trail[2], "status"); !found || c.Old != "in-service" || c.New != "disposed" || !slices.Contains(trail[2].Events, "assets.asset_disposed") {
		t.Fatalf("the sale: %+v", trail[2])
	}
	if !slices.Contains(trail[0].Events, "assets.asset_registered") {
		t.Fatalf("the registration: %+v", trail[0])
	}

	// A kind of aggregate without a guard: only a global administrator.
	feature, err := h.modules.Service.Define.Handle(ctx, mapp.DefineFeature{Kind: "module", Code: "tpv-" + tag, Name: "TPV"})
	ok(err)
	fid, _ := mdomain.ParseFeatureID(feature.ID)
	_, err = h.modules.Service.Change.Handle(ctx, mapp.ChangeFeature{ID: fid, Name: "Punto de venta", Retired: true})
	ok(err)
	h.must(h.get("/api/audit/trail/modules.feature/"+feature.ID, "auditor", nil), 403, "the catalog has no company")
	entries, err := h.audit.Service.Trail.Handle(ctx, aapp.GetTrail{Type: "modules.feature", ID: feature.ID})
	if err != nil || len(entries) != 2 {
		t.Fatalf("history of the catalog: %+v %v", entries, err)
	}
	name, _ := change(entries[1], "name")
	retired, _ := change(entries[1], "retired")
	if name.Old != "TPV" || name.New != "Punto de venta" || retired.Old != false || retired.New != true || len(entries[1].Changes) != 2 {
		t.Fatalf("what changed in the catalog: %+v", entries[1])
	}
	if none, err := h.audit.Service.Trail.Handle(ctx, aapp.GetTrail{Type: "modules.feature", ID: fw.NewUUID().String()}); err != nil || len(none) != 0 {
		t.Fatalf("no history: %+v %v", none, err)
	}

	var types []aapp.TypeDTO
	h.must(h.get("/api/audit/types", "keeper", nil), 403, "no permission")
	h.must(h.get("/api/audit/types", "auditor", &types), 200, "kinds with a history")
	if len(types) != 2 || types[0].Type != "assets.asset" || types[0].Context != "assets" || types[0].Restricted || types[1].Type != "modules.feature" ||
		!types[1].Restricted {
		t.Fatalf("kinds: %+v", types)
	}
}

func TestAudit_ReadsTheHistoryOfWhatYouCanSee_MemoryThenSQLite(t *testing.T) {
	h := compose(t)
	h.scenario("m")

	ctx := context.Background()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "host.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{sinfra.Migrations(), minfra.Migrations()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.sw.Swap(ctx, db); err != nil {
		t.Fatal(err)
	}
	h.scenario("s")
}

func TestAudit_LayersRespectTheArchitecture(t *testing.T) {
	// Audit knows no other context: the host wires them.
	archtest.AssertTreeDoesNotImport(t, "application", []string{"/pkg/persistence", "/pkg/distribution", "database/sql", "net/http", "/contexts/"})
}
