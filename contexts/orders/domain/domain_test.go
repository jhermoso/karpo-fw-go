package domain_test

import (
	"errors"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/orders/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

func isViolation(err error, code string) bool {
	var rv *fw.RuleViolationError
	return errors.As(err, &rv) && rv.Code == code
}

func dec(s string) vocab.Decimal { return vocab.MustDecimal(s) }

func date(s string) vocab.Date {
	d, err := vocab.ParseDate(s)
	if err != nil {
		panic(err)
	}
	return d
}

var (
	company   = domain.OrganizationID{UUID: fw.NewUUID()}
	customer  = domain.PartyID{UUID: fw.NewUUID()}
	warehouse = domain.WarehouseID{UUID: fw.NewUUID()}
)

func draft(t *testing.T, discount string) *domain.Order {
	t.Helper()
	o, err := domain.DraftOrder(domain.NewOrderID(), domain.OrderState{Company: company, Customer: customer, Date: date("2026-10-04"), Warehouse: warehouse,
		CustomerDiscount: dec(discount)})
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func line(qty, price, discount string, stocked bool) domain.Line {
	return domain.Line{Product: domain.ProductID{UUID: fw.NewUUID()}, SKU: "X", Description: "X", UoM: "ea", TaxCode: "G21", Stocked: stocked,
		Quantity: dec(qty), UnitPrice: dec(price), Discount: dec(discount)}
}

// events devuelve los eventos pendientes de un tipo y los borra todos.
func events[E fw.Event](o *domain.Order) []E {
	var out []E
	for _, e := range o.PendingEvents() {
		if x, ok := e.(E); ok {
			out = append(out, x)
		}
	}
	o.ClearEvents()
	return out
}

// TestOrder_AmountLikeTheInvoice comprueba D-149: el importe de la línea de pedido, de lo pendiente
// y de la línea de albarán se calcula como el neto de la línea de factura, con el descuento
// encadenado a 4 decimales y aplicado a la línea entera, redondeando una vez al céntimo. 1000 × 1 €
// con un 5 % y un 2,5 % dan 926,25 (7,375 %), no 926,30 (0,9263 × 1000).
func TestOrder_AmountLikeTheInvoice(t *testing.T) {
	o := draft(t, "2.5")
	if _, err := o.AddLine(line("1000", "1", "5", false)); err != nil {
		t.Fatal(err)
	}
	l := o.State().Lines[0]
	if !l.ChainedDiscount.Equal(dec("7.375")) || !l.Amount.Equal(dec("926.25")) || !l.NetPrice.Equal(dec("0.9263")) || !o.Total().Equal(dec("926.25")) ||
		!o.PendingAmount().Equal(dec("926.25")) {
		t.Fatalf("line %+v, total %s, pending %s", l, o.Total(), o.PendingAmount())
	}
	// 12,5 % y 7,25 % encadenados dan 18,84375 %, que se guarda como 18,8438 % (los 4 decimales de la
	// línea de factura): 10000 × 1 € dan 8115,62 y no 8115,63.
	if _, err := o.AddLine(line("10000", "1", "12.5", false)); err != nil {
		t.Fatal(err)
	}
	o2 := draft(t, "7.25")
	if _, err := o2.AddLine(line("10000", "1", "12.5", false)); err != nil {
		t.Fatal(err)
	}
	if l := o2.State().Lines[0]; !l.ChainedDiscount.Equal(dec("18.8438")) || !l.Amount.Equal(dec("8115.62")) {
		t.Fatalf("rounded chained discount: %+v", l)
	}
	if err := o.RemoveLine(2); err != nil {
		t.Fatal(err)
	}

	// Entregado en dos albaranes, 400 y 600: cada línea de albarán con el precio y el descuento del
	// pedido, y su importe calculado igual (370,50 + 555,75 = 926,25).
	if err := o.Confirm("PED-1"); err != nil {
		t.Fatal(err)
	}
	first, err := o.Deliver([]domain.Pick{{Line: 1, Quantity: dec("400")}})
	if err != nil {
		t.Fatal(err)
	}
	if d := first[0]; !d.UnitPrice.Equal(dec("1")) || !d.Discount.Equal(dec("7.375")) || !d.Amount.Equal(dec("370.50")) {
		t.Fatalf("first delivery line: %+v", d)
	}
	if !o.PendingAmount().Equal(dec("555.75")) {
		t.Fatalf("pending: %s", o.PendingAmount())
	}
	rest, err := o.Deliver([]domain.Pick{{Line: 1, Quantity: dec("600")}})
	if err != nil || !rest[0].Amount.Equal(dec("555.75")) {
		t.Fatalf("second delivery line: %+v %v", rest, err)
	}
	if !domain.LineAmount(dec("1000"), dec("1"), dec("7.375")).Equal(dec("926.25")) || !domain.EffectiveDiscount(dec("5"), dec("2.5")).Equal(dec("7.375")) {
		t.Fatal("LineAmount or EffectiveDiscount")
	}
}

func TestOrder_PricesEachDiscountOnce(t *testing.T) {
	o := draft(t, "10")
	// 0.08 con un 5 % de la tarifa y un 10 % del cliente: 0.08 × 0.95 × 0.90 = 0.0684.
	no, err := o.AddLine(line("1000", "0.08", "5", true))
	if err != nil || no != 1 {
		t.Fatalf("line: %d %v", no, err)
	}
	if _, err := o.AddLine(line("3", "35", "0", false)); err != nil {
		t.Fatal(err)
	}
	s := o.State()
	if !s.Lines[0].NetPrice.Equal(dec("0.0684")) || !s.Lines[0].Amount.Equal(dec("68.40")) || !s.Lines[1].Amount.Equal(dec("94.50")) ||
		!o.Total().Equal(dec("162.90")) {
		t.Fatalf("prices: %+v total %s", s.Lines, o.Total())
	}
	if _, err := o.AddLine(line("0", "1", "0", true)); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("zero quantity: %v", err)
	}
	if _, err := o.AddLine(line("1", "1", "101", true)); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("discount: %v", err)
	}
	if err := o.RemoveLine(2); err != nil || len(o.State().Lines) != 1 {
		t.Fatalf("remove: %v", err)
	}
	if no, _ := o.AddLine(line("3", "35", "0", false)); no != 2 {
		t.Fatalf("numbering: %d", no)
	}
}

func TestOrder_ConfirmHoldDeliver(t *testing.T) {
	o := draft(t, "0")
	if err := o.Confirm("PED-2026-000001"); !isViolation(err, "orders.empty") {
		t.Fatalf("empty: %v", err)
	}
	_, _ = o.AddLine(line("10", "2", "0", true))
	_, _ = o.AddLine(line("3", "35", "0", false))
	if _, err := o.Deliver([]domain.Pick{{Line: 1, Quantity: dec("1")}}); !isViolation(err, "orders.not_confirmed") {
		t.Fatalf("deliver a draft: %v", err)
	}
	if err := o.Confirm("PED-2026-000001"); err != nil {
		t.Fatal(err)
	}
	req := events[domain.StockRequested](o)
	if len(req) != 1 || len(req[0].Lines) != 1 || req[0].Lines[0].Quantity != "10" || req[0].Warehouse != warehouse.String() {
		t.Fatalf("stock request: %+v", req)
	}
	if _, err := o.AddLine(line("1", "1", "0", true)); !isViolation(err, "orders.not_draft") {
		t.Fatalf("frozen: %v", err)
	}
	if _, err := o.Deliver([]domain.Pick{{Line: 1, Quantity: dec("4")}}); !isViolation(err, "orders.not_reserved") {
		t.Fatalf("nothing held yet: %v", err)
	}
	o.Hold(1, dec("6"))
	o.Hold(9, dec("6")) // línea desconocida: se ignora
	if err := o.RequestStock(); err != nil {
		t.Fatal(err)
	}
	if req := events[domain.StockRequested](o); len(req) != 1 || req[0].Lines[0].Quantity != "4" {
		t.Fatalf("second request: %+v", req)
	}
	if _, err := o.Deliver([]domain.Pick{{Line: 1, Quantity: dec("7")}}); !isViolation(err, "orders.not_reserved") {
		t.Fatalf("more than held: %v", err)
	}
	if _, err := o.Deliver([]domain.Pick{{Line: 2, Quantity: dec("4")}}); !isViolation(err, "orders.over_delivered") {
		t.Fatalf("more than ordered: %v", err)
	}
	if _, err := o.Deliver([]domain.Pick{{Line: 1, Quantity: dec("1")}, {Line: 1, Quantity: dec("1")}}); !isViolation(err, "orders.delivery_duplicate") {
		t.Fatalf("line twice: %v", err)
	}
	lines, err := o.Deliver([]domain.Pick{{Line: 1, Quantity: dec("6")}, {Line: 2, Quantity: dec("3")}})
	if err != nil || len(lines) != 2 || !lines[0].Amount.Equal(dec("12")) || !lines[1].Amount.Equal(dec("105")) {
		t.Fatalf("delivery: %+v %v", lines, err)
	}
	s := o.State()
	if s.Status != domain.Confirmed || !s.Lines[0].Reserved.IsZero() || !s.Lines[0].Pending().Equal(dec("4")) || !o.PendingAmount().Equal(dec("8")) {
		t.Fatalf("after the delivery: %+v", s)
	}
	if err := o.Cancel("no"); !isViolation(err, "orders.delivered") {
		t.Fatalf("cancel a served order: %v", err)
	}
	o.Hold(1, dec("9")) // nunca más allá de lo pendiente
	if !o.State().Lines[0].Reserved.Equal(dec("4")) {
		t.Fatalf("hold capped: %s", o.State().Lines[0].Reserved)
	}
	if err := o.RequestStock(); !isViolation(err, "orders.nothing_short") {
		t.Fatalf("nothing short: %v", err)
	}
	last, err := o.Deliver([]domain.Pick{{Line: 1, Quantity: dec("4")}})
	if err != nil || o.State().Status != domain.Delivered {
		t.Fatalf("last delivery: %v %s", err, o.State().Status)
	}
	// El albarán lleva al evento la periodicidad de facturación del cliente (D-146); sin ella, por albarán.
	for want, given := range map[domain.BillingPeriodicity]domain.BillingPeriodicity{domain.Monthly: domain.Monthly, domain.PerDelivery: ""} {
		d, err := domain.IssueDelivery(domain.NewDeliveryID(), o, "ALB-1", vocab.MustDate(2026, 10, 5), last, given)
		if err != nil {
			t.Fatal(err)
		}
		ev := d.PendingEvents()
		if issued, ok := ev[0].(domain.DeliveryIssued); len(ev) != 1 || !ok || issued.BillingPeriodicity != want {
			t.Fatalf("delivery issued %s: %+v %v", want, ev, err)
		}
	}
	if err := o.Close("x"); !isViolation(err, "orders.not_confirmed") {
		t.Fatalf("close a delivered order: %v", err)
	}
}

func TestOrder_CancelAndClose(t *testing.T) {
	o := draft(t, "0")
	_, _ = o.AddLine(line("10", "2", "0", true))
	_ = o.Confirm("PED-2026-000002")
	o.Hold(1, dec("10"))
	if err := o.Close("resto"); !isViolation(err, "orders.nothing_delivered") {
		t.Fatalf("close without deliveries: %v", err)
	}
	if err := o.Cancel(" "); !isViolation(err, "orders.reason") {
		t.Fatalf("reason: %v", err)
	}
	o.ClearEvents()
	if err := o.Cancel("el cliente desiste"); err != nil {
		t.Fatal(err)
	}
	closed := events[domain.OrderClosed](o)
	if len(closed) != 1 || closed[0].Status != "cancelled" || len(closed[0].Lines) != 1 || !o.State().Lines[0].Reserved.IsZero() {
		t.Fatalf("cancelled: %+v", closed)
	}
	if err := o.Cancel("otra vez"); !isViolation(err, "orders.closed") {
		t.Fatalf("cancel twice: %v", err)
	}
	o.Hold(1, dec("1")) // una reserva tardía de un pedido anulado se ignora
	if !o.State().Lines[0].Reserved.IsZero() {
		t.Fatal("late hold")
	}

	p := draft(t, "0")
	_, _ = p.AddLine(line("10", "2", "0", true))
	_ = p.Confirm("PED-2026-000003")
	p.Hold(1, dec("10"))
	if _, err := p.Deliver([]domain.Pick{{Line: 1, Quantity: dec("3")}}); err != nil {
		t.Fatal(err)
	}
	if err := p.Close("sin existencias del resto"); err != nil || p.State().Status != domain.Closed || !p.State().Lines[0].Reserved.IsZero() {
		t.Fatalf("close: %v %+v", err, p.State())
	}
}

func TestTermsAndCounter(t *testing.T) {
	if _, err := domain.ReconstituteTerms(domain.NewTermsID(), domain.TermsState{Company: company, Customer: customer, Discount: dec("100.5")}); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("discount: %v", err)
	}
	tr, err := domain.ReconstituteTerms(domain.NewTermsID(), domain.TermsState{Company: company, Customer: customer, Discount: dec("10")})
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Change(domain.TermsState{Discount: dec("5"), BlockDelivery: true}); err != nil || tr.State().Customer != customer || !tr.State().BlockDelivery {
		t.Fatalf("change: %v", err)
	}
	// El motivo del bloqueo se guarda con el bloqueo y se borra al desbloquear los dos (D-122).
	if err := tr.Change(domain.TermsState{BlockOrders: true, BlockReason: " impagos "}); err != nil || tr.State().BlockReason != "impagos" {
		t.Fatalf("reason: %v %+v", err, tr.State())
	}
	if err := tr.Change(domain.TermsState{BlockReason: "impagos"}); err != nil || tr.State().BlockReason != "" {
		t.Fatalf("unblocked: %v %+v", err, tr.State())
	}
	// La periodicidad de facturación (D-146): por albarán si no se dice; mensual; nada más.
	if tr.State().BillingPeriodicity != domain.PerDelivery {
		t.Fatalf("per delivery by default: %+v", tr.State())
	}
	if err := tr.Change(domain.TermsState{BillingPeriodicity: domain.Monthly}); err != nil || tr.State().BillingPeriodicity != domain.Monthly {
		t.Fatalf("monthly: %v %+v", err, tr.State())
	}
	if err := tr.Change(domain.TermsState{BillingPeriodicity: "weekly"}); !errors.Is(err, fw.ErrValidation) || tr.State().BillingPeriodicity != domain.Monthly {
		t.Fatalf("weekly: %v", err)
	}
	// El cálculo de las facturas del cliente (D-150): sin nada, el de la empresa; los dos redondeos
	// y de 0 a 6 decimales; nada más.
	if tr.State().LineRounding != "" || tr.State().PriceDecimals != nil {
		t.Fatalf("the company's calculation by default: %+v", tr.State())
	}
	zero, six, seven, minus := 0, 6, 7, -1
	for _, ok := range []domain.TermsState{{LineRounding: domain.RoundOnce, PriceDecimals: &zero}, {LineRounding: domain.RoundGrossThenDiscount, PriceDecimals: &six}} {
		if err := tr.Change(ok); err != nil || tr.State().LineRounding != ok.LineRounding || *tr.State().PriceDecimals != *ok.PriceDecimals {
			t.Fatalf("%+v: %v %+v", ok, err, tr.State())
		}
	}
	for _, bad := range []domain.TermsState{{LineRounding: "twice"}, {PriceDecimals: &seven}, {PriceDecimals: &minus}} {
		if err := tr.Change(bad); !errors.Is(err, fw.ErrValidation) {
			t.Fatalf("%+v: %v", bad, err)
		}
	}
	c, err := domain.ReconstituteCounter(domain.NewCounterID(), company, "PED", 2026, 41)
	if err != nil || c.Next() != "PED-2026-000042" {
		t.Fatalf("counter: %v", err)
	}
}
