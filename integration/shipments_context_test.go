package integration

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	orders "github.com/jhermoso/karpo-fw-go/contexts/orders/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/shipments"
	sapp "github.com/jhermoso/karpo-fw-go/contexts/shipments/application"
	sdomain "github.com/jhermoso/karpo-fw-go/contexts/shipments/domain"
	sinfra "github.com/jhermoso/karpo-fw-go/contexts/shipments/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/messaging/inprocess"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// TestShipmentsContext runs Shipments on every engine, fed with the delivery note of Orders as
// its contract declares it: the shipment planned once through the SQL inbox, the round trip of
// the plan with its optional columns, lines and history in order, carriers (unique code, blocked),
// dispatch, tracking in transit, delivery, a shipment by hand returned, the searches and the
// Published Language in the outbox.
func TestShipmentsContext(t *testing.T) {
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
			sw := hotswap.New(db)
			sm := shipments.Compose(sw)
			broker := inprocess.NewBroker()
			broker.Subscribe("shipments", sm.Consumer)
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			violates := func(err error, what string) {
				t.Helper()
				if !errors.Is(err, fw.ErrRuleViolation) {
					t.Fatalf("%s: %v", what, err)
				}
			}
			admin, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "admin", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
			admin.GlobalAdmin = true
			actx := authz.WithContext(ctx, admin)
			svc := sm.Service
			id := func() string { return fw.NewUUID().String() }
			day := func(d int) vocab.Date { return vocab.MustDate(2026, 5, d) }
			acme, customer, delivery := id(), id(), id()

			note := orders.DeliveryIssuedV1{DeliveryID: delivery, OrderID: id(), OrderNumber: "PED-2026-000001", Company: acme, Customer: customer,
				Number: "ALB-2026-000001", Date: "2026-05-06", Total: "242.00", Lines: []orders.DeliveryLine{
					{Line: 1, SKU: "TOR-01", Description: "Tornillos", UoM: "ud", Stocked: true, Quantity: "100"},
					{Line: 2, SKU: "MON-01", Description: "Montaje", UoM: "h", Stocked: false, Quantity: "2"},
					{Line: 3, SKU: "TUE-01", Description: "Tuercas", UoM: "ud", Stocked: true, Quantity: "50.5"}}}
			raw, err := json.Marshal(note)
			must(err)
			for _, msg := range []string{"a", "a", "b"} { // the same message twice, and the same note in another message
				must(broker.Send(ctx, application.Envelope{ID: delivery + msg, Type: note.IntegrationEventType(), Source: "orders",
					OccurredAt: time.Date(2026, 5, 6, 10, 0, 0, 0, time.UTC), Data: raw}))
			}
			planned, err := svc.SearchShipments.Handle(actx, sapp.SearchShipments{Company: acme, Delivery: delivery})
			if err != nil || len(planned.Items) != 1 || planned.Items[0].SourceRef != "ALB-2026-000001" || planned.Items[0].Status != "scheduled" {
				t.Fatalf("planned once: %+v %v", planned.Items, err)
			}
			sid, _ := sdomain.ParseShipmentID(planned.Items[0].ID)

			seur, err := svc.RegisterCarrier.Handle(actx, sapp.RegisterCarrier{Company: acme, Code: "seur", Name: "SEUR",
				TrackingURL: "https://seur.example/track?n={tracking}"})
			must(err)
			_, err = svc.RegisterCarrier.Handle(actx, sapp.RegisterCarrier{Company: acme, Code: "SEUR", Name: "Otro"})
			violates(err, "code used once")
			old, err := svc.RegisterCarrier.Handle(actx, sapp.RegisterCarrier{Company: acme, Code: "OLD", Name: "Transportes Viejos", Party: customer})
			must(err)
			oid, _ := sdomain.ParseCarrierID(old.ID)
			old, err = svc.ChangeCarrier.Handle(actx, sapp.ChangeCarrier{ID: oid, Name: "Transportes Viejos", Party: customer, Blocked: true})
			if err != nil || !old.Blocked || old.Version != 2 || old.Party != customer || old.TrackingURL != "" {
				t.Fatalf("carrier round trip: %+v %v", old, err)
			}

			_, err = svc.Move.Handle(actx, sapp.MoveShipment{ID: sid, To: "in-transit", On: day(7)})
			violates(err, "nothing decided yet")
			plan := sapp.PlanInput{Method: "courier", Carrier: seur.ID, Recipient: "Talleres Vega, att. Marta", Destination: "Calle Mayor 1, 28013 Madrid",
				Packages: 2, WeightKg: "12.5", Cost: "18.90", Tracking: "AB 123/9", EstimatedShip: day(7), EstimatedArrival: day(8)}
			blocked := plan
			blocked.Carrier = old.ID
			_, err = svc.Change.Handle(actx, sapp.ChangeShipment{ID: sid, PlanInput: blocked})
			violates(err, "blocked carrier")
			_, err = svc.Change.Handle(actx, sapp.ChangeShipment{ID: sid, PlanInput: plan})
			must(err)
			_, err = svc.Move.Handle(actx, sapp.MoveShipment{ID: sid, To: "in-transit", On: day(7)})
			must(err)
			tracked := plan
			tracked.Tracking = "AB124"
			_, err = svc.Change.Handle(actx, sapp.ChangeShipment{ID: sid, PlanInput: tracked})
			must(err)
			_, err = svc.Move.Handle(actx, sapp.MoveShipment{ID: sid, To: "delivered", On: day(8), ReceivedBy: "Marta"})
			must(err)

			got, err := svc.GetShipment.Handle(actx, sapp.GetShipment{ID: sid})
			if err != nil || got.Customer != customer || got.SourceType != "orders.delivery" || got.SourceID != delivery || got.Method != "courier" ||
				got.Carrier != seur.ID || got.CarrierName != "SEUR" || got.Tracking != "AB124" || got.TrackingLink != "https://seur.example/track?n=AB124" ||
				got.Recipient != "Talleres Vega, att. Marta" || got.Destination != "Calle Mayor 1, 28013 Madrid" || got.Packages != 2 ||
				got.WeightKg != "12.500" || got.Cost != "18.90" || got.EstimatedShip != "2026-05-07" || got.EstimatedArrival != "2026-05-08" ||
				got.Status != "delivered" || got.Dispatched != "2026-05-07" || got.Closed != "2026-05-08" || got.ReceivedBy != "Marta" || got.Reason != "" ||
				len(got.Lines) != 2 || got.Lines[0].SKU != "TOR-01" || got.Lines[1].Quantity != "50.5" || got.Lines[1].UoM != "ud" || len(got.History) != 3 ||
				got.History[0].Status != "scheduled" || got.History[0].On != "2026-05-06" || got.History[2].Status != "delivered" {
				t.Fatalf("shipment round trip: %+v %v", got, err)
			}

			samples, err := svc.Schedule.Handle(actx, sapp.ScheduleShipment{Company: acme, Customer: customer, On: day(10),
				PlanInput: sapp.PlanInput{Method: "pickup", Packages: 1}, Lines: []sapp.LineInput{{Description: "Muestras", Quantity: "3"}}})
			if err != nil || samples.SourceID != "" || samples.Carrier != "" || samples.EstimatedShip != "" || samples.Lines[0].SKU != "" {
				t.Fatalf("by hand: %+v %v", samples, err)
			}
			xid, _ := sdomain.ParseShipmentID(samples.ID)
			_, err = svc.Move.Handle(actx, sapp.MoveShipment{ID: xid, To: "in-transit", On: day(11)})
			must(err)
			back, err := svc.Move.Handle(actx, sapp.MoveShipment{ID: xid, To: "returned", On: day(12), Reason: "No las quiere"})
			if err != nil || back.Status != "returned" || back.Reason != "No las quiere" || back.Closed != "2026-05-12" {
				t.Fatalf("returned: %+v %v", back, err)
			}

			for query, want := range map[sapp.SearchShipments]int{
				{Company: acme}:                      2,
				{Company: acme, Status: "delivered"}: 1,
				{Company: acme, Tracking: "ab1"}:     1,
				{Company: acme, Carrier: seur.ID}:    1,
				{Company: acme, Customer: customer}:  2,
			} {
				if p, err := svc.SearchShipments.Handle(actx, query); err != nil || len(p.Items) != want {
					t.Fatalf("search %+v: %d shipments, want %d (%v)", query, len(p.Items), want, err)
				}
			}
			all, err := svc.SearchShipments.Handle(actx, sapp.SearchShipments{Company: acme})
			if err != nil || all.Items[0].ID != got.ID {
				t.Fatalf("by the day they were planned: %+v %v", all.Items, err)
			}
			cs, err := svc.SearchCarriers.Handle(actx, sapp.SearchCarriers{Company: acme})
			if err != nil || len(cs) != 2 || cs[0].Code != "OLD" || cs[1].Code != "SEUR" {
				t.Fatalf("carriers: %+v %v", cs, err)
			}
			// Two dispatches, a delivery and a return.
			if n, err := sm.Relay(inprocess.NewBroker()).RelayOnce(ctx); err != nil || n != 4 {
				t.Fatalf("published: %d %v", n, err)
			}
		})
	}
}
