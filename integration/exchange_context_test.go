package integration

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/exchange"
	xapp "github.com/jhermoso/karpo-fw-go/contexts/exchange/application"
	xdomain "github.com/jhermoso/karpo-fw-go/contexts/exchange/domain"
	xinfra "github.com/jhermoso/karpo-fw-go/contexts/exchange/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/messaging/inprocess"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/time/fake"
)

// promoCodes plays Parties: the promotion codes of the collaborators.
type promoCodes map[string]xdomain.PartyID

func (c promoCodes) ByPromotionCode(_ context.Context, _ xdomain.OrganizationID, code string) (xdomain.PartyID, bool, error) {
	p, ok := c[strings.ToUpper(code)]
	return p, ok, nil
}

// TestExchangeContext runs Exchange on every engine: currencies, rates, margins and settings round
// trip (upserts by natural key), the quote with the numbers of the C# tests, a reservation priced
// by the server with its lines, history and instants as each engine gives them back, the
// lifecycle, cancellation, expiry on a clock moved forward, the searches (by a currency of the
// lines, by days) and the dashboard.
func TestExchangeContext(t *testing.T) {
	for _, e := range engines {
		if e.name == "oracle-dotnet-guids" {
			continue
		}
		t.Run(e.name, func(t *testing.T) {
			db := open(t, e)
			ctx := context.Background()
			xinfra.DropAll(ctx, db)
			m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{xinfra.Migrations()})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := m.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			sw := hotswap.New(db)
			partner := xdomain.PartyID{UUID: fw.NewUUID()}
			xm := exchange.Compose(sw, promoCodes{"PROMO1": partner})
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			violates := func(err error, code string) {
				t.Helper()
				var rv *fw.RuleViolationError
				if !errors.As(err, &rv) || rv.Code != code {
					t.Fatalf("want %s: %v", code, err)
				}
			}
			admin, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "admin", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
			admin.GlobalAdmin = true
			actx := authz.WithContext(ctx, admin)
			svc := xm.Service
			acme, customer, office := fw.NewUUID().String(), fw.NewUUID().String(), fw.NewUUID().String()
			now := fw.Now().UTC()
			pickup := now.Add(48 * time.Hour).Truncate(time.Second)
			today := vocab.DateOf(now)

			for _, c := range []xapp.SetCurrency{{Company: acme, Code: "usd", Name: "Dólar", Facial: "5"}, {Company: acme, Code: "GBP", Name: "Libra", Facial: "5"},
				{Company: acme, Code: "CHF", Name: "Franco", Facial: "10"}} {
				_, err := svc.SetCurrency.Handle(actx, c)
				must(err)
			}
			chf, err := svc.SetCurrency.Handle(actx, xapp.SetCurrency{Company: acme, Code: "chf", Name: "Franco suizo", Facial: "10", Blocked: true})
			if err != nil || chf.Version != 2 || chf.Name != "Franco suizo" || !chf.Blocked || chf.Rate != "" {
				t.Fatalf("currency upsert: %+v %v", chf, err)
			}
			_, err = svc.SetRate.Handle(actx, xapp.SetRate{Company: acme, Code: "USD", Rate: "0.92"})
			must(err)
			_, err = svc.SetRate.Handle(actx, xapp.SetRate{Company: acme, Code: "GBP", Rate: "1.17"})
			must(err)
			_, err = svc.SetMargin.Handle(actx, xapp.SetMargin{Company: acme, Currency: "USD", Kind: "percent", Level1: "1", Level2: "1", Level3: "1"})
			must(err)
			mg, err := svc.SetMargin.Handle(actx, xapp.SetMargin{Company: acme, Currency: "usd", Segment: "web", Kind: "percent", Level1: "1.5", Level2: "2", Level3: "2.5"})
			if err != nil || mg.Version != 2 || mg.Level3 != "2.5" || mg.Segment != "WEB" {
				t.Fatalf("margin upsert: %+v %v", mg, err)
			}
			if set, err := svc.GetSettings.Handle(actx, xapp.GetSettings{Company: acme}); err != nil || !set.Defaults || set.Level != 2 {
				t.Fatalf("default settings: %+v %v", set, err)
			}
			set, err := svc.SetSettings.Handle(actx, xapp.SetSettings{Company: acme, Level: 3, PromotionMode: "required", ValidatePromotion: true, ExpiryHours: 6})
			if err != nil || set.Defaults || set.Level != 3 || set.ExpiryHours != 6 {
				t.Fatalf("settings: %+v %v", set, err)
			}
			q, err := svc.Quote.Handle(actx, xapp.GetQuote{Company: acme, Currency: "USD", Amount: "500"})
			if err != nil || q.OfferedRate != "0.943000" || q.Eur != "471.50" || q.Level != 3 {
				t.Fatalf("third level: %+v %v", q, err)
			}
			set, err = svc.SetSettings.Handle(actx, xapp.SetSettings{Company: acme, Level: 2, PromotionMode: "optional", ValidatePromotion: true, ExpiryHours: 4})
			if err != nil || set.Version != 2 {
				t.Fatalf("settings again: %+v %v", set, err)
			}
			q, err = svc.Quote.Handle(actx, xapp.GetQuote{Company: acme, Currency: "USD", Amount: "123"})
			if err != nil || q.OfferedRate != "0.938400" || q.Delivered != "120.00" || q.Eur != "112.61" || q.RateOn != today.String() {
				t.Fatalf("whole notes: %+v %v", q, err)
			}
			_, err = svc.Quote.Handle(actx, xapp.GetQuote{Company: acme, Currency: "CHF", Amount: "100"})
			violates(err, "exchange.currency_blocked")

			reserve := func(code string, lines ...xapp.ReserveLine) (xapp.ReservationDTO, error) {
				return svc.Reserve.Handle(actx, xapp.Reserve{Company: acme, Customer: customer, Channel: "phone", Pickup: pickup, TimeSlot: "tarde",
					Facility: office, PromotionCode: code, Lines: lines})
			}
			_, err = reserve("nope", xapp.ReserveLine{Currency: "USD", Amount: "500"})
			violates(err, "exchange.promotion_unknown")
			res, err := reserve("promo1", xapp.ReserveLine{Currency: "USD", Amount: "503"},
				xapp.ReserveLine{Currency: "GBP", Amount: "300", Collector: true, CollectorNote: "Moneda de 5 libras de 1990"})
			must(err)
			rid, _ := xdomain.ParseReservationID(res.ID)
			for _, to := range []string{"email-verified", "notified"} {
				_, err := svc.Advance.Handle(actx, xapp.AdvanceReservation{ID: rid, To: to})
				must(err)
			}
			got, err := svc.Get.Handle(actx, xapp.GetReservation{Reference: strings.ToLower(res.Reference)})
			if err != nil || got.ID != res.ID || got.Customer != customer || got.Channel != "phone" || got.Segment != "WEB" || got.TimeSlot != "tarde" ||
				got.Pickup != pickup.Format(time.RFC3339) || got.ExpiresAt != pickup.Add(4*time.Hour).Format(time.RFC3339) || got.Facility != office ||
				got.PromotionCode != "promo1" || got.Collaborator != partner.String() || got.Status != "notified" || got.Total != "820.20" ||
				len(got.Lines) != 2 || got.Lines[0].Currency != "USD" || got.Lines[0].Requested != "503.00" || got.Lines[0].Delivered != "500.00" ||
				got.Lines[0].BaseRate != "0.920000" || got.Lines[0].OfferedRate != "0.938400" || got.Lines[0].MarginValue != "2" || got.Lines[0].Eur != "469.20" ||
				!got.Lines[1].Collector || got.Lines[1].CollectorNote != "Moneda de 5 libras de 1990" || got.Lines[1].Eur != "351.00" ||
				len(got.History) != 3 || got.History[0].Status != "registered" || got.History[2].Status != "notified" || got.History[0].At == "" {
				t.Fatalf("reservation round trip: %+v %v", got, err)
			}
			done, err := svc.Advance.Handle(actx, xapp.AdvanceReservation{ID: rid, To: "completed"})
			if err != nil || done.Status != "completed" {
				t.Fatalf("collected: %+v %v", done, err)
			}
			if again, err := svc.Advance.Handle(actx, xapp.AdvanceReservation{ID: rid, To: "completed"}); err != nil || again.Version != done.Version {
				t.Fatalf("collected again: %+v %v", again, err)
			}

			gone, err := reserve("", xapp.ReserveLine{Currency: "USD", Amount: "100"})
			must(err)
			gid, _ := xdomain.ParseReservationID(gone.ID)
			c, err := svc.Cancel.Handle(actx, xapp.CancelReservation{ID: gid, Reason: "El cliente desiste"})
			if err != nil || c.Status != "cancelled" || c.Reason != "El cliente desiste" {
				t.Fatalf("cancel: %+v %v", c, err)
			}
			late, err := reserve("", xapp.ReserveLine{Currency: "USD", Amount: "200"})
			must(err)
			func() {
				defer fw.SetClock(fake.New(pickup.Add(5 * time.Hour)))()
				expired, err := svc.ExpireDue.Handle(actx, xapp.ExpireDue{Company: acme})
				if err != nil || expired.Expired != 1 || !slices.Equal(expired.References, []string{late.Reference}) {
					t.Fatalf("expire: %+v %v", expired, err)
				}
			}()
			lid, _ := xdomain.ParseReservationID(late.ID)
			_, err = svc.Advance.Handle(actx, xapp.AdvanceReservation{ID: lid, To: "email-verified"})
			violates(err, "exchange.transition")

			for query, want := range map[xapp.SearchReservations]int{
				{Company: acme}:                                                         3,
				{Company: acme, Status: "expired"}:                                      1,
				{Company: acme, Currency: "gbp"}:                                        1,
				{Company: acme, Customer: customer}:                                     3,
				{Company: acme, Facility: office}:                                       3,
				{Company: acme, CreatedFrom: today.String(), CreatedTo: today.String()}: 3,
				{Company: acme, PickupTo: today.String()}:                               0,
			} {
				if p, err := svc.Search.Handle(actx, query); err != nil || len(p.Items) != want {
					t.Fatalf("search %+v: %d reservations, want %d (%v)", query, len(p.Items), want, err)
				}
			}
			dash, err := svc.Dashboard.Handle(actx, xapp.GetDashboard{Company: acme, From: today.AddDays(-1).String(), To: today.AddDays(1).String()})
			if err != nil || dash.Reservations != 1 || dash.Eur != "820.20" || dash.ByStatus["cancelled"] != 1 || dash.ByStatus["expired"] != 1 ||
				len(dash.ByCurrency) != 2 || dash.ByCurrency[0].Currency != "USD" || dash.ByCurrency[0].Amount != "500.00" || dash.ByFacility[0].Facility != office {
				t.Fatalf("dashboard: %+v %v", dash, err)
			}
			// Three registrations, three steps, a cancellation and an expiry.
			if n, err := xm.Relay(inprocess.NewBroker()).RelayOnce(ctx); err != nil || n != 8 {
				t.Fatalf("published: %d %v", n, err)
			}
		})
	}
}
