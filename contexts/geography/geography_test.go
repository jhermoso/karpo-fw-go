package geography_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"slices"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jhermoso/karpo-fw-go/contexts/geography"
	gapp "github.com/jhermoso/karpo-fw-go/contexts/geography/application"
	"github.com/jhermoso/karpo-fw-go/contexts/geography/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/geography/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/geography/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authorization"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution/jwtauth"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
	"github.com/jhermoso/karpo-fw-go/pkg/testing/archtest"
)

type env struct {
	t            *testing.T
	srv          *httptest.Server
	sw           *hotswap.Switch
	mod          *geography.Module
	token, other string
	// reader reads boundaries and nothing of the calendar; keeper keeps the calendar.
	reader, keeper string
}

func compose(t *testing.T) *env {
	ctx := context.Background()
	jwt, _ := jwtauth.New(jwtauth.Config{Secret: []byte("geography-test")})
	dir := authorization.NewMemoryDirectory()
	reader, stranger, keeper := fw.NewUUID(), fw.NewUUID(), fw.NewUUID()
	dir.Put(keeper, authz.Subject{Active: true, Permissions: []authz.Permission{gapp.PermBoundaryRead, gapp.PermHolidayRead, gapp.PermHolidayUpdate}})
	dir.Put(reader, authz.Subject{Active: true, Permissions: []authz.Permission{gapp.PermBoundaryRead, gapp.PermReferenceRead}})
	dir.Put(stranger, authz.Subject{Active: true})
	token := func(sub fw.UUID) string {
		s, _ := jwt.Issue(jwtauth.Claims{Subject: sub.String(), Username: "u" + sub.String()[:4], PartyID: fw.NewUUID().String(),
			ExpiresAt: fw.Now().Add(time.Hour).Unix()})
		return "Bearer " + s
	}
	store := memory.NewStore("memory")
	if err := infrastructure.LoadMemory(ctx, store); err != nil {
		t.Fatal(err)
	}
	sw := hotswap.New(store)
	mod := geography.Compose(sw)
	mux := http.NewServeMux()
	mod.HTTP.RegisterRoutes(mux)
	mod.HolidaysHTTP.RegisterRoutes(mux)
	srv := httptest.NewServer(distribution.Chain(mux, distribution.Authorize(jwt, authorization.NewResolver(dir, authorization.Options{}))))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { _ = sw.Close(ctx) })
	return &env{t: t, srv: srv, sw: sw, mod: mod, token: token(reader), other: token(stranger), reader: token(reader), keeper: token(keeper)}
}

func (e *env) do(method, path, auth string, body, out any) int {
	e.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, &buf)
	req.Header.Set("Authorization", auth)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	if out != nil && res.StatusCode < 300 {
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			e.t.Fatalf("%s %s: %v", method, path, err)
		}
	}
	return res.StatusCode
}

func (e *env) must(got, want int, what string) {
	e.t.Helper()
	if got != want {
		e.t.Fatalf("%s: status %d, want %d", what, got, want)
	}
}

// scenario exercises the context through HTTP and its ports; the same on every backend.
func (e *env) scenario() {
	t := e.t
	ctx := context.Background()
	e.must(e.do("GET", "/api/reference/countries", e.other, nil, nil), 403, "permission required")

	var page fw.Page[gapp.BoundaryDTO]
	e.must(e.do("GET", "/api/geography/boundaries?q=madrid&type="+domain.TypeMunicipality.String(), e.token, nil, &page), 200, "search")
	var madrid gapp.BoundaryDTO
	for _, b := range page.Items {
		if b.Name == "Madrid" {
			madrid = b
		}
	}
	if madrid.ID == "" || madrid.INE != "28079" || madrid.TypeName != "Municipality" {
		t.Fatalf("Madrid municipality: %+v", page.Items)
	}
	var detail gapp.BoundaryDetailDTO
	e.must(e.do("GET", "/api/geography/boundaries/"+madrid.ID, e.token, nil, &detail), 200, "boundary")
	names := []string{}
	for _, a := range detail.Ancestors {
		names = append(names, a.Name)
	}
	if len(names) < 3 || names[0] != "Madrid" || !slices.Contains(names, "Spain") {
		t.Fatalf("ancestors of Madrid: %v", names)
	}

	var codes []gapp.PostalCodeDTO
	e.must(e.do("GET", "/api/geography/postal-codes?country=ES&code=28013", e.token, nil, &codes), 200, "postal code")
	if len(codes) != 1 || codes[0].Boundary.ID != madrid.ID {
		t.Fatalf("28013: %+v", codes)
	}
	e.must(e.do("GET", "/api/geography/postal-codes?country=ES&code=2801", e.token, nil, nil), 400, "Spanish postal codes have 5 digits")

	var checked contracts.PostalAddressResult
	e.must(e.do("POST", "/api/geography/address-check", e.token, map[string]any{"Country": "ES", "PostalCode": "28013",
		"Boundary": madrid.ID}, &checked), 200, "address in Madrid")
	if checked.PostalCodeID != codes[0].ID {
		t.Fatalf("checked: %+v", checked)
	}
	var barcelona fw.Page[gapp.BoundaryDTO]
	e.must(e.do("GET", "/api/geography/boundaries?q=barcelona&type="+domain.TypeMunicipality.String(), e.token, nil, &barcelona), 200, "barcelona")
	e.must(e.do("POST", "/api/geography/address-check", e.token, map[string]any{"Country": "ES", "PostalCode": "28013",
		"Boundary": barcelona.Items[0].ID}, nil), 400, "28013 is not in Barcelona")

	var es gapp.CountryDTO
	e.must(e.do("GET", "/api/reference/countries/es", e.token, nil, &es), 200, "Spain")
	if es.Name != "Spain" || es.Alpha3 != "ESP" || !es.EU || !es.Eurozone || es.Currencies[0] != "EUR" ||
		es.Languages[0] != "es" || es.TimeZones[0] != "Europe/Madrid" || es.IBANLength != 24 {
		t.Fatalf("Spain: %+v", es)
	}
	var countries []contracts.CountryInfo
	e.must(e.do("GET", "/api/reference/countries", e.token, nil, &countries), 200, "countries")
	if len(countries) != 248 {
		t.Fatalf("countries: %d", len(countries))
	}
	var streets []domain.StreetType
	e.must(e.do("GET", "/api/reference/street-types?country=ES", e.token, nil, &streets), 200, "street types")
	if len(streets) == 0 {
		t.Fatal("street types")
	}
	var members fw.Page[gapp.BoundaryDTO]
	e.must(e.do("GET", "/api/geography/boundaries?size=100&grouping="+url.QueryEscape(detailGrouping(t, e, es.Boundary)), e.token, nil, &members), 200, "EU members")
	if members.Total != 27 {
		t.Fatalf("EU members: %d", members.Total)
	}

	// Ports for other contexts.
	p := e.mod.Ports
	desc, err := p.Descendants(ctx, []string{detail.Ancestors[0].ID}) // the province
	if err != nil || len(desc) < 150 {
		t.Fatalf("municipalities of the province: %d %v", len(desc), err)
	}
	all, err := p.Descendants(ctx, []string{es.Boundary})
	if err != nil || len(all) < 8000 {
		t.Fatalf("all of Spain (chunked IN lists): %d %v", len(all), err)
	}
	of, err := p.CountryOf(ctx, []string{madrid.ID, detail.Ancestors[0].ID, es.Boundary})
	if err != nil || len(of) != 3 || of[madrid.ID] != "ES" {
		t.Fatalf("country of: %v %v", of, err)
	}
	if err := p.CheckIBAN(ctx, "ES91 2100 0418 4502 0005 1332"); err != nil {
		t.Fatal(err)
	}
	if err := p.CheckIBAN(ctx, "ES91 2100 0418 4502 0005 13"); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("a short Spanish IBAN: %v", err)
	}
}

// detailGrouping returns the European Union: the grouping Spain is a member of.
func detailGrouping(t *testing.T, e *env, spain string) string {
	var d gapp.BoundaryDetailDTO
	e.must(e.do("GET", "/api/geography/boundaries/"+spain, e.token, nil, &d), 200, "spain")
	for _, g := range d.Groupings {
		if g.Name == "European Union" {
			return g.ID
		}
	}
	t.Fatalf("Spain groupings: %+v", d.Groupings)
	return ""
}

func TestGeography_MemoryThenSQLite(t *testing.T) {
	e := compose(t)
	e.scenario()
	e.holidays()

	ctx := context.Background()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "geo.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
	m, _ := infrastructure.Migrator(db)
	start := time.Now()
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	t.Logf("schema and %s seed loaded in %v", "32,500-row", time.Since(start))
	if err := m.Verify(ctx); err != nil {
		t.Fatal(err)
	}
	if err := e.sw.Swap(ctx, db); err != nil {
		t.Fatal(err)
	}
	e.scenario()
	e.holidays()
}

func TestGeography_LayersRespectTheArchitecture(t *testing.T) {
	const m = "github.com/jhermoso/karpo-fw-go"
	archtest.AssertOnlyImports(t, "domain", false, archtest.Std, m+"/pkg/domain/...", m+"/contexts/geography/domain")
	archtest.AssertOnlyImports(t, "contracts", false, archtest.Std)
	archtest.AssertTreeDoesNotImport(t, "application", []string{"/pkg/persistence", "/pkg/distribution", "database/sql", "net/http"})
}
