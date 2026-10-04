package inventory_test

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
	"github.com/jhermoso/karpo-fw-go/contexts/parties"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	pinfra "github.com/jhermoso/karpo-fw-go/contexts/parties/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/products"
	dapp "github.com/jhermoso/karpo-fw-go/contexts/products/application"
	dinfra "github.com/jhermoso/karpo-fw-go/contexts/products/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authorization"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution/jwtauth"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/messaging/inprocess"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
	"github.com/jhermoso/karpo-fw-go/pkg/testing/archtest"
)

// collector keeps the kinds of the stock movements published.
type collector struct{ kinds []string }

func (c *collector) HandleMessage(_ context.Context, env application.Envelope) error {
	var e struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(env.Data, &e); err != nil {
		return err
	}
	c.kinds = append(c.kinds, e.Kind)
	return nil
}

// host composes Parties, Products and Inventory on one hot-swappable backend.
type host struct {
	t        *testing.T
	srv      *httptest.Server
	sw       *hotswap.Switch
	dir      *authorization.MemoryDirectory
	ids      map[string]fw.UUID
	tokens   map[string]string
	inv      *inventory.Module
	prod     *products.Module
	parties  *parties.Module
	adminCtx context.Context
}

func compose(t *testing.T) *host {
	ctx := context.Background()
	sw := hotswap.New(memory.NewStore("memory"))
	pm := parties.Compose(sw, nil)
	dm := products.Compose(sw)
	im := inventory.Compose(sw, iinfra.ProductsCatalog{Catalog: dm.Catalog})

	jwt, _ := jwtauth.New(jwtauth.Config{Secret: []byte("inventory-test")})
	dir := authorization.NewMemoryDirectory()
	h := &host{t: t, sw: sw, dir: dir, ids: map[string]fw.UUID{}, tokens: map[string]string{}, inv: im, prod: dm, parties: pm}
	all := []authz.Permission{iapp.PermWarehouseRead, iapp.PermWarehouseUpdate, iapp.PermStockRead, iapp.PermStockMove, iapp.PermStockAdjust}
	users := map[string][]authz.Permission{"storekeeper": {iapp.PermWarehouseRead, iapp.PermStockRead, iapp.PermStockMove},
		"counter": {iapp.PermStockRead, iapp.PermStockAdjust}, "manager": {iapp.PermWarehouseRead, iapp.PermWarehouseUpdate, iapp.PermStockRead},
		"outsider": all}
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
	im.RegisterRoutes(mux)
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
	for _, u := range []string{"storekeeper", "counter", "manager"} {
		h.grant(u, acme.ID)
	}
	h.grant("outsider", globex.ID)
	product := func(company, sku, kind, uom, tracking string, stocked bool) dapp.ProductDTO {
		p, err := h.prod.Service.RegisterProduct.Handle(ctx, dapp.RegisterProduct{Company: company, SKU: sku, DetailsInput: dapp.DetailsInput{
			Name: sku + " " + tag, Kind: kind, UoM: uom, Stocked: stocked, Tracking: tracking}})
		h.ok(err)
		return p
	}
	tornillo, pintura := product(acme.ID, "TOR-M8", "good", "ea", "", true), product(acme.ID, "PINT", "good", "l", "lot", true)
	montaje, ajeno := product(acme.ID, "MONT", "service", "h", "", false), product(globex.ID, "TOR-M8", "good", "ea", "", true)

	var events collector
	broker := inprocess.NewBroker()
	broker.Subscribe("collector", &events)

	// Warehouses.
	var al1, al2 iapp.WarehouseDTO
	h.must(h.do("POST", "/api/inventory/warehouses", "storekeeper", map[string]any{"company": acme.ID, "code": "al1", "name": "Central"}, nil), 403,
		"moving stock is not opening warehouses")
	h.must(h.do("POST", "/api/inventory/warehouses", "outsider", map[string]any{"company": acme.ID, "code": "al1", "name": "Central"}, nil), 404, "outsider")
	h.must(h.do("POST", "/api/inventory/warehouses", "manager", map[string]any{"company": acme.ID, "code": "al1", "name": "Central"}, &al1), 201, "warehouse")
	h.must(h.do("POST", "/api/inventory/warehouses", "manager", map[string]any{"company": acme.ID, "code": "AL1", "name": "Otro"}, nil), 422, "code once")
	h.must(h.do("POST", "/api/inventory/warehouses", "manager", map[string]any{"company": acme.ID, "code": "AL2", "name": "Tienda"}, &al2), 201, "second warehouse")

	// Receipts: weighted average cost, one movement per source, only stocked products, lots.
	receipt := func(p, qty, cost, lot, source string) map[string]any {
		m := map[string]any{"warehouse": al1.ID, "product": p, "quantity": qty, "unitCost": cost, "date": "2026-10-01", "lot": lot}
		if source != "" {
			m["sourceType"], m["sourceId"] = "purchases.receipt", source
		}
		return m
	}
	var m1, m2, again iapp.MovementDTO
	h.must(h.do("POST", "/api/inventory/receipts", "counter", receipt(tornillo.ID, "100", "2", "", ""), nil), 403, "counting is not moving")
	h.must(h.do("POST", "/api/inventory/receipts", "outsider", receipt(tornillo.ID, "100", "2", "", ""), nil), 404, "outsider")
	h.must(h.do("POST", "/api/inventory/receipts", "storekeeper", receipt(tornillo.ID, "100", "2", "", "ALB-1"), &m1), 201, "receipt")
	h.must(h.do("POST", "/api/inventory/receipts", "storekeeper", receipt(tornillo.ID, "100", "2", "", "ALB-1"), &again), 201, "the same receipt again")
	h.must(h.do("POST", "/api/inventory/receipts", "storekeeper", receipt(tornillo.ID, "300", "3", "", "ALB-2"), &m2), 201, "second receipt")
	if again.ID != m1.ID || m1.Seq != 1 || m1.Value != "200.00" || m2.Balance != "400" || m2.Seq != 2 {
		t.Fatalf("receipts: %+v / %+v / %+v", m1, again, m2)
	}
	h.must(h.do("POST", "/api/inventory/receipts", "storekeeper", receipt(tornillo.ID, "-1", "2", "", ""), nil), 422, "negative quantity")
	h.must(h.do("POST", "/api/inventory/receipts", "storekeeper", receipt(montaje.ID, "1", "2", "", ""), nil), 422, "a service is not stocked")
	h.must(h.do("POST", "/api/inventory/receipts", "storekeeper", receipt(ajeno.ID, "1", "2", "", ""), nil), 422, "a product of another company")
	h.must(h.do("POST", "/api/inventory/receipts", "storekeeper", receipt(pintura.ID, "10", "12.50", "", ""), nil), 422, "a tracked product needs its lot")
	h.must(h.do("POST", "/api/inventory/receipts", "storekeeper", receipt(pintura.ID, "10", "12.50", "L-2026-10", ""), nil), 201, "receipt with lot")

	// Issues never take more than what is available, at the average cost (2.75).
	var out iapp.MovementDTO
	issue := map[string]any{"warehouse": al1.ID, "product": tornillo.ID, "quantity": "150", "date": "2026-10-02", "note": "Albarán de salida 7"}
	h.must(h.do("POST", "/api/inventory/issues", "storekeeper", issue, &out), 201, "issue")
	if out.Quantity != "-150" || out.UnitCost != "2.7500" || out.Value != "-412.50" || out.Balance != "250" {
		t.Fatalf("issue: %+v", out)
	}
	issue["quantity"] = "300"
	h.must(h.do("POST", "/api/inventory/issues", "storekeeper", issue, nil), 422, "more than there is")

	// Reservations hold stock for a source until it is issued or released.
	var res iapp.ReservationDTO
	reserve := map[string]any{"warehouse": al1.ID, "product": tornillo.ID, "quantity": "200", "sourceType": "orders.sales-order", "sourceId": "SO-1"}
	h.must(h.do("POST", "/api/inventory/reservations", "storekeeper", reserve, &res), 201, "reserve")
	h.must(h.do("POST", "/api/inventory/reservations", "storekeeper", reserve, nil), 422, "reserved once per source")
	reserve["sourceId"], reserve["quantity"] = "SO-2", "100"
	h.must(h.do("POST", "/api/inventory/reservations", "storekeeper", reserve, nil), 422, "only 50 are available")
	issue["quantity"] = "60"
	h.must(h.do("POST", "/api/inventory/issues", "storekeeper", issue, nil), 422, "what is reserved for another is not taken")
	issue["quantity"], issue["reservation"] = "120", res.ID
	h.must(h.do("POST", "/api/inventory/issues", "storekeeper", issue, &out), 201, "issue from the reservation")
	if out.Balance != "130" {
		t.Fatalf("issue from reservation: %+v", out)
	}
	issue["quantity"] = "81"
	h.must(h.do("POST", "/api/inventory/issues", "storekeeper", issue, nil), 422, "more than the reservation holds")
	h.must(h.do("POST", "/api/inventory/reservations/"+res.ID+"/release", "storekeeper", nil, &res), 200, "release")
	h.must(h.do("POST", "/api/inventory/reservations/"+res.ID+"/release", "storekeeper", nil, nil), 422, "released once")
	if res.Open != "0" || res.Quantity != "200" {
		t.Fatalf("released: %+v", res)
	}

	// A transfer carries the cost to the other warehouse.
	var moves []iapp.MovementDTO
	h.must(h.do("POST", "/api/inventory/transfers", "storekeeper", map[string]any{"from": al1.ID, "to": al1.ID, "product": tornillo.ID, "quantity": "30"}, nil),
		400, "to itself")
	h.must(h.do("POST", "/api/inventory/transfers", "storekeeper", map[string]any{"from": al1.ID, "to": al2.ID, "product": tornillo.ID, "quantity": "30",
		"date": "2026-10-03"}, &moves), 201, "transfer")
	if len(moves) != 2 || moves[0].Kind != "transfer-out" || moves[0].Balance != "100" || moves[1].Kind != "transfer-in" || moves[1].UnitCost != "2.7500" ||
		moves[1].SourceID != moves[0].ID {
		t.Fatalf("transfer: %+v", moves)
	}

	// A count is another permission and leaves its movement with the reason.
	var lvl iapp.LevelDTO
	count := map[string]any{"warehouse": al1.ID, "product": tornillo.ID, "counted": "98", "date": "2026-10-04", "reason": "Recuento anual"}
	h.must(h.do("POST", "/api/inventory/counts", "storekeeper", count, nil), 403, "moving is not counting")
	h.must(h.do("POST", "/api/inventory/counts", "counter", map[string]any{"warehouse": al1.ID, "product": tornillo.ID, "counted": "98"}, nil), 400, "reason")
	h.must(h.do("POST", "/api/inventory/counts", "counter", count, &lvl), 200, "count")
	if lvl.OnHand != "98" || lvl.AverageCost != "2.7500" || lvl.Value != "269.50" || lvl.SKU != "TOR-M8" {
		t.Fatalf("count: %+v", lvl)
	}
	h.must(h.do("POST", "/api/inventory/counts", "counter", count, &lvl), 200, "the same count moves nothing")

	// The stock card of the product in the warehouse, in posting order.
	var card fw.Page[iapp.MovementDTO]
	h.must(h.do("GET", "/api/inventory/movements?company="+acme.ID+"&warehouse="+al1.ID+"&product="+tornillo.ID, "counter", nil, &card), 200, "stock card")
	kinds := ""
	for _, m := range card.Items {
		kinds += m.Kind + " "
	}
	if len(card.Items) != 6 || kinds != "receipt receipt issue issue transfer-out adjustment " || card.Items[5].Quantity != "-2" ||
		card.Items[5].Note != "Recuento anual" || card.Items[5].Balance != "98" {
		t.Fatalf("stock card: %s %+v", kinds, card.Items)
	}

	// Reorder points, stock, valuation and the availability other contexts read.
	h.must(h.do("PUT", "/api/inventory/reorder-points", "storekeeper", map[string]any{"warehouse": al2.ID, "product": tornillo.ID, "point": "50"}, nil), 403,
		"reorder points are the manager's")
	h.must(h.do("PUT", "/api/inventory/reorder-points", "manager", map[string]any{"warehouse": al2.ID, "product": tornillo.ID, "point": "50"}, &lvl), 200, "reorder point")
	var stock []iapp.LevelDTO
	h.must(h.do("GET", "/api/inventory/stock?company="+acme.ID+"&belowReorder=true", "manager", nil, &stock), 200, "below reorder")
	if len(stock) != 1 || stock[0].Warehouse != al2.ID || stock[0].Available != "30" {
		t.Fatalf("below reorder: %+v", stock)
	}
	h.must(h.do("GET", "/api/inventory/stock?company="+acme.ID, "manager", nil, &stock), 200, "stock")
	if len(stock) != 3 || stock[0].SKU != "PINT" {
		t.Fatalf("stock: %+v", stock)
	}
	var val iapp.ValuationDTO
	h.must(h.do("GET", "/api/inventory/valuation?company="+acme.ID, "manager", nil, &val), 200, "valuation")
	// Screws: (98 + 30) × 2.75 = 352.00; paint: 10 × 12.50 = 125.00.
	if val.Total != "477.00" || len(val.Lines) != 2 || val.Lines[1].OnHand != "128" || val.Lines[1].Value != "352.00" {
		t.Fatalf("valuation: %+v", val)
	}
	h.must(h.do("GET", "/api/inventory/valuation?company="+acme.ID, "outsider", nil, nil), 404, "outsider")
	av, err := h.inv.Availability.Stock(context.Background(), acme.ID, tornillo.ID, "")
	if err != nil || av.OnHand != "128" || av.Reserved != "0" || av.Available != "128" {
		t.Fatalf("availability: %+v %v", av, err)
	}
	h.must(h.do("POST", "/api/inventory/warehouses/"+al2.ID+"/close", "manager", nil, nil), 422, "a warehouse with stock is not closed")

	// Every movement of the ledger is published.
	for {
		n, err := h.inv.Relay(broker).RelayOnce(context.Background())
		h.ok(err)
		if n == 0 {
			break
		}
	}
	if len(events.kinds) != 8 {
		t.Fatalf("published movements: %v", events.kinds)
	}
}

func TestInventory_LedgerLevelsAndReservations_MemoryThenSQLite(t *testing.T) {
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
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{pinfra.Migrations(), dinfra.Migrations(), iinfra.Migrations()})
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

func TestInventory_LayersRespectTheArchitecture(t *testing.T) {
	const m = "github.com/jhermoso/karpo-fw-go"
	archtest.AssertOnlyImports(t, "domain", false, archtest.Std, m+"/pkg/domain/...", m+"/contexts/inventory/domain")
	archtest.AssertOnlyImports(t, "contracts", false, archtest.Std)
	archtest.AssertTreeDoesNotImport(t, "application", []string{"/pkg/persistence", "/pkg/distribution", "database/sql", "net/http"})
}
