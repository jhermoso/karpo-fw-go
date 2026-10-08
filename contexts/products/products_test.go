package products_test

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

	"github.com/jhermoso/karpo-fw-go/contexts/parties"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	pinfra "github.com/jhermoso/karpo-fw-go/contexts/parties/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/products"
	dapp "github.com/jhermoso/karpo-fw-go/contexts/products/application"
	"github.com/jhermoso/karpo-fw-go/contexts/products/contracts"
	dinfra "github.com/jhermoso/karpo-fw-go/contexts/products/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authorization"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution/jwtauth"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
	"github.com/jhermoso/karpo-fw-go/pkg/testing/archtest"
)

// host composes Parties and Products on one hot-swappable backend.
type host struct {
	t        *testing.T
	srv      *httptest.Server
	sw       *hotswap.Switch
	dir      *authorization.MemoryDirectory
	ids      map[string]fw.UUID
	tokens   map[string]string
	prod     *products.Module
	parties  *parties.Module
	adminCtx context.Context
}

func compose(t *testing.T) *host {
	ctx := context.Background()
	sw := hotswap.New(memory.NewStore("memory"))
	pm := parties.Compose(sw, nil)
	dm := products.Compose(sw)

	jwt, _ := jwtauth.New(jwtauth.Config{Secret: []byte("products-test")})
	dir := authorization.NewMemoryDirectory()
	h := &host{t: t, sw: sw, dir: dir, ids: map[string]fw.UUID{}, tokens: map[string]string{}, prod: dm, parties: pm}
	all := []authz.Permission{dapp.PermProductRead, dapp.PermProductUpdate, dapp.PermPriceListRead, dapp.PermPriceListWrite}
	users := map[string][]authz.Permission{"cataloguer": {dapp.PermProductRead, dapp.PermProductUpdate, dapp.PermPriceListRead},
		"pricer": {dapp.PermProductRead, dapp.PermPriceListRead, dapp.PermPriceListWrite}, "outsider": all}
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
	dm.RegisterRoutes(mux)
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

func merge(base map[string]any, over map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		out[k] = v
	}
	return out
}

func (h *host) scenario(tag string) {
	t := h.t
	ctx := h.adminCtx
	org := func(name string, roles ...string) papp.PartyDTO {
		o, err := h.parties.Service.RegisterOrganization.Handle(ctx, papp.RegisterOrganization{LegalName: name + " " + tag, Roles: roles})
		h.ok(err)
		return o
	}
	acme, globex := org("Acme", pdomain.RoleInternalOrganization.String()), org("Globex", pdomain.RoleInternalOrganization.String())
	proveedor := org("Tornillería Ibérica")
	h.grant("cataloguer", acme.ID)
	h.grant("pricer", acme.ID)
	h.grant("outsider", globex.ID)

	var units []products.UnitDTO
	h.must(h.do("GET", "/api/products/units", "cataloguer", nil, &units), 200, "units")
	if len(units) != 14 || units[0].Code != "ea" {
		t.Fatalf("units: %+v", units)
	}

	// Categories: a tree with unique codes per company.
	var ferr, torn dapp.CategoryDTO
	h.must(h.do("POST", "/api/products/categories", "cataloguer", map[string]any{"company": acme.ID, "code": "ferr", "name": "Ferretería"}, &ferr), 201, "category")
	h.must(h.do("POST", "/api/products/categories", "cataloguer", map[string]any{"company": acme.ID, "code": "FERR", "name": "Otra"}, nil), 422, "category code once")
	h.must(h.do("POST", "/api/products/categories", "cataloguer", map[string]any{"company": acme.ID, "code": "TORN", "name": "Tornillería", "parent": ferr.ID},
		&torn), 201, "child category")
	h.must(h.do("POST", "/api/products/categories", "outsider", map[string]any{"company": acme.ID, "code": "X", "name": "X"}, nil), 404, "outsider")

	// Products: SKU and barcodes unique in the company, check digits, goods and services.
	screw := map[string]any{"company": acme.ID, "sku": "tor-m8", "name": "Tornillo M8", "kind": "good", "uom": "ea", "category": torn.ID, "taxCode": "G21",
		"expenseCategory": "goods", "basePrice": "0.125", "standardCost": "0.04", "forSale": true, "forPurchase": true, "stocked": true,
		"barcodes":  []map[string]any{{"type": "ean-13", "value": "8412345678905"}},
		"suppliers": []map[string]any{{"supplier": proveedor.ID, "code": "M8-ZN", "leadDays": 7, "preferred": true}}}
	var tornillo, tuerca, kit, montaje dapp.ProductDTO
	h.must(h.do("POST", "/api/products/products", "pricer", screw, nil), 403, "pricing is not cataloguing")
	h.must(h.do("POST", "/api/products/products", "outsider", screw, nil), 404, "outsider")
	h.must(h.do("POST", "/api/products/products", "cataloguer", merge(screw, map[string]any{"barcodes": []map[string]any{{"type": "ean-13", "value": "8412345678906"}}}),
		nil), 400, "barcode check digit")
	h.must(h.do("POST", "/api/products/products", "cataloguer", merge(screw, map[string]any{"uom": "docena"}), nil), 400, "unknown unit")
	h.must(h.do("POST", "/api/products/products", "cataloguer", screw, &tornillo), 201, "product")
	h.must(h.do("POST", "/api/products/products", "cataloguer", screw, nil), 422, "SKU once")
	if tornillo.SKU != "TOR-M8" || tornillo.BasePrice != "0.1250" || tornillo.Tracking != "none" || len(tornillo.Suppliers) != 1 {
		t.Fatalf("product: %+v", tornillo)
	}
	nut := map[string]any{"company": acme.ID, "sku": "TUE-M8", "name": "Tuerca M8", "kind": "good", "uom": "ea", "category": torn.ID, "taxCode": "G21",
		"basePrice": "0.05", "forSale": true, "stocked": true}
	h.must(h.do("POST", "/api/products/products", "cataloguer", merge(nut, map[string]any{"barcodes": []map[string]any{{"type": "ean-13", "value": "8412345678905"}}}),
		nil), 422, "a barcode identifies one product")
	h.must(h.do("POST", "/api/products/products", "cataloguer", nut, &tuerca), 201, "second product")
	h.must(h.do("POST", "/api/products/products", "cataloguer", map[string]any{"company": acme.ID, "sku": "MONT", "name": "Montaje", "kind": "service", "uom": "h",
		"stocked": true}, nil), 400, "a service is not stocked")
	h.must(h.do("POST", "/api/products/products", "cataloguer", map[string]any{"company": acme.ID, "sku": "MONT", "name": "Montaje", "kind": "service", "uom": "h",
		"taxCode": "G21", "basePrice": "35", "forSale": true}, &montaje), 201, "service")

	// A kit of ten screws and ten nuts; a component cannot contain its kit.
	h.must(h.do("POST", "/api/products/products", "cataloguer", map[string]any{"company": acme.ID, "sku": "KIT-M8", "name": "Kit M8 x10", "kind": "good",
		"uom": "caja", "basePrice": "1.50", "forSale": true, "stocked": true, "components": []map[string]any{{"product": tornillo.ID, "quantity": "10"},
			{"product": tuerca.ID, "quantity": "10"}}}, &kit), 201, "kit")
	h.must(h.do("POST", "/api/products/products", "cataloguer", map[string]any{"company": acme.ID, "sku": "KIT-X", "name": "Kit", "kind": "good", "uom": "caja",
		"components": []map[string]any{{"product": montaje.ID, "quantity": "1"}}}, nil), 422, "a service is not a component")
	change := merge(screw, map[string]any{"components": []map[string]any{{"product": kit.ID, "quantity": "1"}}})
	delete(change, "company")
	delete(change, "sku")
	h.must(h.do("PUT", "/api/products/products/"+tornillo.ID, "cataloguer", change, nil), 422, "kit cycle")
	change["components"], change["tracking"], change["name"] = nil, "lot", "Tornillo M8 zincado"
	h.must(h.do("PUT", "/api/products/products/"+tornillo.ID, "cataloguer", change, &tornillo), 200, "change")
	if tornillo.Tracking != "lot" || tornillo.Name != "Tornillo M8 zincado" || tornillo.Version != 2 {
		t.Fatalf("changed: %+v", tornillo)
	}
	// Globex has its own catalog: the same SKU and barcode are free there.
	_, err := h.prod.Service.RegisterProduct.Handle(ctx, dapp.RegisterProduct{Company: globex.ID, SKU: "TOR-M8", DetailsInput: dapp.DetailsInput{
		Name: "Tornillo", Kind: "good", UoM: "ea", Barcodes: []dapp.BarcodeInput{{Type: "ean-13", Value: "8412345678905"}}}})
	h.ok(err)

	search := func(query string) []dapp.ProductDTO {
		var page fw.Page[dapp.ProductDTO]
		h.must(h.do("GET", "/api/products/products?company="+acme.ID+"&"+query, "pricer", nil, &page), 200, "search "+query)
		return page.Items
	}
	if r := search(""); len(r) != 4 || r[0].SKU != "KIT-M8" {
		t.Fatalf("catalog: %+v", r)
	}
	if r := search("q=zincado"); len(r) != 1 || r[0].ID != tornillo.ID {
		t.Fatalf("by name: %+v", r)
	}
	if r := search("q=tue-m8"); len(r) != 1 || r[0].ID != tuerca.ID {
		t.Fatalf("by SKU: %+v", r)
	}
	if r := search("barcode=8412345678905"); len(r) != 1 || r[0].ID != tornillo.ID {
		t.Fatalf("by barcode: %+v", r)
	}
	if r := search("kind=service"); len(r) != 1 || r[0].ID != montaje.ID {
		t.Fatalf("services: %+v", r)
	}
	if r := search("category=" + torn.ID); len(r) != 2 {
		t.Fatalf("by category: %+v", r)
	}
	h.must(h.do("GET", "/api/products/products/"+tornillo.ID, "outsider", nil, nil), 404, "outsider")

	// Prices: a list with volume tiers and validity; without a line, the base price.
	var list dapp.PriceListDTO
	h.must(h.do("POST", "/api/products/price-lists", "cataloguer", map[string]any{"company": acme.ID, "code": "mayor", "name": "Mayoristas"}, nil), 403,
		"cataloguing is not pricing")
	h.must(h.do("POST", "/api/products/price-lists", "pricer", map[string]any{"company": acme.ID, "code": "mayor", "name": "Mayoristas"}, &list), 201, "price list")
	h.must(h.do("POST", "/api/products/price-lists", "pricer", map[string]any{"company": acme.ID, "code": "MAYOR", "name": "Otra"}, nil), 422, "list code once")
	prices := "/api/products/price-lists/" + list.ID + "/prices"
	h.must(h.do("POST", prices, "pricer", map[string]any{"product": tornillo.ID, "unitPrice": "0.10", "from": "2026-01-01", "to": "2026-12-31"}, nil), 200, "price")
	h.must(h.do("POST", prices, "pricer", map[string]any{"product": tornillo.ID, "minQuantity": "1000", "unitPrice": "0.08", "discount": "5", "from": "2026-01-01"},
		&list), 200, "volume price")
	h.must(h.do("POST", prices, "pricer", map[string]any{"product": tornillo.ID, "unitPrice": "0.09", "from": "2026-06-01"}, nil), 422, "overlap")
	h.must(h.do("POST", prices, "pricer", map[string]any{"product": tornillo.ID, "unitPrice": "0.09", "discount": "120", "from": "2027-01-01"}, nil), 400, "discount")
	h.must(h.do("POST", prices, "pricer", map[string]any{"product": fw.NewUUID().String(), "unitPrice": "1", "from": "2026-01-01"}, nil), 404, "unknown product")
	if len(list.Lines) != 2 {
		t.Fatalf("lines: %+v", list.Lines)
	}
	quote := func(query string) contracts.Quote {
		var q contracts.Quote
		h.must(h.do("GET", "/api/products/quote?company="+acme.ID+"&product="+tornillo.ID+"&"+query, "pricer", nil, &q), 200, "quote "+query)
		return q
	}
	if q := quote("priceList=" + list.ID + "&quantity=10&on=2026-10-04"); q.Net != "0.1000" || q.Source != "list" {
		t.Fatalf("small quantity: %+v", q)
	}
	if q := quote("priceList=" + list.ID + "&quantity=1000&on=2026-10-04"); q.UnitPrice != "0.0800" || q.Discount != "5.00" || q.Net != "0.0760" {
		t.Fatalf("volume: %+v", q)
	}
	if q := quote("priceList=" + list.ID + "&quantity=10&on=2027-02-01"); q.Net != "0.1250" || q.Source != "base" {
		t.Fatalf("after the validity: %+v", q)
	}
	if q := quote("quantity=10&on=2026-10-04"); q.Net != "0.1250" || q.Source != "base" {
		t.Fatalf("base: %+v", q)
	}
	h.must(h.do("GET", "/api/products/quote?company="+acme.ID+"&product="+tornillo.ID+"&quantity=0&on=2026-10-04", "pricer", nil, nil), 400, "quantity")
	h.must(h.do("PUT", "/api/products/price-lists/"+list.ID+"/active", "pricer", map[string]any{"active": false}, nil), 200, "retire list")
	if q := quote("priceList=" + list.ID + "&quantity=10&on=2026-10-04"); q.Source != "base" {
		t.Fatalf("retired list: %+v", q)
	}
	h.must(h.do("POST", prices+"/remove", "pricer", map[string]any{"product": tornillo.ID, "minQuantity": "1000", "from": "2026-01-01"}, &list), 200, "remove price")
	if len(list.Lines) != 1 {
		t.Fatalf("after removal: %+v", list.Lines)
	}

	// A product is discontinued, never deleted; other contexts see it through the Catalog port.
	h.must(h.do("POST", "/api/products/products/"+tuerca.ID+"/discontinue", "cataloguer", map[string]any{"on": "2026-11-01"}, &tuerca), 200, "discontinue")
	h.must(h.do("POST", "/api/products/products/"+tuerca.ID+"/discontinue", "cataloguer", nil, nil), 422, "discontinued once")
	refs, err := h.prod.Catalog.Products(context.Background(), []string{tornillo.ID, tuerca.ID, fw.NewUUID().String(), "x"})
	h.ok(err)
	if len(refs) != 2 || refs[tuerca.ID].Discontinued != "2026-11-01" || refs[tornillo.ID].Tracking != "lot" || !refs[tornillo.ID].Stocked ||
		refs[tornillo.ID].ExpenseCategory != "goods" || refs[tornillo.ID].StandardCost != "0.0400" {
		t.Fatalf("catalog port: %+v", refs)
	}
}

func TestProducts_CatalogAndPrices_MemoryThenSQLite(t *testing.T) {
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
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{pinfra.Migrations(), dinfra.Migrations()})
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

func TestProducts_LayersRespectTheArchitecture(t *testing.T) {
	const m = "github.com/jhermoso/karpo-fw-go"
	archtest.AssertOnlyImports(t, "domain", false, archtest.Std, m+"/pkg/domain/...", m+"/contexts/products/domain")
	archtest.AssertOnlyImports(t, "contracts", false, archtest.Std)
	archtest.AssertTreeDoesNotImport(t, "application", []string{"/pkg/persistence", "/pkg/distribution", "database/sql", "net/http"})
}
