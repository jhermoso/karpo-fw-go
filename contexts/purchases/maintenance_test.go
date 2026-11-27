package purchases_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/purchases"
	uapp "github.com/jhermoso/karpo-fw-go/contexts/purchases/application"
	uinfra "github.com/jhermoso/karpo-fw-go/contexts/purchases/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
	"github.com/jhermoso/karpo-fw-go/pkg/testing/testkit"
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

// El contrato de mantenimiento de los proveedores, el que lee el motor de Angular de Karpo: alta con
// una propiedad de más (se ignora) y Location; 422 con un proveedor repetido; lectura; lista por GET
// y por POST …/search con la página del motor, filtros, orden y 400 con lo que no es un orden;
// cambio con su versión, 409 con la de antes y 400 sin ninguna; lo que no cambia, 400 si cambia y
// comparado por su valor; atomicidad; DELETE que bloquea y se puede repetir; y el permiso y el
// ámbito de siempre.
func (h *host) supplierMaintenance() {
	t := h.t
	acme, globex := fw.NewUUID().String(), fw.NewUUID().String()
	h.grant("clerk", acme)
	h.grant("viewer", acme)
	h.grant("outsider", globex)
	base := purchases.MaintenanceBase
	status := func(user, method, path string, body any) int { s, _, _ := h.raw(method, path, user, body); return s }
	profile := func(days int) map[string]any {
		return map[string]any{"company": acme, "supplier": fw.NewUUID().String(), "paymentDays": days, "withholdingRate": "15"}
	}

	// Alta.
	in := profile(30)
	in["createdBy"] = "nadie"
	code, created, header := h.raw("POST", base, "clerk", in)
	if code != 201 || created["paymentDays"] != float64(30) || created["withholdingRate"] != "15.00" || created["blocked"] != false ||
		header.Get("Location") != base+"/"+created["id"].(string) {
		t.Fatalf("create: %d %v %v", code, created, header)
	}
	id := created["id"].(string)
	for _, days := range []int{60, 0, 90} {
		h.must(status("clerk", "POST", base, profile(days)), 201, "create "+strconv.Itoa(days))
	}
	other := profile(10)
	other["company"] = globex
	h.must(status("outsider", "POST", base, other), 201, "another organization's supplier")
	h.must(status("clerk", "POST", base, map[string]any{"company": acme, "supplier": created["supplier"]}), 422, "the same supplier again")

	// Errores: el diccionario de campos, con sus códigos.
	bad := profile(400)
	code, problem, header := h.raw("POST", base, "clerk", bad)
	errs, _ := problem["errors"].(map[string]any)
	codes, _ := problem["errorCodes"].(map[string]any)
	if code != 400 || errs["paymentDays"] == nil || codes["paymentDays"].([]any)[0] != "range" ||
		!strings.HasPrefix(header.Get("Content-Type"), "application/problem+json") {
		t.Fatalf("validation: %d %v", code, problem)
	}
	h.must(status("clerk", "POST", base, "no es json"), 400, "not JSON")
	h.must(status("clerk", "POST", base, strings.Repeat(" ", 2<<20)), 413, "too large")

	// Lectura.
	code, got, _ := h.raw("GET", base+"/"+id, "clerk", nil)
	if code != 200 || got["paymentDays"] != float64(30) || got["company"] != acme {
		t.Fatalf("get: %d %v", code, got)
	}
	h.must(status("clerk", "GET", base+"/nada", nil), 400, "not an id")

	// Lista por GET y por POST: la página del motor, filtros y orden.
	days := func(page map[string]any) string {
		var out []string
		for _, it := range page["items"].([]any) {
			out = append(out, strconv.Itoa(int(it.(map[string]any)["paymentDays"].(float64))))
		}
		return strings.Join(out, ",")
	}
	code, page, _ := h.raw("GET", base+"?sortBy=paymentDays&sortOrder=desc&pageSize=2&page=2", "clerk", nil)
	if code != 200 || days(page) != "30,0" || page["totalCount"] != float64(4) || page["total"] != float64(4) ||
		page["pageNumber"] != float64(2) || page["pageSize"] != float64(2) || page["totalPages"] != float64(2) {
		t.Fatalf("list: %d %v", code, page)
	}
	if _, page, _ = h.raw("GET", base+"?supplier="+created["supplier"].(string), "clerk", nil); days(page) != "30" {
		t.Fatalf("by supplier: %v", page)
	}
	// El filtro de empresa no enseña otra organización.
	if code, page, _ = h.raw("GET", base+"?company="+globex, "clerk", nil); code != 200 || page["totalCount"] != float64(0) {
		t.Fatalf("another company: %d %v", code, page)
	}
	for _, bad := range []string{"&sortBy=supplier", "&sortOrder=up", "&page=0", "&pageSize=1001"} {
		h.must(status("clerk", "GET", base+"?company="+acme+bad, nil), 400, bad)
	}
	h.must(status("clerk", "GET", base+"?company=nada", nil), 400, "a company that is not an id")
	code, page, _ = h.raw("POST", base+"/search", "clerk", map[string]any{"filter": map[string]any{"company": acme},
		"pagination": map[string]any{"pageNumber": 1, "pageSize": 3, "orderBy": "paymentDays", "ascending": true}})
	if code != 200 || days(page) != "0,30,60" || page["totalCount"] != float64(4) {
		t.Fatalf("search by POST: %d %v", code, page)
	}
	h.must(status("clerk", "POST", base+"/search", map[string]any{"filter": map[string]any{"category": []string{"a"}}}), 400, "a filter that is not a single value")
	for _, n := range []int{0, -1} {
		h.must(status("clerk", "POST", base+"/search", map[string]any{"pagination": map[string]any{"pageNumber": n}}), 400, "a page out of range")
	}
	h.must(status("clerk", "POST", base+"/search", map[string]any{"pagination": map[string]any{"pageSize": 3}}), 200, "no page number")

	// Cambio: lo leído con su versión.
	got["paymentDays"] = 45
	code, changed, _ := h.raw("PUT", base+"/"+id, "clerk", got)
	if code != 200 || changed["paymentDays"] != float64(45) || changed["version"] != got["version"].(float64)+1 {
		t.Fatalf("update: %d %v", code, changed)
	}
	// Como el modal de la rejilla: sólo los campos del formulario y la versión; lo demás se queda,
	// también lo que llega como null.
	code, changed, _ = h.raw("PUT", base+"/"+id, "clerk", map[string]any{"iban": "ES91 2100 0418 4502 0005 1332", "paymentDays": nil,
		"version": changed["version"]})
	if code != 200 || changed["iban"] != "ES9121000418450200051332" || changed["paymentDays"] != float64(45) {
		t.Fatalf("an update with only some fields: %d %v", code, changed)
	}
	h.must(status("clerk", "PUT", base+"/"+id, got), 409, "the version read before")
	for _, c := range []struct{ field, value string }{{"supplier", fw.NewUUID().String()}, {"company", globex}} {
		field := c.field
		code, problem, _ = h.raw("PUT", base+"/"+id, "clerk", map[string]any{field: c.value, "version": changed["version"]})
		if errs, _ := problem["errors"].(map[string]any); code != 400 || errs[field] == nil || problem["errorCodes"].(map[string]any)[field].([]any)[0] != "immutable" {
			t.Fatalf("%s, which no use case changes: %d %v", field, code, problem)
		}
	}
	delete(got, "version")
	code, problem, _ = h.raw("PUT", base+"/"+id, "clerk", got)
	if errs, _ := problem["errors"].(map[string]any); code != 400 || errs["version"] == nil {
		t.Fatalf("without a version: %d %v", code, problem)
	}
	// Lo que no cambia se compara por su valor: la identidad en otras mayúsculas, la retención con
	// otros decimales y la cuenta con espacios.
	v := changed["version"]
	code, changed, _ = h.raw("PUT", base+"/"+id, "clerk", map[string]any{"company": strings.ToUpper(acme), "withholdingRate": "15.0",
		"iban": "es91 2100 0418 4502 0005 1332", "version": v})
	if code != 200 || changed["version"] != v {
		t.Fatalf("an update that changes nothing: %d %v", code, changed)
	}
	// Aunque no cambie nada, cambiar exige poder cambiar.
	h.must(status("viewer", "PUT", base+"/"+id, map[string]any{"version": v}), 403, "an empty change by a viewer")

	// Atómico: si el cambio falla (aquí, una retención fuera de rango), no queda nada.
	code, problem, _ = h.raw("PUT", base+"/"+id, "clerk", map[string]any{"paymentDays": 50, "withholdingRate": "80", "version": v})
	if code != 400 || problem["errorCodes"].(map[string]any)["withholdingRate"] == nil {
		t.Fatalf("a failing change: %d %v", code, problem)
	}
	if _, got, _ = h.raw("GET", base+"/"+id, "clerk", nil); got["paymentDays"] != float64(45) || got["version"] != v {
		t.Fatalf("nothing of a failing change is kept: %v", got)
	}
	// La categoría se guarda sin los espacios de los extremos.
	code, changed, _ = h.raw("PUT", base+"/"+id, "clerk", map[string]any{"category": " rent ", "version": v})
	if code != 200 || changed["category"] != "rent" {
		t.Fatalf("a change with spaces at the ends: %d %v", code, changed)
	}
	v = changed["version"]

	// DELETE bloquea: exige el permiso y el ámbito de escritura, y se puede repetir sin versión nueva.
	h.must(status("viewer", "DELETE", base+"/"+id, nil), 403, "delete without the permission")
	// Bloquear exige un motivo: sin él, 422 con su código.
	code, problem, _ = h.raw("DELETE", base+"/"+id, "clerk", map[string]any{"reason": "  "})
	if code != 422 || problem["code"] != "purchases.block_reason_required" {
		t.Fatalf("delete without a reason: %d %v", code, problem)
	}
	h.must(status("clerk", "DELETE", base+"/"+id, map[string]any{"reason": " impago "}), 204, "delete")
	_, blocked, _ := h.raw("GET", base+"/"+id, "clerk", nil)
	if blocked["blocked"] != true || blocked["version"] != v.(float64)+1 || blocked["category"] != "rent" || blocked["blockReason"] != "impago" {
		t.Fatalf("delete blocks: %v", blocked)
	}
	h.must(status("clerk", "DELETE", base+"/"+id+"?reason=otra", nil), 204, "delete again")
	if _, again, _ := h.raw("GET", base+"/"+id, "clerk", nil); again["version"] != blocked["version"] {
		t.Fatalf("delete again changes nothing: %v", again)
	}
	h.must(status("viewer", "DELETE", base+"/"+id, nil), 403, "delete again without the permission")
	if _, page, _ = h.raw("GET", base+"?isActive=true", "clerk", nil); page["totalCount"] != float64(3) {
		t.Fatalf("a blocked supplier is not active: %v", page)
	}
	s, _, _ := h.dir.Subject(context.Background(), h.ids["outsider"])
	s.Grants = append(s.Grants, authz.Grant{OrganizationID: fw.MustParseUUID(acme), Level: authz.ReadOnly})
	h.dir.Put(h.ids["outsider"], s)
	h.must(status("outsider", "DELETE", base+"/"+id, nil), 403, "delete read-only")
	h.must(status("outsider", "PUT", base+"/"+id, map[string]any{"version": blocked["version"]}), 403, "change read-only")
	s.Grants = s.Grants[:len(s.Grants)-1]
	h.dir.Put(h.ids["outsider"], s)

	// El PUT no bloquea ni desbloquea (D-156 ampliada): pedir otro estado es 422; el mismo, o
	// ningún estado, se ignora con sus motivos.
	code, problem, _ = h.raw("PUT", base+"/"+id, "clerk", map[string]any{"blocked": false, "unblockReason": "pagado", "version": blocked["version"]})
	if code != 422 || problem["code"] != "purchases.block_change_requires_command" {
		t.Fatalf("unblock by the PUT: %d %v", code, problem)
	}
	code, changed, _ = h.raw("PUT", base+"/"+id, "clerk", map[string]any{"blocked": true, "blockReason": "otro", "paymentDays": 46, "version": blocked["version"]})
	if code != 200 || changed["blockReason"] != "impago" || changed["paymentDays"] != float64(46) {
		t.Fatalf("the same block by the PUT: %d %v", code, changed)
	}
	// La orden de desbloquear: versión y motivo obligatorios.
	unblock := base + "/" + id + "/unblock"
	code, problem, _ = h.raw("POST", unblock, "clerk", map[string]any{"version": changed["version"]})
	if code != 422 || problem["code"] != "purchases.unblock_reason_required" {
		t.Fatalf("unblock without a reason: %d %v", code, problem)
	}
	h.must(status("clerk", "POST", unblock, map[string]any{"reason": "pagado"}), 400, "unblock without a version")
	h.must(status("clerk", "POST", unblock, map[string]any{"reason": "pagado", "version": blocked["version"]}), 409, "unblock an old version")
	h.must(status("viewer", "POST", unblock, map[string]any{"reason": "pagado", "version": changed["version"]}), 403, "unblock without the permission")
	code, changed, _ = h.raw("POST", unblock, "clerk", map[string]any{"reason": " pagado ", "version": changed["version"]})
	if code != 200 || changed["blocked"] != false || changed["unblockReason"] != "pagado" || changed["blockReason"] != "" {
		t.Fatalf("unblock: %d %v", code, changed)
	}
	code, problem, _ = h.raw("POST", unblock, "clerk", map[string]any{"reason": "otra", "version": changed["version"]})
	if code != 422 || problem["code"] != "purchases.supplier_not_blocked" {
		t.Fatalf("unblock twice: %d %v", code, problem)
	}
	h.must(status("clerk", "DELETE", base+"/"+id+"?reason=otra+vez", nil), 204, "block by the query")
	if _, got, _ = h.raw("GET", base+"/"+id, "clerk", nil); got["blockReason"] != "otra vez" || got["unblockReason"] != "" {
		t.Fatalf("blocked by the query: %v", got)
	}
	code, problem, _ = h.raw("POST", base+"/"+id+"/block", "clerk", map[string]any{"reason": "x", "version": got["version"]})
	if code != 422 || problem["code"] != "purchases.supplier_already_blocked" {
		t.Fatalf("block twice: %d %v", code, problem)
	}
	// El historial: una fila por bloqueo y desbloqueo efectivo, con quién y por qué.
	var history []uapp.BlockEventDTO
	h.must(h.do("GET", base+"/"+id+"/block-history", "viewer", nil, &history), 200, "history")
	if len(history) != 3 || !history[0].Blocked || history[0].Reason != "impago" || history[1].Blocked || history[1].Reason != "pagado" ||
		!history[2].Blocked || history[2].Reason != "otra vez" || history[0].UserName == "" || history[0].Approval != "" {
		t.Fatalf("history = %+v", history)
	}
	h.must(status("outsider", "GET", base+"/"+id+"/block-history", nil), 404, "history in another organization")
	// El alta no nace bloqueada.
	code, problem, _ = h.raw("POST", base, "clerk", map[string]any{"company": acme, "supplier": fw.NewUUID().String(), "blocked": true, "blockReason": "x"})
	if code != 422 || problem["code"] != "purchases.block_change_requires_command" {
		t.Fatalf("born blocked: %d %v", code, problem)
	}

	// Permiso y ámbito.
	h.must(status("outsider", "GET", base+"/"+id, nil), 404, "another organization")
	h.must(status("outsider", "DELETE", base+"/"+id, nil), 404, "delete in another organization")
	h.must(status("canceller", "GET", base, nil), 403, "without the permission")
	h.must(status("", "GET", base, nil), 401, "nobody")
}

func TestPurchases_SupplierMaintenanceContract_MemoryThenSQLite(t *testing.T) {
	h := compose(t)
	h.supplierMaintenance()
	h.idsFilter("1")
	ctx := context.Background()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "front.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{uinfra.Migrations()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.sw.Swap(ctx, db); err != nil {
		t.Fatal(err)
	}
	h.supplierMaintenance()
	h.idsFilter("2")
}

// denied hace una petición como outsider sin ningún permiso, y le devuelve los suyos después.
func (h *host) denied(method, path string, body any) int {
	s, _, _ := h.dir.Subject(context.Background(), h.ids["outsider"])
	saved := s.Permissions
	s.Permissions = nil
	h.dir.Put(h.ids["outsider"], s)
	defer func() { s.Permissions = saved; h.dir.Put(h.ids["outsider"], s) }()
	code, _, _ := h.raw(method, path, "outsider", body)
	return code
}

// idsFilter: el filtro ids de la lista (B-15, docs/CONTRATO-MANTENIMIENTO.md). Las dos
// identidades pedidas son de dos organizaciones del usuario; la tercera, de otra.
func (h *host) idsFilter(tag string) {
	t := h.t
	_ = t
	org := func(name, user string) string {
		id := fw.NewUUID().String()
		h.grant(user, id)
		return id
	}
	a, b, c := org("Uno", "clerk"), org("Dos", "clerk"), org("Ajena", "outsider")
	create := func(user, org, n string) string {
		code, out, _ := h.raw("POST", purchases.MaintenanceBase, user, map[string]any{"company": org, "supplier": fw.NewUUID().String(), "paymentDays": 30})
		h.must(code, 201, "create "+n)
		return out["id"].(string)
	}
	mine := [2]string{create("clerk", a, "1"), create("clerk", b, "2")}
	foreign := create("outsider", c, "3")
	testkit.CheckIDsFilter(t, testkit.IDsFilterCase{Base: purchases.MaintenanceBase,
		Do: func(method, path string, body any) (int, map[string]any) {
			code, out, _ := h.raw(method, path, "clerk", body)
			return code, out
		},
		Denied: h.denied, Mine: mine, Foreign: foreign, And: "company=" + a,
		Missing: fw.NewUUID().String(), Zero: fw.UUID{}.String(), Bad: "junk", Many: testkit.ManyUUIDs})
}
