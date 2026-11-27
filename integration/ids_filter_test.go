package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/shipments"
	sapp "github.com/jhermoso/karpo-fw-go/contexts/shipments/application"
	sinfra "github.com/jhermoso/karpo-fw-go/contexts/shipments/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// TestIDsFilterAtTheMaximum pide en cada motor una lista con ámbito (los transportistas) por
// exactamente 1000 identidades, el tope del filtro ids (B-15, docs/CONTRATO-MANTENIMIENTO.md):
// un solo IN del máximo de Oracle, los parámetros de SQL Server con el ámbito y otro filtro, y la
// comparación de la identidad. Salen sólo los pedidos del ámbito; con 1001 es un error de validación.
func TestIDsFilterAtTheMaximum(t *testing.T) {
	for _, e := range engines {
		if e.name == "oracle-dotnet-guids" {
			continue
		}
		t.Run(e.name, func(t *testing.T) {
			db := open(t, e)
			ctx := context.Background()
			sinfra.DropAll(ctx, db)
			m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{sinfra.Migrations()})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := m.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			svc := shipments.Compose(hotswap.New(db)).Service
			admin, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "admin", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
			admin.GlobalAdmin = true
			actx := authz.WithContext(ctx, admin)
			acme, globex := fw.NewUUID(), fw.NewUUID()
			register := func(company fw.UUID, code string) string {
				c, err := svc.RegisterCarrier.Handle(actx, sapp.RegisterCarrier{Company: company.String(), Code: code, Name: code})
				if err != nil {
					t.Fatal(err)
				}
				return c.ID
			}
			seur, mrw, foreign := register(acme, "SEUR"), register(acme, "MRW"), register(globex, "GLS")
			clerk, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "clerk", Kind: authz.Service,
				Permissions: []authz.Permission{sapp.PermCarrierRead}, Grants: []authz.Grant{{OrganizationID: acme, Level: authz.Full}}})
			cctx := authz.WithContext(ctx, clerk)

			ids := []string{seur, mrw, foreign}
			for len(ids) < fw.MaxPageSize {
				ids = append(ids, fw.NewUUID().String())
			}
			page, err := svc.PageCarriers.Handle(cctx, sapp.PageCarriers{IDs: ids, ByIDs: true, ActiveOnly: true, Page: 1, Size: fw.MaxPageSize})
			if err != nil {
				t.Fatal(err)
			}
			got := map[string]bool{}
			for _, c := range page.Items {
				got[c.ID] = true
			}
			if page.Total != 2 || len(got) != 2 || !got[seur] || !got[mrw] {
				t.Fatalf("1000 ids: %+v", page)
			}
			_, err = svc.PageCarriers.Handle(cctx, sapp.PageCarriers{IDs: append(ids, fw.NewUUID().String()), ByIDs: true, Page: 1, Size: 10})
			if !errors.Is(err, fw.ErrValidation) {
				t.Fatalf("1001 ids: %v", err)
			}
		})
	}
}
