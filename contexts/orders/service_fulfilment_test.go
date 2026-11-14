package orders_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"sync"
	"testing"

	iapp "github.com/jhermoso/karpo-fw-go/contexts/inventory/application"
	iinfra "github.com/jhermoso/karpo-fw-go/contexts/inventory/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/orders"
	oapp "github.com/jhermoso/karpo-fw-go/contexts/orders/application"
	odomain "github.com/jhermoso/karpo-fw-go/contexts/orders/domain"
	oinfra "github.com/jhermoso/karpo-fw-go/contexts/orders/infrastructure"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	pinfra "github.com/jhermoso/karpo-fw-go/contexts/parties/infrastructure"
	dapp "github.com/jhermoso/karpo-fw-go/contexts/products/application"
	ddomain "github.com/jhermoso/karpo-fw-go/contexts/products/domain"
	dinfra "github.com/jhermoso/karpo-fw-go/contexts/products/infrastructure"
	rinfra "github.com/jhermoso/karpo-fw-go/contexts/receivables/infrastructure"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
)

// El ajuste de D-155 se resuelve producto, si no empresa, si no albarán.
func TestServiceFulfilment_ResolutionOrder(t *testing.T) {
	for _, c := range []struct{ product, company, want string }{
		{"", "", odomain.ServiceFulfilmentDeliveryNote},
		{"", "direct_invoice", "direct_invoice"},
		{"delivery_note", "direct_invoice", "delivery_note"},
		{"direct_invoice", "", "direct_invoice"},
		{"direct_invoice", "delivery_note", "direct_invoice"},
	} {
		if got := odomain.ResolveServiceFulfilment(c.product, c.company); got != c.want {
			t.Errorf("product %q, company %q: %q, want %q", c.product, c.company, got, c.want)
		}
	}
}

func violation(t *testing.T, err error, code string) {
	t.Helper()
	var v *fw.RuleViolationError
	if !errors.As(err, &v) || v.Code != code {
		t.Fatalf("want violation %s, got %v", code, err)
	}
}

// fakeFulfilment hace de Products y Parties: qué productos son servicios, con su ajuste, y el de la
// empresa. direct_invoice aún no se puede guardar en ninguno de los dos, así que se inyecta aquí.
type fakeFulfilment struct {
	mu       sync.Mutex
	services map[odomain.ProductID]string
	company  string
	fail     error
	calls    int
}

func (f *fakeFulfilment) Services(_ context.Context, ids []odomain.ProductID) (map[odomain.ProductID]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.fail != nil {
		return nil, f.fail
	}
	out := map[odomain.ProductID]string{}
	for _, id := range ids {
		if m, ok := f.services[id]; ok {
			out[id] = m
		}
	}
	return out, nil
}

func (f *fakeFulfilment) CompanyMode(context.Context, odomain.OrganizationID) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.company, f.fail
}

func (f *fakeFulfilment) set(fn func(*fakeFulfilment)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

// call envía una petición y devuelve su estado y el código de error del cuerpo, si lo hay.
func (h *host) call(method, path, user string, body, out any) (int, string) {
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
	if res.StatusCode < 300 {
		if out != nil {
			h.ok(json.NewDecoder(res.Body).Decode(out))
		}
		return res.StatusCode, ""
	}
	var p struct {
		Code string `json:"code"`
	}
	_ = json.NewDecoder(res.Body).Decode(&p)
	return res.StatusCode, p.Code
}

func (h *host) mustCode(status int, code string, wantStatus int, wantCode, what string) {
	h.t.Helper()
	if status != wantStatus || code != wantCode {
		h.t.Fatalf("%s: %d %q, want %d %q", what, status, code, wantStatus, wantCode)
	}
}

// fulfilmentScenario: direct_invoice no se admite aún en la empresa ni en el producto; inyectado
// por el puerto, los servicios en direct_invoice no van al albarán y pedirlos da 422.
func (h *host) fulfilmentScenario(tag string, f *fakeFulfilment) {
	t := h.t
	ctx := h.adminCtx
	acme, err := h.parties.Service.RegisterOrganization.Handle(ctx, papp.RegisterOrganization{LegalName: "Acme F " + tag,
		Roles: []string{pdomain.RoleInternalOrganization.String()}})
	h.ok(err)
	ana, err := h.parties.Service.RegisterPerson.Handle(ctx, papp.RegisterPerson{GivenName: "Ana", FirstSurname: "Fuentes " + tag})
	h.ok(err)
	h.grant("seller", acme.ID)
	h.grant("manager", acme.ID)
	h.grant("dispatcher", acme.ID)
	acmeID, _ := pdomain.ParsePartyID(acme.ID)

	// La empresa: un valor desconocido no vale, direct_invoice aún no, delivery_note sí.
	set := func(mode string) error {
		_, err := h.parties.Service.SetInternalOrganizationProfile.Handle(ctx, papp.SetInternalOrganizationProfile{ID: acmeID, ServiceFulfilmentMode: mode})
		return err
	}
	violation(t, set("factura"), pdomain.ErrInvalidServiceFulfilmentMode)
	violation(t, set("direct_invoice"), pdomain.ErrDirectInvoiceUnavailable)
	h.ok(set("delivery_note"))

	// El producto, igual.
	register := func(sku, kind, mode string, stocked bool) (dapp.ProductDTO, error) {
		return h.prod.Service.RegisterProduct.Handle(ctx, dapp.RegisterProduct{Company: acme.ID, SKU: sku, DetailsInput: dapp.DetailsInput{
			Name: sku + " " + tag, Kind: kind, UoM: "h", TaxCode: "G21", BasePrice: "10", ForSale: true, Stocked: stocked, ServiceFulfilmentMode: mode}})
	}
	_, err = register("BAD", "service", "albaran", false)
	violation(t, err, ddomain.ErrInvalidServiceFulfilmentMode)
	_, err = register("DIR", "service", "direct_invoice", false)
	violation(t, err, ddomain.ErrDirectInvoiceUnavailable)
	product := func(sku, kind, mode string, stocked bool) dapp.ProductDTO {
		p, err := register(sku, kind, mode, stocked)
		h.ok(err)
		return p
	}
	montaje := product("MONT", "service", "", false)
	revision := product("REV", "service", "delivery_note", false)
	soporte := product("SOP", "service", "", false)
	tornillo := product("TOR", "good", "", true)
	pid := func(p dapp.ProductDTO) odomain.ProductID {
		id, _ := fw.ParseUUID(p.ID)
		return odomain.ProductID{UUID: id}
	}
	f.set(func(f *fakeFulfilment) {
		f.services = map[odomain.ProductID]string{pid(montaje): "", pid(revision): "delivery_note", pid(soporte): "direct_invoice"}
		f.company, f.fail = "", nil
	})
	wh, err := h.inv.Service.CreateWarehouse.Handle(ctx, iapp.CreateWarehouse{Company: acme.ID, Code: "FUL", Name: "Servicios"})
	h.ok(err)

	order := func(products ...string) oapp.OrderDTO {
		var o oapp.OrderDTO
		h.must(h.do("POST", "/api/orders/orders", "seller", map[string]any{"company": acme.ID, "customer": ana.ID, "date": "2026-10-04", "warehouse": wh.ID}, &o),
			201, "draft")
		for _, p := range products {
			h.must(h.do("POST", "/api/orders/orders/"+o.ID+"/lines", "seller", map[string]any{"product": p, "quantity": "1"}, nil), 200, "line")
		}
		h.must(h.do("POST", "/api/orders/orders/"+o.ID+"/confirm", "manager", nil, &o), 200, "confirm")
		return o
	}
	deliver := func(o oapp.OrderDTO, body map[string]any, out *oapp.DeliveryDTO) (int, string) {
		return h.call("POST", "/api/orders/orders/"+o.ID+"/deliveries", "dispatcher", body, out)
	}
	pick := func(line int) map[string]any {
		return map[string]any{"date": "2026-10-05", "lines": []map[string]any{{"line": line, "quantity": "1"}}}
	}
	all := map[string]any{"date": "2026-10-05"}

	// Sin ajuste de empresa: montaje y revisión van al albarán; soporte, no.
	o := order(montaje.ID, soporte.ID, revision.ID)
	s, c := deliver(o, pick(2), nil)
	h.mustCode(s, c, 422, "orders.service_direct_invoice", "pick a direct invoice service")
	var note oapp.DeliveryDTO
	s, c = deliver(o, all, &note)
	h.mustCode(s, c, 201, "", "deliver")
	if len(note.Lines) != 2 || note.Lines[0].Line != 1 || note.Lines[1].Line != 3 {
		t.Fatalf("delivery without company setting: %+v", note.Lines)
	}

	// La empresa factura directamente: el montaje (sin ajuste) deja de ir al albarán; la revisión,
	// que lo dice el producto, sigue yendo.
	f.set(func(f *fakeFulfilment) { f.company = "direct_invoice" })
	o = order(montaje.ID, revision.ID)
	s, c = deliver(o, pick(1), nil)
	h.mustCode(s, c, 422, "orders.service_direct_invoice", "pick a service of a direct invoice company")
	s, c = deliver(o, all, &note)
	h.mustCode(s, c, 201, "", "deliver")
	if len(note.Lines) != 1 || note.Lines[0].Line != 2 {
		t.Fatalf("delivery with direct_invoice company: %+v", note.Lines)
	}

	// Si los ajustes no se pueden leer: un pedido sin líneas sin stock no los consulta (falla por
	// lo suyo, que nada está retenido); uno con servicios falla.
	f.set(func(f *fakeFulfilment) { f.fail, f.calls = errors.New("settings unavailable"), 0 })
	goods := order(tornillo.ID)
	s, c = deliver(goods, pick(1), nil)
	h.mustCode(s, c, 422, "orders.not_reserved", "only stocked lines: no settings lookup")
	if f.calls != 0 {
		t.Fatalf("settings consulted for an order with only stocked lines: %d calls", f.calls)
	}
	o = order(montaje.ID)
	if s, _ := deliver(o, all, nil); s != http.StatusInternalServerError {
		t.Fatalf("delivery with failing settings: %d, want 500", s)
	}
	f.set(func(f *fakeFulfilment) { f.fail = nil })
}

func TestServiceFulfilment_DeliveryLeavesDirectInvoiceServicesOut_MemoryThenSQLite(t *testing.T) {
	f := &fakeFulfilment{}
	h := compose(t, orders.WithServiceFulfilment(f))
	h.fulfilmentScenario("m", f)

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
	h.fulfilmentScenario("s", f)
}
