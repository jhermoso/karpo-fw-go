package integration

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/orders"
	oapp "github.com/jhermoso/karpo-fw-go/contexts/orders/application"
	odomain "github.com/jhermoso/karpo-fw-go/contexts/orders/domain"
	oinfra "github.com/jhermoso/karpo-fw-go/contexts/orders/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/messaging/inprocess"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/time/fake"
)

// quotedItems plays Products: what each product is and costs.
type quotedItems map[odomain.ProductID]odomain.Item

func (q quotedItems) Price(_ context.Context, _ odomain.OrganizationID, p odomain.ProductID, _ odomain.PriceListID, _ vocab.Decimal, _ vocab.Date) (odomain.Item, bool, error) {
	it, ok := q[p]
	return it, ok, nil
}

// TestQuotesContext runs the quotes of Orders on every engine: a quote with its lines, dates and
// decimals as each engine gives them back, the number taken when it is sent (and not when sending
// fails), the order an accepted quote becomes, in one unit of work with it, rejection, withdrawal
// of a draft that never had a number, expiry on a clock moved forward and the searches.
func TestQuotesContext(t *testing.T) {
	for _, e := range engines {
		if e.name == "oracle-dotnet-guids" {
			continue
		}
		t.Run(e.name, func(t *testing.T) {
			db := open(t, e)
			ctx := context.Background()
			oinfra.DropAll(ctx, db)
			m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{oinfra.Migrations()})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := m.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			dec := func(s string) vocab.Decimal { d, _ := vocab.ParseDecimal(s); return d }
			acme := fw.NewUUID().String()
			company := odomain.OrganizationID{UUID: fw.MustParseUUID(acme)}
			widget, service := odomain.ProductID{UUID: fw.NewUUID()}, odomain.ProductID{UUID: fw.NewUUID()}
			om := orders.Compose(hotswap.New(db), quotedItems{
				widget:  {Company: company, SKU: "W-1", Name: "Tornillo de cabeza avellanada", UoM: "ud", TaxCode: "G", Stocked: true, Sellable: true, UnitPrice: dec("10.1234"), Discount: dec("10")},
				service: {Company: company, SKU: "S-1", Name: "Instalación", UoM: "h", TaxCode: "G", Sellable: true, UnitPrice: dec("100"), Discount: dec("0")},
			}, nil)
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
			svc := om.Service
			customer, other, warehouse := fw.NewUUID().String(), fw.NewUUID().String(), fw.NewUUID().String()
			today := vocab.DateOf(fw.Now())
			id := func(q oapp.QuoteDTO) odomain.QuoteID { x, _ := odomain.ParseQuoteID(q.ID); return x }
			number := func(n int) string { return fmt.Sprintf("PRE-%d-%06d", today.Year(), n) }

			_, err = svc.SetTerms.Handle(actx, oapp.SetTerms{Company: acme, Customer: customer, Discount: "5"})
			must(err)
			q, err := svc.DraftQuote.Handle(actx, oapp.DraftQuote{Company: acme, Customer: customer, Reference: "Obra 12", Notes: "Entrega en obra, mañana"})
			must(err)
			_, err = svc.AddQuoteLine.Handle(actx, oapp.AddQuoteLine{ID: id(q), Product: widget.String(), Quantity: "2.5"})
			must(err)
			_, err = svc.AddQuoteLine.Handle(actx, oapp.AddQuoteLine{ID: id(q), Product: service.String(), Quantity: "1"})
			must(err)
			_, err = svc.RemoveQuoteLine.Handle(actx, oapp.RemoveQuoteLine{ID: id(q), Line: 2})
			must(err)
			_, err = svc.AddQuoteLine.Handle(actx, oapp.AddQuoteLine{ID: id(q), Product: service.String(), Quantity: "2"})
			must(err)
			q, err = svc.ChangeQuote.Handle(actx, oapp.ChangeQuote{ID: id(q), ValidUntil: today.AddDays(10), Reference: "Obra 12-B", Notes: "Entrega en obra, mañana"})
			must(err)
			// 10.1234 − 10 % − 5 % = 8.6555 (four decimals); × 2.5 = 21.64.
			got, err := svc.GetQuote.Handle(actx, oapp.GetQuote{ID: id(q)})
			must(err)
			if got.Status != "draft" || got.Number != "" || got.Date != today.String() || got.ValidUntil != today.AddDays(10).String() || got.CustomerDiscount != "5.00" ||
				got.Reference != "Obra 12-B" || got.Notes != "Entrega en obra, mañana" || got.Order != "" || got.Version != 6 || len(got.Lines) != 2 ||
				got.Lines[0] != (oapp.QuoteLineDTO{No: 1, Product: widget.String(), SKU: "W-1", Description: "Tornillo de cabeza avellanada", UoM: "ud", TaxCode: "G",
					Stocked: true, Quantity: "2.5", UnitPrice: "10.1234", Discount: "10.00", NetPrice: "8.6555", Amount: "21.64"}) ||
				got.Lines[1].No != 2 || got.Lines[1].Stocked || got.Lines[1].Amount != "190.00" || got.Total != "211.64" {
				t.Fatalf("stored draft: %+v", got)
			}

			sent, err := svc.SendQuote.Handle(actx, oapp.SendQuote{ID: id(q)})
			must(err)
			if sent.Status != "sent" || sent.Number != number(1) {
				t.Fatalf("sent: %+v", sent)
			}
			_, err = svc.SendQuote.Handle(actx, oapp.SendQuote{ID: id(q)})
			violates(err, "orders.quote_not_draft")

			// Accepting writes the quote and its order together.
			acc, err := svc.AcceptQuote.Handle(actx, oapp.AcceptQuote{ID: id(q), Warehouse: warehouse})
			must(err)
			oid, _ := odomain.ParseOrderID(acc.Order.ID)
			order, err := svc.GetOrder.Handle(actx, oapp.GetOrder{ID: oid})
			must(err)
			if acc.Quote.Status != "accepted" || acc.Quote.Order != order.ID || order.Status != "draft" || order.Customer != customer || order.Warehouse != warehouse ||
				order.CustomerDiscount != "5.00" || order.Reference != "Obra 12-B" || order.Total != "211.64" || len(order.Lines) != 2 ||
				order.Lines[0].NetPrice != "8.6555" || order.Lines[0].Quantity != "2.5" || !order.Lines[0].Stocked || order.Lines[1].Amount != "190.00" {
				t.Fatalf("accepted: %+v %+v", acc.Quote, order)
			}
			_, err = svc.AcceptQuote.Handle(actx, oapp.AcceptQuote{ID: id(q)})
			violates(err, "orders.quote_not_sent")
			confirmed, err := svc.Confirm.Handle(actx, oapp.ConfirmOrder{ID: oid})
			if err != nil || confirmed.Status != "confirmed" || confirmed.Number == "" {
				t.Fatalf("confirm: %+v %v", confirmed, err)
			}

			send := func(c oapp.DraftQuote) oapp.QuoteDTO {
				t.Helper()
				x, err := svc.DraftQuote.Handle(actx, c)
				must(err)
				_, err = svc.AddQuoteLine.Handle(actx, oapp.AddQuoteLine{ID: id(x), Product: service.String(), Quantity: "1"})
				must(err)
				x, err = svc.SendQuote.Handle(actx, oapp.SendQuote{ID: id(x)})
				must(err)
				return x
			}
			no := send(oapp.DraftQuote{Company: acme, Customer: other})
			no, err = svc.RejectQuote.Handle(actx, oapp.EndQuote{ID: id(no), Reason: "Demasiado caro, señor"})
			if err != nil || no.Status != "rejected" || no.Reason != "Demasiado caro, señor" || no.Number != number(2) || no.CustomerDiscount != "0.00" {
				t.Fatalf("rejected: %+v %v", no, err)
			}

			// A draft withdrawn never had a number, and is read back as such.
			gone, err := svc.DraftQuote.Handle(actx, oapp.DraftQuote{Company: acme, Customer: customer})
			must(err)
			_, err = svc.SendQuote.Handle(actx, oapp.SendQuote{ID: id(gone)})
			violates(err, "orders.quote_empty")
			_, err = svc.WithdrawQuote.Handle(actx, oapp.EndQuote{ID: id(gone)})
			must(err)
			gone, err = svc.GetQuote.Handle(actx, oapp.GetQuote{ID: id(gone)})
			if err != nil || gone.Status != "withdrawn" || gone.Number != "" || gone.Reason != "" {
				t.Fatalf("withdrawn draft: %+v %v", gone, err)
			}

			// A day later: what held until yesterday is not accepted nor sent, and the job closes it.
			short := send(oapp.DraftQuote{Company: acme, Customer: customer, ValidUntil: today})
			long := send(oapp.DraftQuote{Company: acme, Customer: customer, ValidUntil: today.AddDays(1)})
			stale, err := svc.DraftQuote.Handle(actx, oapp.DraftQuote{Company: acme, Customer: customer, ValidUntil: today})
			must(err)
			_, err = svc.AddQuoteLine.Handle(actx, oapp.AddQuoteLine{ID: id(stale), Product: service.String(), Quantity: "1"})
			must(err)
			func() {
				defer fw.SetClock(fake.New(fw.Now().Add(24 * time.Hour)))()
				_, err := svc.AcceptQuote.Handle(actx, oapp.AcceptQuote{ID: id(short)})
				violates(err, "orders.quote_expired")
				_, err = svc.SendQuote.Handle(actx, oapp.SendQuote{ID: id(stale)})
				violates(err, "orders.quote_expired")
				out, err := svc.ExpireQuotes.Handle(actx, oapp.ExpireQuotes{Company: acme})
				if err != nil || !slices.Equal(out.Numbers, []string{short.Number}) {
					t.Fatalf("expire: %+v %v", out, err)
				}
				if again, err := svc.ExpireQuotes.Handle(actx, oapp.ExpireQuotes{Company: acme}); err != nil || len(again.Numbers) != 0 {
					t.Fatalf("expire again: %+v %v", again, err)
				}
			}()
			// The number the failed sending took went back with it.
			_, err = svc.ChangeQuote.Handle(actx, oapp.ChangeQuote{ID: id(stale), ValidUntil: today.AddDays(5)})
			must(err)
			stale, err = svc.SendQuote.Handle(actx, oapp.SendQuote{ID: id(stale)})
			if err != nil || stale.Number != number(5) || short.Number != number(3) || long.Number != number(4) {
				t.Fatalf("numbers without gaps: %+v %v", stale, err)
			}

			byStatus := func(st string) fw.Page[oapp.QuoteDTO] {
				t.Helper()
				p, err := svc.SearchQuotes.Handle(actx, oapp.SearchQuotes{Company: acme, Status: st})
				must(err)
				return p
			}
			if p := byStatus("sent"); p.Total != 2 || p.Items[0].Number != number(5) || p.Items[1].Number != number(4) {
				t.Fatalf("sent, the latest number first: %+v", p.Items)
			}
			if byStatus("expired").Total != 1 || byStatus("accepted").Total != 1 || byStatus("withdrawn").Total != 1 || byStatus("draft").Total != 0 || byStatus("").Total != 6 {
				t.Fatal("by status")
			}
			if p, err := svc.SearchQuotes.Handle(actx, oapp.SearchQuotes{Customer: other}); err != nil || p.Total != 1 || p.Items[0].ID != no.ID {
				t.Fatalf("by customer: %+v %v", p, err)
			}
			// Five sent; accepted, rejected and expired; the order confirmed and its stock asked for.
			if n, err := om.Relay(inprocess.NewBroker()).RelayOnce(ctx); err != nil || n != 10 {
				t.Fatalf("published: %d %v", n, err)
			}
		})
	}
}
