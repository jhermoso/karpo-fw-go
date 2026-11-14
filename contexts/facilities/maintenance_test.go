package facilities_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/facilities"
	fdomain "github.com/jhermoso/karpo-fw-go/contexts/facilities/domain"
	finfra "github.com/jhermoso/karpo-fw-go/contexts/facilities/infrastructure"
	ginfra "github.com/jhermoso/karpo-fw-go/contexts/geography/infrastructure"
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

// El contrato de mantenimiento de las instalaciones, el que lee el motor de Angular de Karpo: alta
// con una propiedad de más (se ignora) y Location; lectura; lista por GET y por POST …/search con la
// página del motor, filtros, búsqueda, orden en los dos sentidos y 400 con lo que no es un orden;
// cambio devolviendo lo leído con su versión, 409 con la de antes y 400 sin ninguna; errores como
// diccionario; baja que desactiva y se puede repetir; y el permiso y el ámbito de siempre.
func (h *host) maintenance(tag string) {
	t := h.t
	acme, err := h.parties.Service.RegisterOrganization.Handle(h.adminCtx, papp.RegisterOrganization{LegalName: "Acme Front " + tag,
		Roles: []string{pdomain.RoleInternalOrganization.String()}})
	if err != nil {
		t.Fatal(err)
	}
	h.grant("clerk", acme.ID)
	base := facilities.MaintenanceBase
	warehouse := fdomain.TypeWarehouse.String()
	status := func(method, path string, body any) int { s, _, _ := h.raw(method, path, "clerk", body); return s }

	// Alta.
	code, created, header := h.raw("POST", base, "clerk", map[string]any{"organization": acme.ID, "type": warehouse, "name": "Almacén Norte",
		"createdBy": "nadie"})
	if code != 201 || created["name"] != "Almacén Norte" || created["active"] != true || header.Get("Location") != base+"/"+created["id"].(string) {
		t.Fatalf("create: %d %v %v", code, created, header)
	}
	id := created["id"].(string)
	for _, name := range []string{"Almacén Este", "Almacén Oeste", "Almacén Sur"} {
		h.must(status("POST", base, map[string]any{"organization": acme.ID, "type": warehouse, "name": name}), 201, "create "+name)
	}

	// Errores: el diccionario de campos, con sus códigos.
	code, problem, header := h.raw("POST", base, "clerk", map[string]any{"organization": acme.ID, "type": "nada", "name": "X"})
	errs, _ := problem["errors"].(map[string]any)
	codes, _ := problem["errorCodes"].(map[string]any)
	if code != 400 || errs["type"] == nil || codes["type"].([]any)[0] != "format" || !strings.HasPrefix(header.Get("Content-Type"), "application/problem+json") {
		t.Fatalf("validation: %d %v", code, problem)
	}
	h.must(status("POST", base, "no es json"), 400, "not JSON")
	h.must(status("POST", base, strings.Repeat(" ", 2<<20)), 413, "too large")

	// Lectura.
	code, got, _ := h.raw("GET", base+"/"+id, "clerk", nil)
	if code != 200 || got["name"] != "Almacén Norte" || got["typeName"] != "Warehouse" {
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
	q := "?organization=" + acme.ID
	code, page, _ := h.raw("GET", base+q+"&sortBy=name&sortOrder=desc&pageSize=2&page=2", "clerk", nil)
	if code != 200 || names(page) != "Almacén Norte,Almacén Este" || page["totalCount"] != float64(4) || page["total"] != float64(4) ||
		page["pageNumber"] != float64(2) || page["pageSize"] != float64(2) || page["totalPages"] != float64(2) {
		t.Fatalf("list: %d %v", code, page)
	}
	if _, page, _ = h.raw("GET", base+q+"&search=oeste&isActive=true", "clerk", nil); names(page) != "Almacén Oeste" {
		t.Fatalf("search: %v", page)
	}
	for _, bad := range []string{"&sortBy=areaM2", "&sortOrder=up", "&page=0", "&pageSize=1001"} {
		h.must(status("GET", base+q+bad, nil), 400, bad)
	}
	code, page, _ = h.raw("POST", base+"/search", "clerk", map[string]any{"filter": map[string]any{"organization": acme.ID, "type": warehouse},
		"pagination": map[string]any{"pageNumber": 1, "pageSize": 3, "orderBy": "name", "ascending": true}})
	if code != 200 || names(page) != "Almacén Este,Almacén Norte,Almacén Oeste" || page["totalCount"] != float64(4) {
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
	got["name"] = "Almacén Norte 2"
	code, changed, _ := h.raw("PUT", base+"/"+id, "clerk", got)
	if code != 200 || changed["name"] != "Almacén Norte 2" || changed["version"] != got["version"].(float64)+1 {
		t.Fatalf("update: %d %v", code, changed)
	}
	// Como el modal de la rejilla: sólo los campos del formulario y la versión; lo demás se queda.
	code, changed, _ = h.raw("PUT", base+"/"+id, "clerk", map[string]any{"name": "Almacén Norte 3", "active": false, "version": changed["version"]})
	if code != 200 || changed["name"] != "Almacén Norte 3" || changed["active"] != false || changed["type"] != warehouse {
		t.Fatalf("an update with only some fields: %d %v", code, changed)
	}
	h.must(status("PUT", base+"/"+id, got), 409, "the version read before")
	code, problem, _ = h.raw("PUT", base+"/"+id, "clerk", map[string]any{"description": "otra", "version": changed["version"]})
	if errs, _ := problem["errors"].(map[string]any); code != 400 || errs["description"] == nil {
		t.Fatalf("a field no use case changes: %d %v", code, problem)
	}
	delete(got, "version")
	code, problem, _ = h.raw("PUT", base+"/"+id, "clerk", got)
	if errs, _ := problem["errors"].(map[string]any); code != 400 || errs["version"] == nil {
		t.Fatalf("without a version: %d %v", code, problem)
	}
	// Reactivar y renombrar en un solo cambio.
	code, changed, _ = h.raw("PUT", base+"/"+id, "clerk", map[string]any{"name": "Almacén Norte", "active": true, "version": changed["version"]})
	if code != 200 || changed["name"] != "Almacén Norte" || changed["active"] != true {
		t.Fatalf("reactivate and rename: %d %v", code, changed)
	}

	// Atómico: si un paso falla (aquí, mover bajo algo que no es una identidad), no queda hecho
	// ninguno, ni el nombre ni la versión.
	v := changed["version"]
	code, problem, _ = h.raw("PUT", base+"/"+id, "clerk", map[string]any{"name": "Otro nombre", "partOf": "nada", "version": v})
	if errs, _ := problem["errors"].(map[string]any); code != 400 || errs["partOf"] == nil {
		t.Fatalf("a failing step: %d %v", code, problem)
	}
	if _, got, _ = h.raw("GET", base+"/"+id, "clerk", nil); got["name"] != "Almacén Norte" || got["version"] != v {
		t.Fatalf("nothing of a failing change is kept: %v", got)
	}
	// Lo que no cambia se compara por su valor; una ubicación vacía o null es «no viene».
	for _, loc := range []any{nil, map[string]any{}} {
		code, changed, _ = h.raw("PUT", base+"/"+id, "clerk", map[string]any{"organization": strings.ToUpper(acme.ID), "type": strings.ToUpper(warehouse),
			"location": loc, "version": v})
		if code != 200 || changed["version"] != v {
			t.Fatalf("an update that changes nothing: %d %v", code, changed)
		}
	}
	// El alta inactiva se rechaza.
	code, problem, _ = h.raw("POST", base, "clerk", map[string]any{"organization": acme.ID, "type": warehouse, "name": "Inactivo", "active": false})
	if errs, _ := problem["errors"].(map[string]any); code != 400 || errs["active"] == nil {
		t.Fatalf("registered inactive: %d %v", code, problem)
	}

	// Baja: la desactiva, y repetirla no es un error ni un cambio, pero exige poder escribir.
	h.must(status("DELETE", base+"/"+id, nil), 204, "delete")
	_, got, _ = h.raw("GET", base+"/"+id, "clerk", nil)
	h.must(status("DELETE", base+"/"+id, nil), 204, "delete again")
	if _, again, _ := h.raw("GET", base+"/"+id, "clerk", nil); got["active"] != false || again["version"] != got["version"] {
		t.Fatalf("deactivated once: %v %v", got, again)
	}
	s, _, _ := h.dir.Subject(context.Background(), h.ids["outsider"])
	s.Grants = append(s.Grants, authz.Grant{OrganizationID: fw.MustParseUUID(acme.ID), Level: authz.ReadOnly})
	h.dir.Put(h.ids["outsider"], s)
	h.must(func() int { s, _, _ := h.raw("DELETE", base+"/"+id, "outsider", nil); return s }(), 403, "delete read-only")
	s.Grants = s.Grants[:len(s.Grants)-1]
	h.dir.Put(h.ids["outsider"], s)

	// Permiso y ámbito.
	h.must(func() int { s, _, _ := h.raw("GET", base+"/"+id, "outsider", nil); return s }(), 404, "another organization")
	h.must(func() int { s, _, _ := h.raw("GET", base, "", nil); return s }(), 401, "nobody")
}

func TestFacilities_MaintenanceContract_MemoryThenSQLite(t *testing.T) {
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
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{pinfra.Migrations(), ginfra.Migrations(), finfra.Migrations()})
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
