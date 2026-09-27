package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/geography"
	gapp "github.com/jhermoso/karpo-fw-go/contexts/geography/application"
	"github.com/jhermoso/karpo-fw-go/contexts/geography/contracts"
	gdomain "github.com/jhermoso/karpo-fw-go/contexts/geography/domain"
	ginfra "github.com/jhermoso/karpo-fw-go/contexts/geography/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// TestGeographyContext loads the 32,500-row seed on every engine (batched inserts, INSERT ALL on
// Oracle) and walks it: search, ancestors, all of Spain (IN lists within SQL Server's parameter
// limit), postal codes, countries and IBAN.
func TestGeographyContext(t *testing.T) {
	for _, e := range engines {
		t.Run(e.name, func(t *testing.T) {
			db := open(t, e)
			ctx := context.Background()
			ginfra.DropAll(ctx, db)
			_, _ = db.ExecContext(ctx, "DELETE FROM "+sqlrepo.DefaultMigrationsTable+" WHERE context = '"+ginfra.Context+"'")
			m, err := ginfra.Migrator(db)
			if err != nil {
				t.Fatal(err)
			}
			if err := m.Verify(ctx); !errors.Is(err, application.ErrSchemaOutdated) {
				t.Fatalf("empty database: %v", err)
			}
			start := time.Now()
			if _, err := m.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			t.Logf("%s: schema and seed in %v", e.name, time.Since(start).Round(time.Millisecond))

			ac, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "reader", Kind: authz.Service,
				Permissions: []authz.Permission{gapp.PermBoundaryRead, gapp.PermReferenceRead}})
			ctx = authz.WithContext(ctx, ac)
			mod := geography.Compose(hotswap.New(db))
			svc, p := mod.Service, mod.Ports

			towns, err := svc.SearchBoundaries.Handle(ctx, gapp.SearchBoundaries{Text: "madrid", Type: gdomain.TypeMunicipality.String(), Size: 50})
			if err != nil {
				t.Fatal(err)
			}
			var madrid gapp.BoundaryDTO
			for _, b := range towns.Items {
				if b.Name == "Madrid" {
					madrid = b
				}
			}
			if madrid.INE != "28079" {
				t.Fatalf("Madrid: %+v", towns.Items)
			}
			detail, err := svc.GetBoundary.Handle(ctx, gapp.GetBoundary{ID: madrid.ID})
			if err != nil || len(detail.Ancestors) < 3 || detail.Ancestors[0].Name != "Madrid" {
				t.Fatalf("ancestors: %+v %v", detail.Ancestors, err)
			}
			es, err := svc.GetCountry.Handle(ctx, gapp.GetCountry{Alpha2: "ES"})
			if err != nil || es.Name != "Spain" || es.Currencies[0] != "EUR" || es.TimeZones[0] != "Europe/Madrid" {
				t.Fatalf("Spain: %+v %v", es, err)
			}
			all, err := p.Descendants(ctx, []string{es.Boundary})
			if err != nil || len(all) < 8000 {
				t.Fatalf("all of Spain: %d %v", len(all), err)
			}
			of, err := p.CountryOf(ctx, []string{madrid.ID})
			if err != nil || of[madrid.ID] != "ES" {
				t.Fatalf("country of Madrid: %v %v", of, err)
			}
			res, err := p.CheckPostalAddress(ctx, contracts.PostalAddressQuery{Country: "ES", PostalCode: "28013", Boundary: madrid.ID})
			if err != nil || res.PostalCodeID == "" {
				t.Fatalf("28013 in Madrid: %+v %v", res, err)
			}
			if _, err := p.CheckPostalAddress(ctx, contracts.PostalAddressQuery{Country: "ES", PostalCode: "08001", Boundary: madrid.ID}); !errors.Is(err, fw.ErrValidation) {
				t.Fatalf("08001 is not in Madrid: %v", err)
			}
			if err := p.CheckIBAN(ctx, "ES9121000418450200051332"); err != nil {
				t.Fatal(err)
			}
			countries, err := svc.ListCountries.Handle(ctx, gapp.ListCountries{})
			if err != nil || len(countries) != 248 {
				t.Fatalf("countries: %d %v", len(countries), err)
			}
			if err := m.Verify(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}
