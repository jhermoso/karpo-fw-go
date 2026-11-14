package products_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	pinfra "github.com/jhermoso/karpo-fw-go/contexts/parties/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/products"
	dapp "github.com/jhermoso/karpo-fw-go/contexts/products/application"
	ddomain "github.com/jhermoso/karpo-fw-go/contexts/products/domain"
	dinfra "github.com/jhermoso/karpo-fw-go/contexts/products/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
)

// raw hace una petición y devuelve el estado, el cuerpo como JSON genérico y las cabeceras.
func (h *host) raw(method, path, user string, body any) (int, map[string]any, http.Header) {
	h.t.Helper()
	var buf bytes.Buffer
	switch b := body.(type) {
	case nil:
	case string:
		buf.WriteString(b)
	default:
		_ = json.NewEncoder(&buf).Encode(b)
	}
	req, _ := http.NewRequest(method, h.srv.URL+path, &buf)
	req.Header.Set("Authorization", h.tokens[user])
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out, res.Header
}

// El contrato de mantenimiento de los productos, el que lee el motor de Angular de Karpo: alta con
// una propiedad de más (se ignora) y Location; lectura; lista por GET y por POST …/search con la
// página del motor, filtros, búsqueda, orden en los dos sentidos y 400 con lo que no es un orden;
// cambio devolviendo lo leído con su versión, 409 con la de antes y 400 sin ninguna, conservando
// los códigos de barras; errores como diccionario; baja que retira y se puede repetir; y el permiso
// y el ámbito de siempre.
func (h *host) maintenance(tag string) {
	t := h.t
	acme, err := h.parties.Service.RegisterOrganization.Handle(h.adminCtx, papp.RegisterOrganization{LegalName: "Acme Front " + tag,
		Roles: []string{pdomain.RoleInternalOrganization.String()}})
	h.ok(err)
	h.grant("cataloguer", acme.ID)
	base := products.MaintenanceBase

	// Alta.
	status, created, header := h.raw("POST", base, "cataloguer", map[string]any{"company": acme.ID, "sku": "TOR-8", "name": "Tornillo M8", "kind": "good",
		"uom": "ea", "basePrice": "0.12", "forSale": true, "createdBy": "nadie"})
	if status != 201 || created["sku"] != "TOR-8" || created["name"] != "Tornillo M8" || header.Get("Location") != base+"/"+created["id"].(string) ||
		created["version"] != float64(1) {
		t.Fatalf("create: %d %v %v", status, created, header)
	}
	id := created["id"].(string)
	for i, name := range []string{"Arandela", "Broca", "Clavo"} {
		status, _, _ := h.raw("POST", base, "cataloguer", map[string]any{"company": acme.ID, "sku": "X-" + string(rune('A'+i)), "name": name,
			"kind": "good", "uom": "ea"})
		h.must(status, 201, "create "+name)
	}

	// Errores: el diccionario de campos, y la regla con su código.
	status, problem, header := h.raw("POST", base, "cataloguer", map[string]any{"company": acme.ID, "sku": "TOR-9", "kind": "gas", "uom": "ea"})
	errs, _ := problem["errors"].(map[string]any)
	codes, _ := problem["errorCodes"].(map[string]any)
	if status != 400 || errs["kind"] == nil || codes["kind"].([]any)[0] != "enum" || !strings.HasPrefix(header.Get("Content-Type"), "application/problem+json") {
		t.Fatalf("validation: %d %v", status, problem)
	}
	status, problem, _ = h.raw("POST", base, "cataloguer", map[string]any{"company": acme.ID, "sku": "tor-8", "name": "Otro", "kind": "good", "uom": "ea"})
	if status != 422 || problem["code"] == nil || problem["errors"] != nil {
		t.Fatalf("a repeated SKU: %d %v", status, problem)
	}
	h.must(func() int { s, _, _ := h.raw("POST", base, "cataloguer", "no es json"); return s }(), 400, "not JSON")

	// Lectura.
	status, got, _ := h.raw("GET", base+"/"+id, "cataloguer", nil)
	if status != 200 || got["basePrice"] != "0.1200" || got["barcodes"] != nil {
		t.Fatalf("get: %d %v", status, got)
	}
	h.must(func() int { s, _, _ := h.raw("GET", base+"/nada", "cataloguer", nil); return s }(), 400, "not an id")

	// Lista por GET y por POST: la página del motor, filtros, búsqueda y orden.
	names := func(page map[string]any) string {
		var out []string
		for _, it := range page["items"].([]any) {
			out = append(out, it.(map[string]any)["name"].(string))
		}
		return strings.Join(out, ",")
	}
	q := "?company=" + acme.ID
	status, page, _ := h.raw("GET", base+q+"&sortBy=name&sortOrder=desc&pageSize=2&page=2", "cataloguer", nil)
	if status != 200 || names(page) != "Broca,Arandela" || page["totalCount"] != float64(4) || page["total"] != float64(4) || page["pageNumber"] != float64(2) ||
		page["pageSize"] != float64(2) || page["totalPages"] != float64(2) {
		t.Fatalf("list: %d %v", status, page)
	}
	if _, page, _ = h.raw("GET", base+q+"&search=tornillo&isActive=true", "cataloguer", nil); names(page) != "Tornillo M8" {
		t.Fatalf("search: %v", page)
	}
	for _, bad := range []string{"&sortBy=basePrice", "&sortOrder=up", "&page=0", "&pageSize=1001"} {
		h.must(func() int { s, _, _ := h.raw("GET", base+q+bad, "cataloguer", nil); return s }(), 400, bad)
	}
	status, page, _ = h.raw("POST", base+"/search", "cataloguer", map[string]any{"filter": map[string]any{"company": acme.ID, "isActive": true, "kind": "good"},
		"pagination": map[string]any{"pageNumber": 1, "pageSize": 3, "orderBy": "name", "ascending": true}})
	if status != 200 || names(page) != "Arandela,Broca,Clavo" || page["totalCount"] != float64(4) {
		t.Fatalf("search by POST: %d %v", status, page)
	}
	// Como en GET, una página fuera de rango da 400.
	for _, n := range []int{0, -1} {
		if code, _, _ := h.raw("POST", base+"/search", "cataloguer", map[string]any{"pagination": map[string]any{"pageNumber": n}}); code != 400 {
			t.Fatalf("search by POST, page %d: %d", n, code)
		}
	}
	h.must(func() int {
		s, _, _ := h.raw("POST", base+"/search", "cataloguer", map[string]any{"filter": map[string]any{"name": []string{"a"}}})
		return s
	}(), 400, "a filter that is not a single value")

	// Cambio: lo leído con su versión; los códigos de barras se conservan.
	_, err = h.prod.Service.ChangeProduct.Handle(h.adminCtx, changeWithBarcode(got))
	h.ok(err)
	_, got, _ = h.raw("GET", base+"/"+id, "cataloguer", nil)
	got["name"] = "Tornillo M8 zincado"
	status, changed, _ := h.raw("PUT", base+"/"+id, "cataloguer", got)
	if status != 200 || changed["name"] != "Tornillo M8 zincado" || changed["version"] != got["version"].(float64)+1 {
		t.Fatalf("update: %d %v", status, changed)
	}
	full, err := h.prod.Service.GetProduct.Handle(h.adminCtx, getProduct(id))
	if err != nil || len(full.Barcodes) != 1 {
		t.Fatalf("the barcodes are kept: %+v %v", full, err)
	}
	// Como el modal de la rejilla: sólo los campos del formulario y la versión; lo demás se queda.
	status, changed, _ = h.raw("PUT", base+"/"+id, "cataloguer", map[string]any{"name": "Tornillo M8 inox", "version": changed["version"]})
	if status != 200 || changed["name"] != "Tornillo M8 inox" || changed["basePrice"] != "0.1200" || changed["forSale"] != true || changed["sku"] != "TOR-8" {
		t.Fatalf("an update with only some fields: %d %v", status, changed)
	}
	h.must(func() int { s, _, _ := h.raw("PUT", base+"/"+id, "cataloguer", got); return s }(), 409, "the version read before")
	delete(got, "version")
	status, problem, _ = h.raw("PUT", base+"/"+id, "cataloguer", got)
	if errs, _ := problem["errors"].(map[string]any); status != 400 || errs["version"] == nil {
		t.Fatalf("without a version: %d %v", status, problem)
	}

	// Baja: lo retira, y repetirla no es un error.
	h.must(func() int { s, _, _ := h.raw("DELETE", base+"/"+id, "cataloguer", nil); return s }(), 204, "delete")
	h.must(func() int { s, _, _ := h.raw("DELETE", base+"/"+id, "cataloguer", nil); return s }(), 204, "delete again")
	if _, got, _ = h.raw("GET", base+"/"+id, "cataloguer", nil); got["discontinued"] == "" {
		t.Fatalf("discontinued: %v", got)
	}

	// Permiso y ámbito.
	h.must(func() int { s, _, _ := h.raw("GET", base+"/"+id, "dealer", nil); return s }(), 403, "no permission")
	h.must(func() int { s, _, _ := h.raw("GET", base+"/"+id, "outsider", nil); return s }(), 404, "another company")
	h.must(func() int { s, _, _ := h.raw("GET", base, "", nil); return s }(), 401, "nobody")
}

func TestProducts_MaintenanceContract_MemoryThenSQLite(t *testing.T) {
	h := compose(t)
	h.maintenance("m")
	ctx := context.Background()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "front.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	h.ok(err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{pinfra.Migrations(), dinfra.Migrations()})
	h.ok(err)
	_, err = m.Migrate(ctx)
	h.ok(err)
	h.ok(h.sw.Swap(ctx, db))
	h.maintenance("s")
}

func getProduct(id string) dapp.GetProduct {
	pid, _ := ddomain.ParseProductID(id)
	return dapp.GetProduct{ID: pid}
}

// changeWithBarcode le pone un código de barras por la API de siempre, con lo demás como está.
func changeWithBarcode(got map[string]any) dapp.ChangeProduct {
	pid, _ := ddomain.ParseProductID(got["id"].(string))
	s := func(k string) string { x, _ := got[k].(string); return x }
	b := func(k string) bool { x, _ := got[k].(bool); return x }
	return dapp.ChangeProduct{ID: pid, DetailsInput: dapp.DetailsInput{Name: s("name"), Kind: s("kind"), UoM: s("uom"), BasePrice: s("basePrice"),
		ForSale: b("forSale"), Barcodes: []dapp.BarcodeInput{{Type: "ean-13", Value: "8412345678905"}}}}
}
