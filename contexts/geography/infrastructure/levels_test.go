package infrastructure

import (
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/jhermoso/karpo-fw-go/contexts/geography/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
)

// officialNUTS reads the classification Eurostat publishes (GISCO, NUTS_AT_2024.csv, downloaded
// on 2026-10-12 from https://gisco-services.ec.europa.eu/distribution/v2/nuts/csv/): code -> name
// of the regions of a country.
func officialNUTS(t *testing.T, country string) map[string]string {
	t.Helper()
	f, err := os.Open(filepath.Join("seed", "official", "NUTS_AT_"+domain.NUTSVersion+".csv.gz"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(gz).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	col := map[string]int{}
	for i, h := range rows[0] {
		col[strings.TrimPrefix(h, "\ufeff")] = i // the file may start with a byte order mark
	}
	out := map[string]string{}
	for _, r := range rows[1:] {
		if r[col["CNTR_CODE"]] == country {
			out[r[col["NUTS_ID"]]] = strings.TrimSpace(r[col["NAME_LATN"]])
		}
	}
	return out
}

// Every NUTS code of the seed is an official one, every official one is in the seed, and the
// level of a region is the one its code says.
func TestSeed_NUTSCodesAreTheOfficialOnes(t *testing.T) {
	official := officialNUTS(t, "ES")
	if len(official) != 86 {
		t.Fatalf("official Spanish regions: %d", len(official))
	}
	s, err := LoadSeed()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	byID := map[domain.BoundaryID]*domain.Boundary{}
	for _, b := range s.Boundaries {
		byID[b.ID()] = b
	}
	for _, b := range s.Boundaries {
		code, p := b.Codes().NUTS, b.Position()
		if code == "" {
			if p.Level == domain.Level1 || p.Level == domain.Level2 || p.Level == domain.Level3 {
				t.Errorf("%s is level %s without a NUTS code", b.Name(), p.Level)
			}
			continue
		}
		if _, ok := official[code]; !ok {
			t.Errorf("%s carries %s, which is not an official code", b.Name(), code)
		}
		if seen[code] {
			t.Errorf("%s is carried twice", code)
		}
		seen[code] = true
		want := domain.Level([]string{"country", "1", "2", "3"}[len(code)-2])
		if p.Level != want || p.Country != "ES" {
			t.Errorf("%s (%s): position %+v, want level %s of ES", b.Name(), code, p, want)
		}
		if b.Type() == domain.TypeIsland {
			if b.Name() != official[code] {
				t.Errorf("island %s is called %q, officially %q", code, b.Name(), official[code])
			}
			parent, ok := b.Parent()
			if !ok || byID[parent].Type() != domain.TypeProvince || byID[parent].Codes().NUTS != "" {
				t.Errorf("island %s must hang from an insular province", b.Name())
			}
		}
	}
	for code, name := range official {
		if !seen[code] {
			t.Errorf("the seed lacks %s %s", code, name)
		}
	}
}

// Every municipality is in the NUTS 3 region the official LAU table says: its province on the
// mainland, its island in the Balearic and Canary Islands.
func TestSeed_MunicipalitiesAreInTheirOfficialRegion(t *testing.T) {
	f, err := os.Open(filepath.Join("seed", "official", "LAU_ES_2024.csv.gz"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(gz).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	official := map[string]string{} // LAU code -> NUTS 3
	for _, r := range rows[1:] {
		official[r[1]] = r[0]
	}
	s, err := LoadSeed()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[domain.BoundaryID]*domain.Boundary{}
	for _, b := range s.Boundaries {
		byID[b.ID()] = b
	}
	municipalities, insular := 0, 0
	for _, b := range s.Boundaries {
		if b.Type() != domain.TypeMunicipality {
			continue
		}
		municipalities++
		want, ok := official[b.Codes().LAU]
		if !ok {
			t.Errorf("%s (%s) is not in the official table", b.Name(), b.Codes().LAU)
			continue
		}
		parent, ok := b.Parent()
		if !ok {
			t.Errorf("%s has no parent", b.Name())
			continue
		}
		p := byID[parent]
		if p.Position().Level != domain.Level3 || p.Codes().NUTS != want {
			t.Errorf("%s hangs from %s (%s, level %q), officially in %s", b.Name(), p.Name(), p.Codes().NUTS, p.Position().Level, want)
		}
		if p.Type() == domain.TypeIsland {
			insular++
		}
	}
	if municipalities != len(official) || insular != 155 {
		t.Fatalf("%d municipalities against %d official ones, %d of them on an island", municipalities, len(official), insular)
	}
}

// A database seeded before the levels existed converges on the same data as a new one, and
// running the migration again changes nothing.
func TestSeedLevels_UpgradesAnOldDatabase(t *testing.T) {
	ctx := context.Background()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "geo.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
	m, err := Migrator(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	const dump = `SELECT b.id || '|' || b.name || '|' || b.boundary_type || '|' || COALESCE(b.nuts_code, '') || '|' ||
		COALESCE(b.country, '') || '|' || COALESCE(b.boundary_level, '') FROM geo_boundaries b ORDER BY 1`
	snapshot := func() []string {
		t.Helper()
		var out []string
		for _, q := range []string{dump,
			"SELECT id || '|' || name FROM geo_boundary_types ORDER BY 1",
			"SELECT boundary_id || '|' || parent || '|' || kind FROM geo_boundary_links ORDER BY 1",
			"SELECT country || '|' || boundary_level || '|' || name || '|' || principal FROM geo_country_levels ORDER BY 1"} {
			rows, err := raw.QueryContext(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			for rows.Next() {
				var s string
				if err := rows.Scan(&s); err != nil {
					t.Fatal(err)
				}
				out = append(out, s)
			}
			if err := rows.Close(); err != nil {
				t.Fatal(err)
			}
		}
		return out
	}
	fresh := snapshot()

	// Back to what migration 2 left with the seed of before.
	for _, stmt := range []string{
		// The municipalities of the islands, back under their province.
		`UPDATE geo_boundary_links SET parent = (SELECT l.parent FROM geo_boundary_links l WHERE l.boundary_id = geo_boundary_links.parent
			AND l.kind = 'containment') WHERE kind = 'containment' AND parent IN (SELECT id FROM geo_boundaries WHERE boundary_level = '3' AND ine_code IS NULL)`,
		"DELETE FROM geo_country_levels",
		"DELETE FROM geo_boundary_links WHERE boundary_id IN (SELECT id FROM geo_boundaries WHERE boundary_level = '3' AND ine_code IS NULL)",
		"DELETE FROM geo_boundaries WHERE boundary_level = '3' AND ine_code IS NULL",
		"DELETE FROM geo_boundary_types WHERE name = 'Island'",
		"UPDATE geo_boundaries SET boundary_type = (SELECT id FROM geo_boundary_types WHERE name = 'Region') WHERE boundary_level = '2'",
		"UPDATE geo_boundaries SET nuts_code = 'ES530' WHERE ine_code = '07'",
		"UPDATE geo_boundaries SET nuts_code = 'ES703' WHERE ine_code = '35'",
		"UPDATE geo_boundaries SET nuts_code = 'ES707' WHERE ine_code = '38'",
		"UPDATE geo_boundaries SET country = NULL, boundary_level = NULL",
	} {
		if _, err := raw.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	if old := snapshot(); len(old) != len(fresh)-10-10-1-4 {
		t.Fatalf("the old database should lack ten islands, their links, a type and four levels: %d rows against %d", len(old), len(fresh))
	}

	for pass := 1; pass <= 2; pass++ {
		if err := seedLevels(ctx, db); err != nil {
			t.Fatal(err)
		}
		if err := islandMunicipalities(ctx, db); err != nil {
			t.Fatal(err)
		}
		got := snapshot()
		if len(got) != len(fresh) {
			t.Fatalf("pass %d: %d rows, want %d", pass, len(got), len(fresh))
		}
		for i := range got {
			if got[i] != fresh[i] {
				t.Fatalf("pass %d: %q, want %q", pass, got[i], fresh[i])
			}
		}
	}
}
