package infrastructure_test

import (
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/geography/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/geography/infrastructure"
)

func TestMapKeys_LadderIsComplete(t *testing.T) {
	s, err := infrastructure.LoadSeed()
	if err != nil {
		t.Fatal(err)
	}
	keys, err := infrastructure.MapKeys(s, 10)
	if err != nil {
		t.Fatal(err)
	}
	byCode := map[string]infrastructure.MapKey{}
	count := map[string]int{}
	children := map[string]int{}
	for _, k := range keys {
		if _, dup := byCode[k.Code]; dup {
			t.Fatalf("clave repetida %s", k.Code)
		}
		byCode[k.Code] = k
		count[k.Kind]++
		children[k.ParentCode]++
	}
	want := map[string]int{"country": 248, "region": 19, "province": 49, "island": 10, "municipality": 8132}
	for kind, n := range want {
		if count[kind] != n {
			t.Errorf("%s: %d entidades, esperaba %d", kind, count[kind], n)
		}
	}
	for _, k := range keys {
		if k.Kind == "country" {
			if k.ParentCode != "" {
				t.Errorf("el país %s tiene padre %s", k.Code, k.ParentCode)
			}
		} else if _, ok := byCode[k.ParentCode]; !ok {
			t.Errorf("%s: el padre %q no está en la tabla", k.Code, k.ParentCode)
		}
		if k.HasChildren != (children[k.Code] > 0) {
			t.Errorf("%s: hasChildren=%v con %d hijos", k.Code, k.HasChildren, children[k.Code])
		}
		if k.Kind == "municipality" && (k.LAU != k.Code || k.INE != k.Code) {
			t.Errorf("%s: lau=%s ine=%s", k.Code, k.LAU, k.INE)
		}
		if (k.IslandOf != "") != (k.Kind == "municipality" && byCode[k.ParentCode].Kind == "island") {
			t.Errorf("%s: islandOf=%q", k.Code, k.IslandOf)
		}
	}
	for code, parent := range map[string]string{"07040": "ES532", "38038": "ES709", "ES709": "ES-CN", "ES532": "ES-IB",
		"ES-MD": "ES", "51001": "51", "28": "ES-MD", "28079": "28"} {
		if byCode[code].ParentCode != parent {
			t.Errorf("padre de %s = %q, esperaba %s", code, byCode[code].ParentCode, parent)
		}
	}
	// Las provincias insulares no tienen nivel: no están en el mapa, sus islas sí, y ningún
	// municipio cuelga de ellas.
	islandProvinces := 0
	for _, b := range s.Boundaries {
		if b.Type() != domain.TypeProvince || b.Position().Level != domain.NoLevel {
			continue
		}
		islandProvinces++
		if _, ok := byCode[b.Codes().INE]; ok {
			t.Errorf("la provincia sin nivel %s está en el mapa", b.Codes().INE)
		}
		if children[b.Codes().INE] > 0 {
			t.Errorf("algo cuelga de la provincia sin nivel %s", b.Codes().INE)
		}
		islands := 0
		for _, c := range s.Boundaries {
			if p, ok := c.Parent(); ok && p == b.ID() && c.Type() == domain.TypeIsland {
				islands++
			}
		}
		if islands == 0 {
			t.Errorf("la provincia sin nivel %s no tiene islas", b.Codes().INE)
		}
	}
	if islandProvinces != 3 {
		t.Errorf("%d provincias sin nivel, esperaba 3", islandProvinces)
	}
	if k := byCode["ES5"]; k.Code != "" {
		t.Error("el NUTS 1 está en el mapa")
	}
}

func TestMapKey_RowFollowsTheHeader(t *testing.T) {
	row := (infrastructure.MapKey{Code: "07040", ParentCode: "ES532", Name: "Palma", Level: "local", Kind: "municipality",
		HasChildren: false, LAU: "07040", INE: "07040", IslandOf: "ES532"}).Row()
	if len(row) != len(infrastructure.MapKeyHeader) || row[5] != "false" || row[8] != "ES532" {
		t.Fatal(row)
	}
	if (infrastructure.MapKey{HasChildren: true}).Row()[5] != "true" {
		t.Fatal("hasChildren")
	}
}
