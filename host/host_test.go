package host_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	expapp "github.com/jhermoso/karpo-fw-go/contexts/exports/application"
	finapp "github.com/jhermoso/karpo-fw-go/contexts/financial/application"
	geoinfra "github.com/jhermoso/karpo-fw-go/contexts/geography/infrastructure"
	impapp "github.com/jhermoso/karpo-fw-go/contexts/imports/application"
	"github.com/jhermoso/karpo-fw-go/contexts/security"
	secapp "github.com/jhermoso/karpo-fw-go/contexts/security/application"
	"github.com/jhermoso/karpo-fw-go/host"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
)

const (
	bootPassword = "boot-password-0001"
	rootPassword = "root-password-0002"
)

type client struct {
	t   *testing.T
	url string
}

// do sends a request with a bearer token ("" for none) and decodes a 2xx answer into out; a
// *[]byte takes the body as it comes.
func (c client) do(method, path, token string, body, out any) int {
	c.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest(method, c.url+path, &buf)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	if out != nil && res.StatusCode < 300 {
		if raw, ok := out.(*[]byte); ok {
			*raw, _ = io.ReadAll(res.Body)
		} else if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			c.t.Fatal(err)
		}
	}
	return res.StatusCode
}

func (c client) must(got, want int, what string) {
	c.t.Helper()
	if got != want {
		c.t.Fatalf("%s: status %d, want %d", what, got, want)
	}
}

// scenario starts a host on a migrated backend and walks through what only the whole does: the
// first administrator, a session, an import that registers companies in Parties, an account in
// one of them, its export written by the chores, the messages delivered and the history read.
func scenario(t *testing.T, sw *hotswap.Switch) {
	ctx := context.Background()
	t.Setenv(security.EnvBootstrapUser, "root")
	t.Setenv(security.EnvBootstrapPassword, bootPassword)
	h, err := host.Compose(sw, host.Options{JWTSecret: []byte("host-test-secret")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := host.Compose(sw, host.Options{}); err == nil {
		t.Fatal("a host does not start without a secret to sign sessions")
	}

	// Starting: the permissions of every context, the catalog of modules and the first administrator.
	started, err := h.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if started.Permissions != len(host.Permissions()) || started.Permissions < 140 || started.Features == 0 || started.Bootstrap != secapp.BootstrapCreated {
		t.Fatalf("started: %+v", started)
	}
	again, err := h.Start(ctx)
	if err != nil || again.Features != 0 || again.Bootstrap != secapp.BootstrapNotNeeded {
		t.Fatalf("starting again changes nothing: %+v %v", again, err)
	}

	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)
	c := client{t: t, url: srv.URL}

	// Nothing without a session; the administrator changes the password first.
	c.must(c.do("GET", "/api/financial/accounts", "", nil, nil), 401, "no session")
	c.must(c.do("GET", "/api/audit/types", "not-a-token", nil, nil), 401, "a bad token")
	var tok secapp.Tokens
	c.must(c.do("POST", "/api/auth/login", "", map[string]any{"username": "root", "password": "wrong-password-0000"}, nil), 401, "wrong password")
	c.must(c.do("POST", "/api/auth/login", "", map[string]any{"username": "root", "password": bootPassword}, &tok), 200, "login")
	c.must(c.do("GET", "/api/financial/accounts", tok.AccessToken, nil, nil), 403, "the password first")
	c.must(c.do("POST", "/api/auth/change-password", tok.AccessToken, map[string]any{"currentPassword": bootPassword, "newPassword": rootPassword}, &tok), 200, "change password")
	root := tok.AccessToken
	var who authz.Resolution
	c.must(c.do("GET", "/api/auth/context", root, nil, &who), 200, "context")
	if who.Context == nil || !who.Context.GlobalAdmin {
		t.Fatalf("the first administrator is global: %+v", who)
	}

	// Every permission of every context is in the catalog of Security.
	var catalog []struct {
		Code   string `json:"code"`
		Active bool   `json:"active"`
	}
	c.must(c.do("GET", "/api/security/permissions", root, nil, &catalog), 200, "permissions")
	known := []string{}
	for _, p := range catalog {
		if p.Active {
			known = append(known, p.Code)
		}
	}
	for _, p := range host.Permissions() {
		if !slices.Contains(known, string(p)) {
			t.Fatalf("%s is not in the catalog (%d codes)", p, len(known))
		}
	}

	// An import registers its companies in Parties through the loader of the host; the kinds
	// nobody loads yet are skipped saying so.
	files := impapp.RunImport{Source: "personio", Files: []impapp.FileDTO{{Role: "org-units", Name: "org.csv",
		Content: "unitType,name,parentOrg,notes\nInternalOrganization,Maccorp Exact Change,,\nInternalOrganization,Karpo Servicios,,\nDepartment,Operaciones,Karpo Servicios,\n"}}}
	var run impapp.RunDTO
	c.must(c.do("POST", "/api/imports/runs", root, files, &run), 201, "import")
	count := func(kind string) impapp.CountDTO {
		for _, k := range run.Counts {
			if k.Kind == kind {
				return k
			}
		}
		return impapp.CountDTO{}
	}
	if run.Status != "succeeded" || count("legal-entity").Created != 2 || count("department").Skipped != 1 || run.Warnings != 1 {
		t.Fatalf("import: %+v", run)
	}
	companies, err := h.Parties.Organizations.All(ctx)
	if err != nil || len(companies) != 2 {
		t.Fatalf("companies in Parties: %+v %v", companies, err)
	}
	// Again, and with the name written otherwise: nothing new.
	files.Files[0].Content = "unitType,name\nInternalOrganization,MACCORP exact  change\nInternalOrganization,Karpo Servicios\n"
	c.must(c.do("POST", "/api/imports/runs", root, files, &run), 201, "import again")
	if count("legal-entity").Created != 0 || count("legal-entity").Unchanged != 2 {
		t.Fatalf("import again: %+v", run.Counts)
	}
	if companies, _ = h.Parties.Organizations.All(ctx); len(companies) != 2 {
		t.Fatalf("no company twice: %+v", companies)
	}
	maccorp := companies[0].ID
	if companies[0].Name != "Maccorp Exact Change" {
		maccorp = companies[1].ID
	}

	// An account of a customer of one of them, and its list as a file: asked over HTTP, written by
	// the chores of the host, downloaded by who asked.
	var acc finapp.AccountDTO
	c.must(c.do("POST", "/api/financial/accounts", root, map[string]any{"company": maccorp, "number": "ES91 2100 0418 4502 0005 1332",
		"holder": companies[0].ID, "name": "Cuenta de pago", "uses": []string{"customer-payment"}}, &acc), 201, "open an account")
	var job expapp.JobDTO
	c.must(c.do("POST", "/api/exports", root, map[string]any{"dataset": "customer-accounts", "filter": map[string]string{"company": maccorp}}, &job), 202, "ask for the export")
	chores, err := h.RunChores(ctx)
	if err != nil || chores.ExportsWritten != 1 || chores.ImportsClosed != 0 || chores.Quotes != 0 || chores.Reservations != 0 {
		t.Fatalf("chores: %+v %v", chores, err)
	}
	var file []byte
	c.must(c.do("GET", "/api/exports/"+job.ID+"/download", root, nil, &file), 200, "download")
	if !strings.Contains(string(file), "Número,Nombre,Divisa,Estado,Titular,Apertura,Cierre,Virtual,Pruebas\r\nES9121000418450200051332,Cuenta de pago,EUR,active,") ||
		!strings.HasSuffix(string(file), ",No,No\r\n") {
		t.Fatalf("file: %q", file)
	}
	if idle, err := h.RunChores(ctx); err != nil || idle != (host.Chores{}) {
		t.Fatalf("nothing left to do: %+v %v", idle, err)
	}

	// What the contexts published reaches those that listen, and then there is nothing to carry.
	moved, err := h.Deliver(ctx)
	if err != nil || moved < 5 {
		t.Fatalf("delivered: %d %v", moved, err)
	}
	if moved, err = h.Deliver(ctx); err != nil || moved != 0 {
		t.Fatalf("delivered twice: %d %v", moved, err)
	}

	// The history of anything, read in one place.
	var types []struct {
		Type string `json:"type"`
	}
	c.must(c.do("GET", "/api/audit/types", root, nil, &types), 200, "kinds with a history")
	kinds := []string{}
	for _, k := range types {
		kinds = append(kinds, k.Type)
	}
	for _, k := range []string{"financial.account", "parties.party", "exports.job", "imports.run", "orders.quote", "security.user"} {
		if !slices.Contains(kinds, k) {
			t.Fatalf("no history of %s: %v", k, kinds)
		}
	}
	var trail []map[string]any
	c.must(c.do("GET", "/api/audit/trail/financial.account/"+acc.ID, root, nil, &trail), 200, "the history of the account")
	if len(trail) != 1 {
		t.Fatalf("trail: %+v", trail)
	}
}

func TestHost_OnMemory(t *testing.T) {
	store := memory.NewStore("memory")
	if err := geoinfra.LoadMemory(context.Background(), store); err != nil {
		t.Fatal(err)
	}
	sw := hotswap.New(store)
	t.Cleanup(func() { _ = sw.Close(context.Background()) })
	scenario(t, sw)
}

func TestHost_OnSQLite(t *testing.T) {
	ctx := context.Background()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "karpo.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
	m, err := sqlrepo.NewMigrator(db, host.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Verify(ctx); err != nil {
		t.Fatal(err)
	}
	scenario(t, hotswap.New(db))
}
