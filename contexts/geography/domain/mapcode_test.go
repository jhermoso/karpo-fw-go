package domain_test

import (
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/geography/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

func TestMapCode_FollowsTheKeyRule(t *testing.T) {
	unknown := domain.MustBoundaryTypeID("10000000-0000-0000-0005-0000000000ff")
	cases := []struct {
		name               string
		typ                domain.BoundaryTypeID
		codes              domain.Codes
		country            string
		level              domain.Level
		wantCode, wantKind string
	}{
		{"país", domain.TypeCountry, domain.Codes{Geo: "ES", Abbreviation: "ESP"}, "ES", domain.LevelCountry, "ES", "country"},
		{"país sin nivel", domain.TypeCountry, domain.Codes{Geo: "ES"}, "ES", domain.NoLevel, "", ""},
		{"comunidad", domain.TypeAutonomous, domain.Codes{Geo: "ES30", Abbreviation: "MD", NUTS: "ES30"}, "ES", domain.Level2, "ES-MD", "region"},
		{"Navarra, defecto conocido", domain.TypeAutonomous, domain.Codes{Geo: "ES22", Abbreviation: "NA"}, "ES", domain.Level2, "ES-NA", "region"},
		{"comunidad sin abreviatura", domain.TypeAutonomous, domain.Codes{Geo: "ES30"}, "ES", domain.Level2, "", ""},
		{"comunidad sin país", domain.TypeAutonomous, domain.Codes{Abbreviation: "MD"}, "", domain.Level2, "", ""},
		{"provincia", domain.TypeProvince, domain.Codes{Geo: "28", INE: "28", NUTS: "ES300"}, "ES", domain.Level3, "28", "province"},
		{"provincia insular sin nivel", domain.TypeProvince, domain.Codes{Geo: "38", INE: "38"}, "ES", domain.NoLevel, "", ""},
		{"provincia 07 sin nivel", domain.TypeProvince, domain.Codes{Geo: "07", INE: "07"}, "ES", domain.NoLevel, "", ""},
		{"provincia con INE mal formado", domain.TypeProvince, domain.Codes{INE: "2A"}, "ES", domain.Level3, "", ""},
		{"Tenerife", domain.TypeIsland, domain.Codes{Geo: "ES709", NUTS: "ES709"}, "ES", domain.Level3, "ES709", "island"},
		{"Mallorca", domain.TypeIsland, domain.Codes{Geo: "ES532", NUTS: "ES532"}, "ES", domain.Level3, "ES532", "island"},
		{"07040", domain.TypeMunicipality, domain.Codes{Geo: "07040", INE: "07040", LAU: "07040"}, "ES", domain.LevelLocal, "07040", "municipality"},
		{"38038", domain.TypeMunicipality, domain.Codes{INE: "38038"}, "ES", domain.LevelLocal, "38038", "municipality"},
		{"28079", domain.TypeMunicipality, domain.Codes{INE: "28079"}, "ES", domain.LevelLocal, "28079", "municipality"},
		{"51001", domain.TypeMunicipality, domain.Codes{INE: "51001"}, "ES", domain.LevelLocal, "51001", "municipality"},
		{"municipio sin INE", domain.TypeMunicipality, domain.Codes{Geo: "28079"}, "ES", domain.LevelLocal, "", ""},
		{"municipio con INE corto", domain.TypeMunicipality, domain.Codes{INE: "2807"}, "ES", domain.LevelLocal, "", ""},
		{"NUTS 1", domain.TypeRegion, domain.Codes{Geo: "ES3", NUTS: "ES3"}, "ES", domain.Level1, "", ""},
		{"región de nivel 2 sin regla", domain.TypeRegion, domain.Codes{Geo: "ES30"}, "ES", domain.Level2, "", ""},
		{"tipo desconocido", unknown, domain.Codes{Geo: "X"}, "ES", domain.Level3, "", ""},
		{"continente", domain.TypeContinent, domain.Codes{Geo: "150"}, "", domain.NoLevel, "", ""},
	}
	for _, c := range cases {
		b, err := domain.ReconstituteBoundary(domain.BoundaryID{UUID: fw.NewUUID()}, c.name, c.typ, c.codes,
			domain.Position{Country: c.country, Level: c.level}, true, nil)
		if err != nil {
			t.Fatal(err)
		}
		code, kind, ok := b.MapCode()
		if code != c.wantCode || kind != c.wantKind || ok != (c.wantCode != "") {
			t.Errorf("%s: MapCode() = %q, %q, %v; quería %q, %q", c.name, code, kind, ok, c.wantCode, c.wantKind)
		}
	}
}

func TestMapParents_ClimbsToTheFirstAncestorOnTheMap(t *testing.T) {
	var all []*domain.Boundary
	mk := func(name string, typ domain.BoundaryTypeID, codes domain.Codes, level domain.Level, parent *domain.Boundary) *domain.Boundary {
		var links []domain.Link
		if parent != nil {
			links = []domain.Link{{ID: fw.NewUUID(), Parent: parent.ID(), Kind: domain.Containment}}
		}
		b, err := domain.ReconstituteBoundary(domain.BoundaryID{UUID: fw.NewUUID()}, name, typ, codes,
			domain.Position{Country: "ES", Level: level}, true, links)
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, b)
		return b
	}
	europe := mk("Europa", domain.TypeContinent, domain.Codes{Geo: "150"}, domain.NoLevel, nil)
	spain := mk("España", domain.TypeCountry, domain.Codes{Geo: "ES"}, domain.LevelCountry, europe)
	east := mk("Este", domain.TypeRegion, domain.Codes{Geo: "ES5"}, domain.Level1, spain)
	balears := mk("Illes Balears", domain.TypeAutonomous, domain.Codes{Abbreviation: "IB"}, domain.Level2, east)
	province := mk("Balears", domain.TypeProvince, domain.Codes{INE: "07"}, domain.NoLevel, balears)
	mallorca := mk("Mallorca", domain.TypeIsland, domain.Codes{Geo: "ES532"}, domain.Level3, province)
	palma := mk("Palma", domain.TypeMunicipality, domain.Codes{INE: "07040"}, domain.LevelLocal, mallorca)
	orphan := mk("Huérfano", domain.TypeMunicipality, domain.Codes{INE: "99999"}, domain.LevelLocal, nil)
	lost := mk("Perdido", domain.TypeMunicipality, domain.Codes{INE: "99998"}, domain.LevelLocal, orphan)
	byID := map[domain.BoundaryID]*domain.Boundary{}
	for _, b := range all {
		if b != orphan {
			byID[b.ID()] = b
		}
	}
	calls := 0
	load := func(ids []domain.BoundaryID) ([]*domain.Boundary, error) {
		calls++
		seen := map[domain.BoundaryID]bool{}
		var out []*domain.Boundary
		for _, id := range ids {
			if seen[id] {
				t.Fatalf("identidad repetida en una ronda")
			}
			seen[id] = true
			if b, ok := byID[id]; ok {
				out = append(out, b)
			}
		}
		return out, nil
	}
	got, err := domain.MapParents(all, 10, load)
	if err != nil {
		t.Fatal(err)
	}
	want := map[domain.BoundaryID]string{palma.ID(): "ES532", mallorca.ID(): "ES-IB", balears.ID(): "ES"}
	if len(got) != len(want) {
		t.Fatalf("MapParents = %v", got)
	}
	for id, code := range want {
		if got[id] != code {
			t.Errorf("padre de %v = %q, esperaba %s", id, got[id], code)
		}
	}
	if calls != 2 {
		t.Errorf("%d rondas, esperaba 2", calls)
	}
	_ = lost
	// Con una sola ronda, la isla y la comunidad (que necesitan dos) se quedan sin padre.
	calls = 0
	got, _ = domain.MapParents(all, 1, load)
	if calls != 1 || got[palma.ID()] != "ES532" || got[mallorca.ID()] != "" || got[balears.ID()] != "" {
		t.Errorf("una ronda: %d llamadas, %v", calls, got)
	}
	if _, err := domain.MapParents(all, 10, func([]domain.BoundaryID) ([]*domain.Boundary, error) { return nil, fw.ErrNotFound }); err == nil {
		t.Error("el error de la carga se pierde")
	}
}

func TestMapCode_DoesNotDependOnActive(t *testing.T) {
	for _, active := range []bool{true, false} {
		b, err := domain.ReconstituteBoundary(domain.BoundaryID{UUID: fw.NewUUID()}, "Madrid", domain.TypeMunicipality,
			domain.Codes{INE: "28079"}, domain.Position{Country: "ES", Level: domain.LevelLocal}, active, nil)
		if err != nil {
			t.Fatal(err)
		}
		if code, kind, ok := b.MapCode(); code != "28079" || kind != "municipality" || !ok {
			t.Errorf("active=%v: MapCode() = %q, %q, %v", active, code, kind, ok)
		}
	}
}
