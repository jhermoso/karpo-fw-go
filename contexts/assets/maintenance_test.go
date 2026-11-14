package assets_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/assets"
	sinfra "github.com/jhermoso/karpo-fw-go/contexts/assets/infrastructure"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	pinfra "github.com/jhermoso/karpo-fw-go/contexts/parties/infrastructure"
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

// El contrato de mantenimiento de los activos, el que lee el motor de Angular de Karpo: alta con una
// propiedad de más (se ignora) y Location; lectura; lista por GET y por POST …/search con la página
// del motor, filtros, búsqueda, orden en los dos sentidos y 400 con lo que no es un orden; cambio
// devolviendo lo leído con su versión, 409 con la de antes y 400 sin ninguna; lo que no cambia, 400
// si cambia y comparado por su valor; atomicidad; baja por PUT; DELETE que sólo confirma una baja;
// y el permiso y el ámbito de siempre.
func (h *host) maintenance(tag string) {
	t := h.t
	org := func(name string) string {
		o, err := h.parties.Service.RegisterOrganization.Handle(h.adminCtx, papp.RegisterOrganization{LegalName: name + " " + tag,
			Roles: []string{pdomain.RoleInternalOrganization.String()}})
		h.ok(err)
		return o.ID
	}
	acme, globex := org("Acme Front"), org("Globex Front")
	h.grant("clerk", acme)
	h.grant("outsider", globex)
	base := assets.MaintenanceBase
	status := func(method, path string, body any) int { s, _, _ := h.raw(method, path, "clerk", body); return s }
	asset := func(code, name string) map[string]any {
		return map[string]any{"company": acme, "code": code, "name": name, "class": "machinery", "acquired": "2026-01-10", "cost": "1200",
			"lifeMonths": 12}
	}

	// Alta.
	in := asset("m-01", "Torno Norte")
	in["createdBy"] = "nadie"
	code, created, header := h.raw("POST", base, "clerk", in)
	if code != 201 || created["code"] != "M-01" || created["status"] != "in-service" || header.Get("Location") != base+"/"+created["id"].(string) {
		t.Fatalf("create: %d %v %v", code, created, header)
	}
	id := created["id"].(string)
	for i, name := range []string{"Torno Este", "Torno Oeste", "Torno Sur"} {
		h.must(status("POST", base, asset("M-0"+string(rune('2'+i)), name)), 201, "create "+name)
	}
	// El activo de otra organización, que la lista de clerk no debe enseñar.
	other := asset("G-01", "Torno Ajeno")
	other["company"] = globex
	h.must(func() int { s, _, _ := h.raw("POST", base, "outsider", other); return s }(), 201, "another organization's asset")

	// Errores: el diccionario de campos, con sus códigos.
	bad := asset("M-09", "X")
	bad["class"] = "boats"
	code, problem, header := h.raw("POST", base, "clerk", bad)
	errs, _ := problem["errors"].(map[string]any)
	codes, _ := problem["errorCodes"].(map[string]any)
	if code != 400 || errs["class"] == nil || codes["class"].([]any)[0] != "enum" || !strings.HasPrefix(header.Get("Content-Type"), "application/problem+json") {
		t.Fatalf("validation: %d %v", code, problem)
	}
	h.must(status("POST", base, "no es json"), 400, "not JSON")
	h.must(status("POST", base, strings.Repeat(" ", 2<<20)), 413, "too large")
	bad = asset("M-09", "X")
	bad["disposed"] = "2026-03-01"
	h.must(status("POST", base, bad), 400, "registered disposed of")

	// Lectura.
	code, got, _ := h.raw("GET", base+"/"+id, "clerk", nil)
	if code != 200 || got["name"] != "Torno Norte" || got["monthly"] != "100.00" {
		t.Fatalf("get: %d %v", code, got)
	}
	h.must(status("GET", base+"/nada", nil), 400, "not an id")

	// Lista por GET y por POST: la página del motor, filtros, búsqueda y orden.
	names := func(page map[string]any) string {
		var out []string
		for _, it := range page["items"].([]any) {
			out = append(out, it.(map[string]any)["name"].(string))
		}
		return strings.Join(out, ",")
	}
	code, page, _ := h.raw("GET", base+"?sortBy=name&sortOrder=desc&pageSize=2&page=2", "clerk", nil)
	if code != 200 || names(page) != "Torno Norte,Torno Este" || page["totalCount"] != float64(4) || page["total"] != float64(4) ||
		page["pageNumber"] != float64(2) || page["pageSize"] != float64(2) || page["totalPages"] != float64(2) {
		t.Fatalf("list: %d %v", code, page)
	}
	if _, page, _ = h.raw("GET", base+"?search=oeste&inService=true", "clerk", nil); names(page) != "Torno Oeste" {
		t.Fatalf("search: %v", page)
	}
	// El filtro de empresa no enseña otra organización.
	if code, page, _ = h.raw("GET", base+"?company="+globex, "clerk", nil); code != 200 || page["totalCount"] != float64(0) {
		t.Fatalf("another company: %d %v", code, page)
	}
	for _, bad := range []string{"&sortBy=cost", "&sortOrder=up", "&page=0", "&pageSize=1001", "&class=boats"} {
		h.must(status("GET", base+"?company="+acme+bad, nil), 400, bad)
	}
	code, page, _ = h.raw("POST", base+"/search", "clerk", map[string]any{"filter": map[string]any{"company": acme, "class": "machinery"},
		"pagination": map[string]any{"pageNumber": 1, "pageSize": 3, "orderBy": "code", "ascending": false}})
	if code != 200 || names(page) != "Torno Sur,Torno Oeste,Torno Este" || page["totalCount"] != float64(4) {
		t.Fatalf("search by POST: %d %v", code, page)
	}
	// Como en GET, una página fuera de rango da 400.
	for _, n := range []int{0, -1} {
		if code, _, _ := h.raw("POST", base+"/search", "clerk", map[string]any{"pagination": map[string]any{"pageNumber": n}}); code != 400 {
			t.Fatalf("search by POST, page %d: %d", n, code)
		}
	}
	h.must(status("POST", base+"/search", map[string]any{"filter": map[string]any{"name": []string{"a"}}}), 400, "a filter that is not a single value")

	// Cambio: lo leído con su versión.
	got["name"] = "Torno Norte 2"
	code, changed, _ := h.raw("PUT", base+"/"+id, "clerk", got)
	if code != 200 || changed["name"] != "Torno Norte 2" || changed["version"] != got["version"].(float64)+1 {
		t.Fatalf("update: %d %v", code, changed)
	}
	// Como el modal de la rejilla: sólo los campos del formulario y la versión; lo demás se queda,
	// también lo que llega como null.
	code, changed, _ = h.raw("PUT", base+"/"+id, "clerk", map[string]any{"serial": "S-1", "cost": nil, "version": changed["version"]})
	if code != 200 || changed["serial"] != "S-1" || changed["name"] != "Torno Norte 2" || changed["cost"] != "1200.00" {
		t.Fatalf("an update with only some fields: %d %v", code, changed)
	}
	h.must(status("PUT", base+"/"+id, got), 409, "the version read before")
	for field, value := range map[string]any{"code": "m-01", "cost": "1300", "lifeMonths": 24, "acquired": "2026-01-11", "company": globex} {
		code, problem, _ = h.raw("PUT", base+"/"+id, "clerk", map[string]any{field: value, "version": changed["version"]})
		if errs, _ := problem["errors"].(map[string]any); code != 400 || errs[field] == nil || problem["errorCodes"].(map[string]any)[field].([]any)[0] != "immutable" {
			t.Fatalf("%s, which no use case changes: %d %v", field, code, problem)
		}
	}
	delete(got, "version")
	code, problem, _ = h.raw("PUT", base+"/"+id, "clerk", got)
	if errs, _ := problem["errors"].(map[string]any); code != 400 || errs["version"] == nil {
		t.Fatalf("without a version: %d %v", code, problem)
	}
	// Lo que no cambia se compara por su valor: identidad en mayúsculas, código con espacios en
	// los extremos, importes con otras cifras.
	v := changed["version"]
	code, changed, _ = h.raw("PUT", base+"/"+id, "clerk", map[string]any{"company": strings.ToUpper(acme), "code": " M-01 ", "cost": "1200.0",
		"residual": "0", "version": v})
	if code != 200 || changed["version"] != v {
		t.Fatalf("an update that changes nothing: %d %v", code, changed)
	}
	// Las fechas se comparan como fechas, y el nombre sin los espacios de los extremos.
	code, changed, _ = h.raw("PUT", base+"/"+id, "clerk", map[string]any{"acquired": " 2026-01-10 ", "name": " Torno Norte 2 ", "version": v})
	if code != 200 || changed["version"] != v {
		t.Fatalf("same date and name, written otherwise: %d %v", code, changed)
	}
	// Aunque no cambie nada, cambiar exige poder cambiar.
	h.grant("viewer", acme)
	h.must(func() int { s, _, _ := h.raw("PUT", base+"/"+id, "viewer", map[string]any{"version": v}); return s }(), 403, "an empty change by a viewer")
	// Lo obtenido sin fecha ni tipo de baja no se ignora.
	code, problem, _ = h.raw("PUT", base+"/"+id, "clerk", map[string]any{"proceeds": "500", "version": v})
	if errs, _ := problem["errors"].(map[string]any); code != 400 || errs["proceeds"] == nil {
		t.Fatalf("proceeds without a disposal: %d %v", code, problem)
	}

	// Atómico: si un paso falla (aquí, la baja con un tipo que no existe), no queda hecho ninguno,
	// ni el nombre ni la versión.
	code, problem, _ = h.raw("PUT", base+"/"+id, "clerk", map[string]any{"name": "Otro nombre", "disposed": "2026-03-15", "disposal": "nada", "version": v})
	if code != 422 || problem["code"] != "assets.disposal_kind" {
		t.Fatalf("a failing step: %d %v", code, problem)
	}
	if _, got, _ = h.raw("GET", base+"/"+id, "clerk", nil); got["name"] != "Torno Norte 2" || got["version"] != v || got["status"] != "in-service" {
		t.Fatalf("nothing of a failing change is kept: %v", got)
	}

	// DELETE no da de baja un activo en servicio (le faltan fecha, tipo e importe): 409 con su código.
	code, problem, _ = h.raw("DELETE", base+"/"+id, "clerk", nil)
	if code != 409 || problem["code"] != assets.CodeDisposalRequiresData {
		t.Fatalf("delete in service: %d %v", code, problem)
	}
	// La baja va por PUT, con el cambio de nombre en la misma unidad de trabajo.
	code, changed, _ = h.raw("PUT", base+"/"+id, "clerk", map[string]any{"name": "Torno vendido", "disposed": "2026-03-15", "disposal": "sale",
		"proceeds": "1000", "version": v})
	if code != 200 || changed["status"] != "disposed" || changed["name"] != "Torno vendido" || changed["proceeds"] != "1000.00" {
		t.Fatalf("dispose by PUT: %d %v", code, changed)
	}
	code, problem, _ = h.raw("PUT", base+"/"+id, "clerk", map[string]any{"disposal": "scrap", "version": changed["version"]})
	if errs, _ := problem["errors"].(map[string]any); code != 400 || errs["disposal"] == nil {
		t.Fatalf("a disposal does not change: %d %v", code, problem)
	}
	// Sobre uno dado de baja, DELETE es 204 y se puede repetir sin cambiar nada; pero exige poder
	// dar de baja en esa organización.
	h.must(status("DELETE", base+"/"+id, nil), 204, "delete")
	h.must(status("DELETE", base+"/"+id, nil), 204, "delete again")
	if _, again, _ := h.raw("GET", base+"/"+id, "clerk", nil); again["version"] != changed["version"] {
		t.Fatalf("delete changes nothing: %v", again)
	}
	h.must(func() int { s, _, _ := h.raw("DELETE", base+"/"+id, "viewer", nil); return s }(), 403, "delete without the permission")
	s, _, _ := h.dir.Subject(context.Background(), h.ids["outsider"])
	s.Grants = append(s.Grants, authz.Grant{OrganizationID: fw.MustParseUUID(acme), Level: authz.ReadOnly})
	h.dir.Put(h.ids["outsider"], s)
	h.must(func() int { s, _, _ := h.raw("DELETE", base+"/"+id, "outsider", nil); return s }(), 403, "delete read-only")
	s.Grants = s.Grants[:len(s.Grants)-1]
	h.dir.Put(h.ids["outsider"], s)

	// Permiso y ámbito.
	h.must(func() int { s, _, _ := h.raw("GET", base+"/"+id, "outsider", nil); return s }(), 404, "another organization")
	h.must(func() int { s, _, _ := h.raw("DELETE", base+"/"+id, "outsider", nil); return s }(), 404, "delete in another organization")
	h.must(func() int { s, _, _ := h.raw("GET", base, "", nil); return s }(), 401, "nobody")
}

func TestAssets_MaintenanceContract_MemoryThenSQLite(t *testing.T) {
	h := compose(t)
	h.maintenance("m")
	ctx := context.Background()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "front.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{pinfra.Migrations(), sinfra.Migrations()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.sw.Swap(ctx, db); err != nil {
		t.Fatal(err)
	}
	h.maintenance("s")
}
