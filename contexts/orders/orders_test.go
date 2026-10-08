package orders_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jhermoso/karpo-fw-go/contexts/inventory"
	iapp "github.com/jhermoso/karpo-fw-go/contexts/inventory/application"
	iinfra "github.com/jhermoso/karpo-fw-go/contexts/inventory/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/orders"
	oapp "github.com/jhermoso/karpo-fw-go/contexts/orders/application"
	oinfra "github.com/jhermoso/karpo-fw-go/contexts/orders/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/parties"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	pinfra "github.com/jhermoso/karpo-fw-go/contexts/parties/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/products"
	dapp "github.com/jhermoso/karpo-fw-go/contexts/products/application"
	ddomain "github.com/jhermoso/karpo-fw-go/contexts/products/domain"
	dinfra "github.com/jhermoso/karpo-fw-go/contexts/products/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/receivables"
	rapp "github.com/jhermoso/karpo-fw-go/contexts/receivables/application"
	rinfra "github.com/jhermoso/karpo-fw-go/contexts/receivables/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authorization"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution/jwtauth"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/messaging/inprocess"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
	"github.com/jhermoso/karpo-fw-go/pkg/testing/archtest"
	"github.com/jhermoso/karpo-fw-go/pkg/time/fake"
)

// host composes Parties, Products, Inventory, Receivables and Orders on one hot-swappable
// backend, with one in-process broker carrying the Published Language between Orders and
// Inventory.
type host struct {
	t        *testing.T
	srv      *httptest.Server
	sw       *hotswap.Switch
	dir      *authorization.MemoryDirectory
	ids      map[string]fw.UUID
	tokens   map[string]string
	ord      *orders.Module
	inv      *inventory.Module
	prod     *products.Module
	rec      *receivables.Module
	parties  *parties.Module
	broker   *inprocess.Broker
	adminCtx context.Context
}

func compose(t *testing.T) *host {
	ctx := context.Background()
	sw := hotswap.New(memory.NewStore("memory"))
	pm := parties.Compose(sw, nil)
	dm := products.Compose(sw)
	im := inventory.Compose(sw, iinfra.ProductsCatalog{Catalog: dm.Catalog})
	rm := receivables.Compose(sw, nil)
	om := orders.Compose(sw, oinfra.ProductsCatalog{Catalog: dm.Catalog, Pricing: dm.Pricing}, oinfra.ReceivablesCredit{Exposure: rm.Credit})
	broker := inprocess.NewBroker()
	broker.Subscribe("inventory", im.Consumer)
	broker.Subscribe("orders", om.Consumer)

	jwt, _ := jwtauth.New(jwtauth.Config{Secret: []byte("orders-test")})
	dir := authorization.NewMemoryDirectory()
	h := &host{t: t, sw: sw, dir: dir, ids: map[string]fw.UUID{}, tokens: map[string]string{}, ord: om, inv: im, prod: dm, rec: rm, parties: pm,
		broker: broker}
	all := []authz.Permission{oapp.PermOrderRead, oapp.PermOrderUpdate, oapp.PermOrderConfirm, oapp.PermOrderDeliver, oapp.PermOrderCancel,
		oapp.PermTermsRead, oapp.PermTermsUpdate}
	users := map[string][]authz.Permission{"seller": {oapp.PermOrderRead, oapp.PermOrderUpdate, oapp.PermTermsRead},
		"manager":    {oapp.PermOrderRead, oapp.PermOrderUpdate, oapp.PermOrderConfirm, oapp.PermOrderCancel, oapp.PermTermsRead, oapp.PermTermsUpdate},
		"dispatcher": {oapp.PermOrderRead, oapp.PermOrderDeliver}, "outsider": all}
	for u, perms := range users {
		h.ids[u] = fw.NewUUID()
		dir.Put(h.ids[u], authz.Subject{Active: true, Permissions: perms})
		tok, _ := jwt.Issue(jwtauth.Claims{Subject: h.ids[u].String(), Username: u, PartyID: fw.NewUUID().String(), ExpiresAt: fw.Now().Add(time.Hour).Unix()})
		h.tokens[u] = "Bearer " + tok
	}
	ac, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "setup", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
	ac.GlobalAdmin = true
	h.adminCtx = authz.WithContext(ctx, ac)

	mux := http.NewServeMux()
	om.RegisterRoutes(mux)
	h.srv = httptest.NewServer(distribution.Chain(mux, distribution.Authorize(jwt, authorization.NewResolver(dir, authorization.Options{}))))
	t.Cleanup(h.srv.Close)
	t.Cleanup(func() { _ = sw.Close(ctx) })
	return h
}

func (h *host) grant(user string, orgs ...string) {
	s, _, _ := h.dir.Subject(context.Background(), h.ids[user])
	for _, o := range orgs {
		s.Grants = append(s.Grants, authz.Grant{OrganizationID: fw.MustParseUUID(o), Level: authz.Full})
	}
	h.dir.Put(h.ids[user], s)
}

func (h *host) do(method, path, user string, body, out any) int {
	h.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest(method, h.srv.URL+path, &buf)
	req.Header.Set("Authorization", h.tokens[user])
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer res.Body.Close()
	if out != nil && res.StatusCode < 300 {
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			h.t.Fatal(err)
		}
	}
	return res.StatusCode
}

func (h *host) must(got, want int, what string) {
	h.t.Helper()
	if got != want {
		h.t.Fatalf("%s: status %d, want %d", what, got, want)
	}
}

func (h *host) ok(err error) {
	h.t.Helper()
	if err != nil {
		h.t.Fatal(err)
	}
}

// deliver relays the Published Language of Orders and Inventory until quiet.
func (h *host) deliver() {
	for moved := true; moved; {
		moved = false
		for _, r := range []interface {
			RelayOnce(context.Context) (int, error)
		}{h.ord.Relay(h.broker), h.inv.Relay(h.broker)} {
			n, err := r.RelayOnce(context.Background())
			h.ok(err)
			moved = moved || n > 0
		}
	}
}

func (h *host) order(id string) oapp.OrderDTO {
	h.t.Helper()
	var o oapp.OrderDTO
	h.must(h.do("GET", "/api/orders/orders/"+id, "seller", nil, &o), 200, "order")
	return o
}

func (h *host) scenario(tag string) {
	t := h.t
	ctx := h.adminCtx
	org := func(name string) papp.PartyDTO {
		o, err := h.parties.Service.RegisterOrganization.Handle(ctx, papp.RegisterOrganization{LegalName: name + " " + tag,
			Roles: []string{pdomain.RoleInternalOrganization.String()}})
		h.ok(err)
		return o
	}
	acme, globex := org("Acme"), org("Globex")
	ana, err := h.parties.Service.RegisterPerson.Handle(ctx, papp.RegisterPerson{GivenName: "Ana", FirstSurname: "Núñez " + tag})
	h.ok(err)
	for _, u := range []string{"seller", "manager", "dispatcher"} {
		h.grant(u, acme.ID)
	}
	h.grant("outsider", globex.ID)

	// Products with their prices, a warehouse with 600 screws.
	product := func(company, sku string, d dapp.DetailsInput) dapp.ProductDTO {
		d.Name = sku + " " + tag
		p, err := h.prod.Service.RegisterProduct.Handle(ctx, dapp.RegisterProduct{Company: company, SKU: sku, DetailsInput: d})
		h.ok(err)
		return p
	}
	tornillo := product(acme.ID, "TOR-M8", dapp.DetailsInput{Kind: "good", UoM: "ea", TaxCode: "G21", BasePrice: "0.125", ForSale: true, Stocked: true})
	montaje := product(acme.ID, "MONT", dapp.DetailsInput{Kind: "service", UoM: "h", TaxCode: "G21", BasePrice: "35", ForSale: true})
	bloqueado := product(acme.ID, "OLD", dapp.DetailsInput{Kind: "good", UoM: "ea", BasePrice: "1", ForSale: true, BlockedSales: true})
	ajeno := product(globex.ID, "TOR-M8", dapp.DetailsInput{Kind: "good", UoM: "ea", BasePrice: "1", ForSale: true})
	list, err := h.prod.Service.CreatePriceList.Handle(ctx, dapp.CreatePriceList{Company: acme.ID, Code: "MAYOR", Name: "Mayoristas"})
	h.ok(err)
	lid, _ := ddomain.ParsePriceListID(list.ID)
	_, err = h.prod.Service.SetPrice.Handle(ctx, dapp.SetPrice{ID: lid, Product: tornillo.ID, MinQuantity: "1000", UnitPrice: "0.08", Discount: "5",
		From: vocab.MustDate(2026, 1, 1)})
	h.ok(err)
	wh, err := h.inv.Service.CreateWarehouse.Handle(ctx, iapp.CreateWarehouse{Company: acme.ID, Code: "AL1", Name: "Central"})
	h.ok(err)
	receive := func(q string) {
		_, err := h.inv.Service.Receive.Handle(ctx, iapp.Receive{Warehouse: wh.ID, Product: tornillo.ID, Quantity: q, UnitCost: "0.04",
			Date: vocab.MustDate(2026, 10, 1)})
		h.ok(err)
	}
	receive("600")
	stock := func() (string, string) {
		s, err := h.inv.Availability.Stock(context.Background(), acme.ID, tornillo.ID, wh.ID)
		h.ok(err)
		return s.OnHand, s.Reserved
	}

	// The terms of the customer: the wholesale list and 10% on every line.
	var terms oapp.TermsDTO
	h.must(h.do("PUT", "/api/orders/terms", "seller", map[string]any{"company": acme.ID, "customer": ana.ID, "priceList": list.ID, "discount": "10"}, nil), 403,
		"selling is not setting terms")
	h.must(h.do("PUT", "/api/orders/terms", "manager", map[string]any{"company": acme.ID, "customer": ana.ID, "priceList": list.ID, "discount": "110"}, nil), 400,
		"discount")
	h.must(h.do("PUT", "/api/orders/terms", "manager", map[string]any{"company": acme.ID, "customer": ana.ID, "priceList": list.ID, "discount": "10"}, &terms), 200,
		"terms")

	// A draft priced by Products: each discount once.
	var o oapp.OrderDTO
	draft := map[string]any{"company": acme.ID, "customer": ana.ID, "date": "2026-10-04", "warehouse": wh.ID, "reference": "Su pedido 77"}
	h.must(h.do("POST", "/api/orders/orders", "outsider", draft, nil), 404, "outsider")
	h.must(h.do("POST", "/api/orders/orders", "dispatcher", draft, nil), 403, "delivering is not selling")
	h.must(h.do("POST", "/api/orders/orders", "seller", draft, &o), 201, "draft")
	if o.Status != "draft" || o.PriceList != list.ID || o.CustomerDiscount != "10.00" || o.Number != "" {
		t.Fatalf("draft: %+v", o)
	}
	lines := "/api/orders/orders/" + o.ID + "/lines"
	h.must(h.do("POST", lines, "seller", map[string]any{"product": tornillo.ID, "quantity": "0"}, nil), 400, "quantity")
	h.must(h.do("POST", lines, "seller", map[string]any{"product": bloqueado.ID, "quantity": "1"}, nil), 422, "blocked product")
	h.must(h.do("POST", lines, "seller", map[string]any{"product": ajeno.ID, "quantity": "1"}, nil), 422, "product of another company")
	h.must(h.do("POST", lines, "seller", map[string]any{"product": tornillo.ID, "quantity": "1000"}, nil), 200, "line")
	h.must(h.do("POST", lines, "seller", map[string]any{"product": montaje.ID, "quantity": "3"}, nil), 200, "service line")
	h.must(h.do("POST", lines, "seller", map[string]any{"product": montaje.ID, "quantity": "9"}, &o), 200, "a line to remove")
	h.must(h.do("POST", lines+"/remove", "seller", map[string]any{"line": 3}, &o), 200, "remove")
	// 0.08 × 0.95 × 0.90 = 0.0684 → 68.40; 35 × 0.90 = 31.50 → 94.50.
	if len(o.Lines) != 2 || o.Lines[0].UnitPrice != "0.0800" || o.Lines[0].Discount != "5.00" || o.Lines[0].NetPrice != "0.0684" || o.Lines[0].Amount != "68.40" ||
		!o.Lines[0].Stocked || o.Lines[1].NetPrice != "31.5000" || o.Lines[1].Stocked || o.Total != "162.90" || o.Lines[0].TaxCode != "G21" {
		t.Fatalf("priced: %+v", o)
	}

	// Confirmation: its own permission, the credit of Receivables, a number.
	confirm := "/api/orders/orders/" + o.ID + "/confirm"
	h.must(h.do("POST", confirm, "seller", nil, nil), 403, "selling is not confirming")
	limit := "100"
	_, err = h.rec.Service.SetCredit.Handle(ctx, rapp.SetCredit{Seller: acme.ID, Customer: ana.ID, Limit: &limit})
	h.ok(err)
	h.must(h.do("POST", confirm, "manager", nil, nil), 422, "credit exceeded")
	limit = "1000"
	_, err = h.rec.Service.SetCredit.Handle(ctx, rapp.SetCredit{Seller: acme.ID, Customer: ana.ID, Limit: &limit})
	h.ok(err)
	h.must(h.do("POST", confirm, "manager", nil, &o), 200, "confirm")
	h.must(h.do("POST", confirm, "manager", nil, nil), 422, "confirmed once")
	if o.Status != "confirmed" || o.Number != "PED-2026-000001" || o.Lines[0].Short != "1000" {
		t.Fatalf("confirmed: %+v", o)
	}
	h.must(h.do("POST", lines, "seller", map[string]any{"product": montaje.ID, "quantity": "1"}, nil), 422, "a confirmed order does not change")

	// Nothing is delivered of a stocked line until Inventory holds it; it holds the 600 there are.
	deliveries := "/api/orders/orders/" + o.ID + "/deliveries"
	h.must(h.do("POST", deliveries, "dispatcher", map[string]any{"lines": []map[string]any{{"line": 1, "quantity": "100"}}}, nil), 422, "not held yet")
	h.deliver()
	if o = h.order(o.ID); o.Lines[0].Reserved != "600" || o.Lines[0].Short != "400" {
		t.Fatalf("held: %+v", o.Lines[0])
	}
	if on, res := stock(); on != "600" || res != "600" {
		t.Fatalf("stock held: %s %s", on, res)
	}

	// The delivery note of everything that can go: 600 screws and the 3 hours.
	var note oapp.DeliveryDTO
	h.must(h.do("POST", deliveries, "seller", nil, nil), 403, "selling is not delivering")
	h.must(h.do("POST", deliveries, "dispatcher", map[string]any{"date": "2026-10-03"}, nil), 422, "before the order")
	h.must(h.do("POST", deliveries, "dispatcher", map[string]any{"date": "2026-10-05"}, &note), 201, "delivery note")
	// 600 × 0.0684 = 41.04, plus 94.50.
	if note.Number != "ALB-2026-000001" || note.OrderNumber != "PED-2026-000001" || len(note.Lines) != 2 || note.Lines[0].Quantity != "600" ||
		note.Lines[0].Amount != "41.04" || note.Total != "135.54" || note.OrderStatus != "confirmed" {
		t.Fatalf("delivery note: %+v", note)
	}
	h.deliver()
	if on, res := stock(); on != "0" || res != "0" {
		t.Fatalf("stock issued: %s %s", on, res)
	}
	card, err := h.inv.Service.Ledger.Handle(ctx, iapp.GetLedger{Company: acme.ID, Warehouse: wh.ID, Product: tornillo.ID})
	h.ok(err)
	if len(card.Items) != 2 || card.Items[1].Kind != "issue" || card.Items[1].Quantity != "-600" || card.Items[1].Note != "Albarán ALB-2026-000001" {
		t.Fatalf("stock card: %+v", card.Items)
	}

	// The rest waits for stock: asked again when 250 arrive, delivered, and the order closed.
	request := "/api/orders/orders/" + o.ID + "/request-stock"
	h.must(h.do("POST", request, "manager", nil, nil), 200, "ask again")
	h.deliver()
	if o = h.order(o.ID); o.Lines[0].Reserved != "0" || o.Lines[0].Delivered != "600" || o.Pending != "27.36" {
		t.Fatalf("still short: %+v", o.Lines[0])
	}
	receive("250")
	h.must(h.do("POST", request, "manager", nil, nil), 200, "ask again with stock")
	h.deliver()
	if o = h.order(o.ID); o.Lines[0].Reserved != "250" || o.Lines[0].Short != "150" {
		t.Fatalf("held later: %+v", o.Lines[0])
	}
	h.must(h.do("POST", deliveries, "dispatcher", map[string]any{"lines": []map[string]any{{"line": 1, "quantity": "300"}}}, nil), 422, "more than held")
	// A note without a date is of today and takes its number from the series of that year: those
	// requests run on a fixed clock, so that the scenario stays in 2026 whatever the day the test runs.
	// The moment is a past one: the tokens are issued with the real clock and must not have expired.
	today := func(f func()) {
		defer fw.SetClock(fake.New(time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)))()
		f()
	}
	today(func() {
		h.must(h.do("POST", deliveries, "dispatcher", map[string]any{"lines": []map[string]any{{"line": 1, "quantity": "250"}}}, &note), 201, "second delivery")
	})
	if note.Number != "ALB-2026-000002" || note.Total != "17.10" {
		t.Fatalf("second delivery: %+v", note)
	}
	h.must(h.do("POST", "/api/orders/orders/"+o.ID+"/cancel", "manager", map[string]any{"reason": "x"}, nil), 422, "a served order is closed, not cancelled")
	h.must(h.do("POST", "/api/orders/orders/"+o.ID+"/close", "seller", map[string]any{"reason": "x"}, nil), 403, "selling is not closing")
	h.must(h.do("POST", "/api/orders/orders/"+o.ID+"/close", "manager", map[string]any{"reason": "El cliente renuncia al resto"}, &o), 200, "close")
	if o.Status != "closed" || o.Lines[0].Delivered != "850" || o.Lines[0].Pending != "150" {
		t.Fatalf("closed: %+v", o)
	}
	h.deliver()
	if on, res := stock(); on != "0" || res != "0" {
		t.Fatalf("stock after the second delivery: %s %s", on, res)
	}

	// A second order holds stock and is cancelled: what it held is released.
	receive("500")
	var o2 oapp.OrderDTO
	h.must(h.do("POST", "/api/orders/orders", "seller", draft, &o2), 201, "second draft")
	h.must(h.do("POST", "/api/orders/orders/"+o2.ID+"/lines", "seller", map[string]any{"product": tornillo.ID, "quantity": "100"}, &o2), 200, "its line")
	// Under 1000 units the list has no line: the base price, 0.125 × 0.90.
	if o2.Lines[0].NetPrice != "0.1125" || o2.Lines[0].Amount != "11.25" {
		t.Fatalf("base price: %+v", o2.Lines[0])
	}
	h.must(h.do("POST", "/api/orders/orders/"+o2.ID+"/confirm", "manager", nil, &o2), 200, "confirm the second")
	h.deliver()
	if _, res := stock(); res != "100" || o2.Number != "PED-2026-000002" {
		t.Fatalf("second order held: %s %s", res, o2.Number)
	}
	h.must(h.do("POST", "/api/orders/orders/"+o2.ID+"/close", "manager", map[string]any{"reason": "x"}, nil), 422, "nothing delivered: cancel")
	h.must(h.do("POST", "/api/orders/orders/"+o2.ID+"/cancel", "manager", map[string]any{"reason": "El cliente desiste"}, &o2), 200, "cancel")
	h.deliver()
	if on, res := stock(); on != "500" || res != "0" || o2.Status != "cancelled" {
		t.Fatalf("released: %s %s %s", on, res, o2.Status)
	}

	// A customer blocked for orders or deliveries.
	var o3 oapp.OrderDTO
	h.must(h.do("POST", "/api/orders/orders", "seller", draft, &o3), 201, "third draft")
	h.must(h.do("POST", "/api/orders/orders/"+o3.ID+"/lines", "seller", map[string]any{"product": montaje.ID, "quantity": "1"}, nil), 200, "its line")
	h.must(h.do("PUT", "/api/orders/terms", "manager", map[string]any{"company": acme.ID, "customer": ana.ID, "blockOrders": true}, nil), 200, "block orders")
	h.must(h.do("POST", "/api/orders/orders/"+o3.ID+"/confirm", "manager", nil, nil), 422, "blocked for orders")
	h.must(h.do("PUT", "/api/orders/terms", "manager", map[string]any{"company": acme.ID, "customer": ana.ID, "blockDelivery": true}, nil), 200, "block deliveries")
	h.must(h.do("POST", "/api/orders/orders/"+o3.ID+"/confirm", "manager", nil, &o3), 200, "confirm the third")
	h.must(h.do("POST", "/api/orders/orders/"+o3.ID+"/deliveries", "dispatcher", nil, nil), 422, "blocked for deliveries")
	h.must(h.do("PUT", "/api/orders/terms", "manager", map[string]any{"company": acme.ID, "customer": ana.ID}, nil), 200, "unblock")
	today(func() {
		h.must(h.do("POST", "/api/orders/orders/"+o3.ID+"/deliveries", "dispatcher", nil, &note), 201, "deliver the service")
	})
	// The discount of the customer is the one of the day the draft was opened (10%).
	if note.OrderStatus != "delivered" || note.Number != "ALB-2026-000003" || note.Lines[0].NetPrice != "31.5000" {
		t.Fatalf("third delivery: %+v", note)
	}

	var page fw.Page[oapp.OrderDTO]
	h.must(h.do("GET", "/api/orders/orders?company="+acme.ID+"&status=closed", "seller", nil, &page), 200, "closed orders")
	if len(page.Items) != 1 || page.Items[0].ID != o.ID {
		t.Fatalf("closed orders: %+v", page.Items)
	}
	var notes fw.Page[oapp.DeliveryDTO]
	h.must(h.do("GET", "/api/orders/deliveries?company="+acme.ID+"&order="+o.ID, "dispatcher", nil, &notes), 200, "delivery notes of the order")
	if len(notes.Items) != 2 || notes.Items[1].Number != "ALB-2026-000002" {
		t.Fatalf("delivery notes: %+v", notes.Items)
	}
	h.must(h.do("GET", "/api/orders/orders/"+o.ID, "outsider", nil, nil), 404, "outsider")
	h.must(h.do("GET", "/api/orders/deliveries/"+note.ID, "outsider", nil, nil), 404, "outsider")
}

func TestOrders_PriceHoldDeliverRelease_MemoryThenSQLite(t *testing.T) {
	h := compose(t)
	h.scenario("m")

	ctx := context.Background()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "host.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{pinfra.Migrations(), dinfra.Migrations(), iinfra.Migrations(), rinfra.Migrations(),
		oinfra.Migrations()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.sw.Swap(ctx, db); err != nil {
		t.Fatal(err)
	}
	h.scenario("s")
}

func TestOrders_LayersRespectTheArchitecture(t *testing.T) {
	const m = "github.com/jhermoso/karpo-fw-go"
	archtest.AssertOnlyImports(t, "domain", false, archtest.Std, m+"/pkg/domain/...", m+"/contexts/orders/domain")
	archtest.AssertOnlyImports(t, "contracts", false, archtest.Std)
	archtest.AssertTreeDoesNotImport(t, "application", []string{"/pkg/persistence", "/pkg/distribution", "database/sql", "net/http"})
}
