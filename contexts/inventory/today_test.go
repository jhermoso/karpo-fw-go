package inventory_test

import (
	"context"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/inventory"
	iapp "github.com/jhermoso/karpo-fw-go/contexts/inventory/application"
	idomain "github.com/jhermoso/karpo-fw-go/contexts/inventory/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/time/fake"
)

// zones hace de anfitrión: la zona de cada empresa; la que no está, UTC.
type zones map[string]*time.Location

func (z zones) DateOf(_ context.Context, company fw.UUID, at time.Time) (vocab.Date, error) {
	if loc := z[company.String()]; loc != nil {
		return vocab.DateOf(at.In(loc)), nil
	}
	return vocab.DateOf(at.UTC()), nil
}

// catalog hace de Products: cada producto es de una empresa y se almacena.
type catalog map[idomain.ProductID]idomain.OrganizationID

func (c catalog) Items(_ context.Context, ps []idomain.ProductID) (map[idomain.ProductID]idomain.Item, error) {
	out := map[idomain.ProductID]idomain.Item{}
	for _, p := range ps {
		if company, ok := c[p]; ok {
			out[p] = idomain.Item{Company: company, SKU: "SKU", Name: "Tornillo", Stocked: true}
		}
	}
	return out, nil
}

// Un traspaso sin fecha es del día de la empresa del almacén de origen: el 14 de octubre a las
// 22:30 UTC ya es el 15 en Madrid y aún el 14 en Canarias.
func TestInventory_TransferOnTheCompanysDay(t *testing.T) {
	defer fw.SetClock(fake.New(time.Date(2026, 10, 14, 22, 30, 0, 0, time.UTC)))()
	madrid, _ := time.LoadLocation("Europe/Madrid")
	canary, _ := time.LoadLocation("Atlantic/Canary")
	inMadrid, inCanaries := fw.NewUUID().String(), fw.NewUUID().String()
	cat := catalog{}
	sw := hotswap.New(memory.NewStore("memory"))
	t.Cleanup(func() { _ = sw.Close(context.Background()) })
	m := inventory.Compose(sw, cat, inventory.WithToday(zones{inMadrid: madrid, inCanaries: canary}))
	ac, err := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "setup", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
	if err != nil {
		t.Fatal(err)
	}
	ac.GlobalAdmin = true
	ctx := authz.WithContext(context.Background(), ac)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	for company, want := range map[string]string{inMadrid: "2026-10-15", inCanaries: "2026-10-14"} {
		product := idomain.ProductID{UUID: fw.NewUUID()}
		cat[product] = idomain.OrganizationID{UUID: fw.MustParseUUID(company)}
		from, err := m.Service.CreateWarehouse.Handle(ctx, iapp.CreateWarehouse{Company: company, Code: "A", Name: "Origen"})
		must(err)
		to, err := m.Service.CreateWarehouse.Handle(ctx, iapp.CreateWarehouse{Company: company, Code: "B", Name: "Destino"})
		must(err)
		_, err = m.Service.Receive.Handle(ctx, iapp.Receive{Warehouse: from.ID, Product: product.String(), Quantity: "10", UnitCost: "1",
			Date: vocab.MustDate(2026, 10, 1)})
		must(err)
		ms, err := m.Service.Transfer.Handle(ctx, iapp.Transfer{From: from.ID, To: to.ID, Product: product.String(), Quantity: "4"})
		if err != nil || len(ms) != 2 || ms[0].Date != want || ms[1].Date != want {
			t.Fatalf("transfer of %s: %+v %v, want %s", company, ms, err, want)
		}
	}
}
