package shipments_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/shipments"
	sinfra "github.com/jhermoso/karpo-fw-go/contexts/shipments/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
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

// El contrato de mantenimiento de los transportistas, el que lee el motor de Angular de Karpo: alta
// con una propiedad de más (se ignora) y Location; lectura; lista por GET y por POST …/search con la
// página del motor, filtros, búsqueda, orden y 400 con lo que no es un orden; cambio con su versión,
// 409 con la de antes y 400 sin ninguna; lo que no cambia, 400 si cambia y comparado por su valor;
// atomicidad; DELETE que bloquea y se puede repetir; y el permiso y el ámbito de siempre.
func (h *host) carrierMaintenance() {
	t := h.t
	acme, globex := fw.NewUUID().String(), fw.NewUUID().String()
	h.grant("planner", acme)
	h.grant("viewer", acme)
	h.grant("outsider", globex)
	base := shipments.MaintenanceBase
	status := func(user, method, path string, body any) int { s, _, _ := h.raw(method, path, user, body); return s }
	carrier := func(code, name string) map[string]any {
		return map[string]any{"company": acme, "code": code, "name": name, "trackingUrl": "https://track.example/{tracking}"}
	}

	// Alta.
	in := carrier("seur", "SEUR Norte")
	in["createdBy"] = "nadie"
	code, created, header := h.raw("POST", base, "planner", in)
	if code != 201 || created["code"] != "SEUR" || created["blocked"] != false || header.Get("Location") != base+"/"+created["id"].(string) {
		t.Fatalf("create: %d %v %v", code, created, header)
	}
	id := created["id"].(string)
	for i, name := range []string{"MRW Este", "GLS Oeste", "DHL Sur"} {
		h.must(status("planner", "POST", base, carrier("C-0"+string(rune('2'+i)), name)), 201, "create "+name)
	}
	other := carrier("UPS", "UPS Ajeno")
	other["company"] = globex
	h.must(status("outsider", "POST", base, other), 201, "another organization's carrier")

	// Errores: el diccionario de campos, con sus códigos.
	bad := carrier("C-09", "X")
	bad["trackingUrl"] = "http://sin-marcador"
	code, problem, header := h.raw("POST", base, "planner", bad)
	errs, _ := problem["errors"].(map[string]any)
	codes, _ := problem["errorCodes"].(map[string]any)
	if code != 400 || errs["trackingUrl"] == nil || codes["trackingUrl"].([]any)[0] != "format" ||
		!strings.HasPrefix(header.Get("Content-Type"), "application/problem+json") {
		t.Fatalf("validation: %d %v", code, problem)
	}
	h.must(status("planner", "POST", base, "no es json"), 400, "not JSON")
	h.must(status("planner", "POST", base, strings.Repeat(" ", 2<<20)), 413, "too large")
	bad = carrier("C-09", "X")
	bad["blocked"] = true
	h.must(status("planner", "POST", base, bad), 400, "registered blocked")

	// Lectura.
	code, got, _ := h.raw("GET", base+"/"+id, "planner", nil)
	if code != 200 || got["name"] != "SEUR Norte" || got["trackingUrl"] != "https://track.example/{tracking}" {
		t.Fatalf("get: %d %v", code, got)
	}
	h.must(status("planner", "GET", base+"/nada", nil), 400, "not an id")

	// Lista por GET y por POST: la página del motor, filtros, búsqueda y orden.
	names := func(page map[string]any) string {
		var out []string
		for _, it := range page["items"].([]any) {
			out = append(out, it.(map[string]any)["name"].(string))
		}
		return strings.Join(out, ",")
	}
	code, page, _ := h.raw("GET", base+"?sortBy=name&sortOrder=desc&pageSize=2&page=2", "planner", nil)
	if code != 200 || names(page) != "GLS Oeste,DHL Sur" || page["totalCount"] != float64(4) || page["total"] != float64(4) ||
		page["pageNumber"] != float64(2) || page["pageSize"] != float64(2) || page["totalPages"] != float64(2) {
		t.Fatalf("list: %d %v", code, page)
	}
	if _, page, _ = h.raw("GET", base+"?search=oeste&isActive=true", "planner", nil); names(page) != "GLS Oeste" {
		t.Fatalf("search: %v", page)
	}
	// El filtro de empresa no enseña otra organización.
	if code, page, _ = h.raw("GET", base+"?company="+globex, "planner", nil); code != 200 || page["totalCount"] != float64(0) {
		t.Fatalf("another company: %d %v", code, page)
	}
	for _, bad := range []string{"&sortBy=party", "&sortOrder=up", "&page=0", "&pageSize=1001"} {
		h.must(status("planner", "GET", base+"?company="+acme+bad, nil), 400, bad)
	}
	h.must(status("planner", "GET", base+"?company=nada", nil), 400, "a company that is not an id")
	code, page, _ = h.raw("POST", base+"/search", "planner", map[string]any{"filter": map[string]any{"company": acme},
		"pagination": map[string]any{"pageNumber": 1, "pageSize": 3, "orderBy": "code", "ascending": false}})
	if code != 200 || names(page) != "SEUR Norte,DHL Sur,GLS Oeste" || page["totalCount"] != float64(4) {
		t.Fatalf("search by POST: %d %v", code, page)
	}
	h.must(status("planner", "POST", base+"/search", map[string]any{"filter": map[string]any{"name": []string{"a"}}}), 400, "a filter that is not a single value")
	// Como en GET, una página fuera de rango da 400; sin número se pide la primera.
	for _, n := range []int{0, -1} {
		h.must(status("planner", "POST", base+"/search", map[string]any{"pagination": map[string]any{"pageNumber": n}}), 400, "a page out of range")
	}
	h.must(status("planner", "POST", base+"/search", map[string]any{"pagination": map[string]any{"pageSize": 3}}), 200, "no page number")

	// Cambio: lo leído con su versión.
	got["name"] = "SEUR Norte 2"
	code, changed, _ := h.raw("PUT", base+"/"+id, "planner", got)
	if code != 200 || changed["name"] != "SEUR Norte 2" || changed["version"] != got["version"].(float64)+1 {
		t.Fatalf("update: %d %v", code, changed)
	}
	// Como el modal de la rejilla: sólo los campos del formulario y la versión; lo demás se queda,
	// también lo que llega como null.
	code, changed, _ = h.raw("PUT", base+"/"+id, "planner", map[string]any{"trackingUrl": "https://seur.example/t/{tracking}", "name": nil,
		"version": changed["version"]})
	if code != 200 || changed["trackingUrl"] != "https://seur.example/t/{tracking}" || changed["name"] != "SEUR Norte 2" {
		t.Fatalf("an update with only some fields: %d %v", code, changed)
	}
	h.must(status("planner", "PUT", base+"/"+id, got), 409, "the version read before")
	// El código se compara tal cual: cambiarle sólo las mayúsculas también es cambiarlo.
	for _, c := range []struct{ field, value string }{{"code", "SEUR2"}, {"code", "seur"}, {"company", globex}} {
		field := c.field
		code, problem, _ = h.raw("PUT", base+"/"+id, "planner", map[string]any{field: c.value, "version": changed["version"]})
		if errs, _ := problem["errors"].(map[string]any); code != 400 || errs[field] == nil || problem["errorCodes"].(map[string]any)[field].([]any)[0] != "immutable" {
			t.Fatalf("%s, which no use case changes: %d %v", field, code, problem)
		}
	}
	delete(got, "version")
	code, problem, _ = h.raw("PUT", base+"/"+id, "planner", got)
	if errs, _ := problem["errors"].(map[string]any); code != 400 || errs["version"] == nil {
		t.Fatalf("without a version: %d %v", code, problem)
	}
	// Lo que no cambia se compara por su valor: la identidad en otras mayúsculas, el código y el
	// nombre con espacios en los extremos.
	v := changed["version"]
	code, changed, _ = h.raw("PUT", base+"/"+id, "planner", map[string]any{"company": strings.ToUpper(acme), "code": " SEUR ", "name": " SEUR Norte 2 ",
		"version": v})
	if code != 200 || changed["version"] != v {
		t.Fatalf("an update that changes nothing: %d %v", code, changed)
	}
	// Aunque no cambie nada, cambiar exige poder cambiar.
	h.must(status("viewer", "PUT", base+"/"+id, map[string]any{"version": v}), 403, "an empty change by a viewer")

	// Atómico: si el cambio falla (aquí, un enlace sin marcador), no queda nada, ni el nombre ni la
	// versión.
	code, problem, _ = h.raw("PUT", base+"/"+id, "planner", map[string]any{"name": "Otro nombre", "trackingUrl": "https://sin-marcador", "version": v})
	if code != 400 || problem["errorCodes"].(map[string]any)["trackingUrl"] == nil {
		t.Fatalf("a failing change: %d %v", code, problem)
	}
	if _, got, _ = h.raw("GET", base+"/"+id, "planner", nil); got["name"] != "SEUR Norte 2" || got["version"] != v {
		t.Fatalf("nothing of a failing change is kept: %v", got)
	}
	// El nombre y el enlace se guardan sin los espacios de los extremos.
	code, changed, _ = h.raw("PUT", base+"/"+id, "planner", map[string]any{"name": " SEUR Norte 3 ",
		"trackingUrl": " https://seur.example/s/{tracking} ", "version": v})
	if code != 200 || changed["name"] != "SEUR Norte 3" || changed["trackingUrl"] != "https://seur.example/s/{tracking}" {
		t.Fatalf("a change with spaces at the ends: %d %v", code, changed)
	}
	v = changed["version"]

	// DELETE bloquea: exige el permiso y el ámbito de escritura, y se puede repetir sin versión nueva.
	h.must(status("viewer", "DELETE", base+"/"+id, nil), 403, "delete without the permission")
	h.must(status("planner", "DELETE", base+"/"+id, nil), 204, "delete")
	_, blocked, _ := h.raw("GET", base+"/"+id, "planner", nil)
	if blocked["blocked"] != true || blocked["version"] != v.(float64)+1 {
		t.Fatalf("delete blocks: %v", blocked)
	}
	h.must(status("planner", "DELETE", base+"/"+id, nil), 204, "delete again")
	if _, again, _ := h.raw("GET", base+"/"+id, "planner", nil); again["version"] != blocked["version"] {
		t.Fatalf("delete again changes nothing: %v", again)
	}
	h.must(status("viewer", "DELETE", base+"/"+id, nil), 403, "delete again without the permission")
	if _, page, _ = h.raw("GET", base+"?isActive=true", "planner", nil); page["totalCount"] != float64(3) {
		t.Fatalf("a blocked carrier is not active: %v", page)
	}
	s, _, _ := h.dir.Subject(context.Background(), h.ids["outsider"])
	s.Grants = append(s.Grants, authz.Grant{OrganizationID: fw.MustParseUUID(acme), Level: authz.ReadOnly})
	h.dir.Put(h.ids["outsider"], s)
	h.must(status("outsider", "DELETE", base+"/"+id, nil), 403, "delete read-only")
	h.must(status("outsider", "PUT", base+"/"+id, map[string]any{"version": blocked["version"]}), 403, "change read-only")
	s.Grants = s.Grants[:len(s.Grants)-1]
	h.dir.Put(h.ids["outsider"], s)

	// Permiso y ámbito.
	h.must(status("outsider", "GET", base+"/"+id, nil), 404, "another organization")
	h.must(status("outsider", "DELETE", base+"/"+id, nil), 404, "delete in another organization")
	h.must(status("dock", "GET", base, nil), 403, "without the permission")
	h.must(status("", "GET", base, nil), 401, "nobody")
}

func TestShipments_CarrierMaintenanceContract_MemoryThenSQLite(t *testing.T) {
	h := compose(t)
	h.carrierMaintenance()
	ctx := context.Background()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "front.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{sinfra.Migrations()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.sw.Swap(ctx, db); err != nil {
		t.Fatal(err)
	}
	h.carrierMaintenance()
}
