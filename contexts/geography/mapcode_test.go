package geography_test

import (
	"context"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/geography"
	gapp "github.com/jhermoso/karpo-fw-go/contexts/geography/application"
	"github.com/jhermoso/karpo-fw-go/contexts/geography/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/geography/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
)

// countingBoundaries cuenta las consultas Find.
type countingBoundaries struct {
	domain.BoundaryRepository
	finds int
}

func (c *countingBoundaries) Find(ctx context.Context, s spec.Specification[*domain.Boundary], o ...spec.Order[*domain.Boundary]) ([]*domain.Boundary, error) {
	c.finds++
	return c.BoundaryRepository.Find(ctx, s, o...)
}

func mapFixture(t *testing.T) (context.Context, *geography.Module, *countingBoundaries) {
	ctx := context.Background()
	store := memory.NewStore("memory")
	if err := infrastructure.LoadMemory(ctx, store); err != nil {
		t.Fatal(err)
	}
	sw := hotswap.New(store)
	t.Cleanup(func() { _ = sw.Close(ctx) })
	mod := geography.Compose(sw)
	ac, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "test", Kind: authz.Service,
		Permissions: []authz.Permission{authz.Wildcard}})
	return authz.WithContext(ctx, ac), mod, &countingBoundaries{BoundaryRepository: mod.Ports.Boundaries}
}

func TestSearchBoundaries_ParentCodesInBatches(t *testing.T) {
	ctx, mod, counter := mapFixture(t)
	ports := mod.Ports
	ports.Boundaries = counter
	svc := gapp.NewService(ports)
	pages := map[string]gapp.SearchBoundaries{
		"municipios insulares": {Country: "ES", Level: "local", Text: "san", Size: 200},
		"nivel 3":              {Country: "ES", Level: "3", Size: 200},
		"comunidades":          {Country: "ES", Level: "2", Size: 200},
	}
	for name, q := range pages {
		counter.finds = 0
		page, err := svc.SearchBoundaries.Handle(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		if counter.finds > 2 {
			t.Errorf("%s: %d consultas Find para %d límites; como mucho 2", name, counter.finds, len(page.Items))
		}
		for _, b := range page.Items {
			if b.Code == "" || b.ParentCode == "" {
				t.Errorf("%s: %s sin code o sin parentCode: %+v", name, b.Name, b)
			}
		}
	}
	counter.finds = 0
	countries, err := svc.SearchBoundaries.Handle(ctx, gapp.SearchBoundaries{Type: domain.TypeCountry.String(), Size: 50})
	if err != nil {
		t.Fatal(err)
	}
	if counter.finds != 0 || countries.Items[0].Code == "" || countries.Items[0].ParentCode != "" {
		t.Errorf("países: %d consultas, %+v", counter.finds, countries.Items[0])
	}
}

func TestMapCodes_APIMatchesTheTileTable(t *testing.T) {
	ctx, mod, _ := mapFixture(t)
	s, err := infrastructure.LoadSeed()
	if err != nil {
		t.Fatal(err)
	}
	keys, err := infrastructure.MapKeys(s, gapp.MaxDepth)
	if err != nil {
		t.Fatal(err)
	}
	table := map[string]infrastructure.MapKey{}
	for _, k := range keys {
		table[k.Code] = k
	}
	seen := 0
	for _, level := range []string{"country", "1", "2", "3", "local"} {
		for n := 1; ; n++ {
			page, err := mod.Service.SearchBoundaries.Handle(ctx, gapp.SearchBoundaries{Level: level, Page: n, Size: 1000})
			if err != nil {
				t.Fatal(err)
			}
			for _, b := range page.Items {
				if b.Code == "" {
					continue
				}
				if level == "1" {
					t.Errorf("el NUTS 1 %s tiene code %s", b.Name, b.Code)
				}
				k, ok := table[b.Code]
				if !ok || k.Level != b.Level || k.ParentCode != b.ParentCode {
					t.Errorf("%s: API (%s, %s, %s) y tesela %+v", b.Name, b.Code, b.Level, b.ParentCode, k)
				}
				seen++
			}
			if n >= page.TotalPages {
				break
			}
		}
	}
	if seen != len(keys) {
		t.Errorf("la API da %d claves y la tabla %d", seen, len(keys))
	}
	// El GET de un límite también las lleva.
	page, _ := mod.Service.SearchBoundaries.Handle(ctx, gapp.SearchBoundaries{Text: "Tenerife", Type: domain.TypeIsland.String()})
	d, err := mod.Service.GetBoundary.Handle(ctx, gapp.GetBoundary{ID: page.Items[0].ID})
	if err != nil || d.Code != "ES709" || d.ParentCode != "ES-CN" {
		t.Fatalf("Tenerife: %+v, %v", d.BoundaryDTO, err)
	}
}

// La regla de claves del mapa y la de Locate (festivos) dicen lo mismo.
func TestMapCodes_RoundTripThroughLocate(t *testing.T) {
	ctx, mod, _ := mapFixture(t)
	s, err := infrastructure.LoadSeed()
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, b := range s.Boundaries {
		code, kind, ok := b.MapCode()
		if !ok || kind == domain.MapKindIsland || kind == domain.MapKindCountry && b.Codes().Geo != "ES" {
			continue
		}
		ref, err := mod.Holidays.Locate(ctx, code)
		if err != nil || ref.ID != b.ID().String() {
			t.Errorf("Locate(%s) = %+v, %v; esperaba %s", code, ref, err, b.Name())
		}
		checked++
	}
	if checked < 8000 {
		t.Fatalf("sólo %d claves comprobadas", checked)
	}
}
