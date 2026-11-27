package hr_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	fapp "github.com/jhermoso/karpo-fw-go/contexts/facilities/application"
	fdomain "github.com/jhermoso/karpo-fw-go/contexts/facilities/domain"
	finfra "github.com/jhermoso/karpo-fw-go/contexts/facilities/infrastructure"
	ginfra "github.com/jhermoso/karpo-fw-go/contexts/geography/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/hr"
	happ "github.com/jhermoso/karpo-fw-go/contexts/hr/application"
	hdomain "github.com/jhermoso/karpo-fw-go/contexts/hr/domain"
	hinfra "github.com/jhermoso/karpo-fw-go/contexts/hr/infrastructure"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	pinfra "github.com/jhermoso/karpo-fw-go/contexts/parties/infrastructure"
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

// El contrato de mantenimiento de los centros de trabajo, el que lee el motor de Angular de Karpo:
// alta con una propiedad de más (se ignora) y Location; 422 con un código repetido; 400 con lo que
// no es una fecha y 413; lectura; lista por GET y por POST …/search con la página del motor,
// filtros, orden y 400 con lo que no es un orden; cambio con su versión, 409 con la de antes y 400
// sin ninguna; lo que no cambia, 400 si cambia y comparado por su valor; atomicidad; DELETE que
// cierra y se puede repetir; y el permiso y el ámbito de siempre.
func (h *host) workCenterMaintenance(tag string) {
	t := h.t
	ctx := h.adminCtx
	ps := h.parties.Service
	org := func(name string) string {
		o, err := ps.RegisterOrganization.Handle(ctx, papp.RegisterOrganization{LegalName: name + " " + tag,
			Roles: []string{pdomain.RoleInternalOrganization.String()}})
		if err != nil {
			t.Fatal(err)
		}
		return o.ID
	}
	acme, globex := org("Acme"), org("Globex")
	h.grant("clerk", authz.Full, acme)
	h.grant("outsider", authz.Full, globex)
	facility := func(owner, name string) string {
		f, err := h.fac.Service.Register.Handle(ctx, fapp.RegisterFacility{Organization: owner, Type: fdomain.TypeBuilding.String(), Name: name + " " + tag})
		if err != nil {
			t.Fatal(err)
		}
		return f.ID
	}
	base := hr.MaintenanceBase
	status := func(user, method, path string, body any) int { s, _, _ := h.raw(method, path, user, body); return s }
	center := func(owner, code string) map[string]any {
		return map[string]any{"employer": owner, "facility": facility(owner, code), "code": code, "opened": "2020-01-01"}
	}

	// Alta.
	in := center(acme, "b-2")
	in["createdBy"] = "nadie"
	code, created, header := h.raw("POST", base, "clerk", in)
	if code != 201 || created["code"] != "B-2" || created["active"] != true || created["headquarters"] != false ||
		header.Get("Location") != base+"/"+created["id"].(string) {
		t.Fatalf("create: %d %v %v", code, created, header)
	}
	id := created["id"].(string)
	for _, c := range []string{"A-1", "D-4", "C-3"} {
		h.must(status("clerk", "POST", base, center(acme, c)), 201, "create "+c)
	}
	h.must(status("outsider", "POST", base, center(globex, "A-1")), 201, "another organization's work center")
	again := center(acme, "B-2")
	h.must(status("clerk", "POST", base, again), 422, "the same code again")

	// Errores: el diccionario de campos, con sus códigos.
	bad := center(acme, "E-5")
	bad["opened"] = "ayer"
	code, problem, header := h.raw("POST", base, "clerk", bad)
	if errs, _ := problem["errors"].(map[string]any); code != 400 || errs["opened"] == nil || problem["errorCodes"].(map[string]any)["opened"].([]any)[0] != "format" ||
		!strings.HasPrefix(header.Get("Content-Type"), "application/problem+json") {
		t.Fatalf("validation: %d %v", code, problem)
	}
	bad["opened"], bad["closed"] = "2020-01-01", "2021-01-01"
	h.must(status("clerk", "POST", base, bad), 400, "born closed")
	h.must(status("clerk", "POST", base, "no es json"), 400, "not JSON")
	h.must(status("clerk", "POST", base, strings.Repeat(" ", 2<<20)), 413, "too large")

	// Lectura.
	code, got, _ := h.raw("GET", base+"/"+id, "clerk", nil)
	if code != 200 || got["code"] != "B-2" || got["employer"] != acme || got["opened"] != "2020-01-01" {
		t.Fatalf("get: %d %v", code, got)
	}
	h.must(status("clerk", "GET", base+"/nada", nil), 400, "not an id")

	// Lista por GET y por POST: la página del motor, filtros y orden.
	codes := func(page map[string]any) string {
		var out []string
		for _, it := range page["items"].([]any) {
			out = append(out, it.(map[string]any)["code"].(string))
		}
		return strings.Join(out, ",")
	}
	code, page, _ := h.raw("GET", base+"?sortBy=code&sortOrder=desc&pageSize=2&page=2&employer="+acme, "clerk", nil)
	if code != 200 || codes(page) != "B-2,A-1" || page["totalCount"] != float64(4) || page["total"] != float64(4) ||
		page["pageNumber"] != float64(2) || page["pageSize"] != float64(2) || page["totalPages"] != float64(2) {
		t.Fatalf("list: %d %v", code, page)
	}
	// El filtro de empresa no enseña otra organización.
	if code, page, _ = h.raw("GET", base+"?company="+globex, "clerk", nil); code != 200 || page["totalCount"] != float64(0) {
		t.Fatalf("another company: %d %v", code, page)
	}
	for _, bad := range []string{"&sortBy=opened", "&sortOrder=up", "&page=0", "&pageSize=1001"} {
		h.must(status("clerk", "GET", base+"?company="+acme+bad, nil), 400, bad)
	}
	h.must(status("clerk", "GET", base+"?employer=nada", nil), 400, "an employer that is not an id")
	code, page, _ = h.raw("POST", base+"/search", "clerk", map[string]any{"filter": map[string]any{"company": acme},
		"pagination": map[string]any{"pageNumber": 1, "pageSize": 3, "orderBy": "code", "ascending": true}})
	if code != 200 || codes(page) != "A-1,B-2,C-3" || page["totalCount"] != float64(4) {
		t.Fatalf("search by POST: %d %v", code, page)
	}
	h.must(status("clerk", "POST", base+"/search", map[string]any{"filter": map[string]any{"code": []string{"a"}}}), 400, "a filter that is not a single value")
	for _, n := range []int{0, -1} {
		h.must(status("clerk", "POST", base+"/search", map[string]any{"pagination": map[string]any{"pageNumber": n}}), 400, "a page out of range")
	}
	h.must(status("clerk", "POST", base+"/search", map[string]any{"pagination": map[string]any{"pageSize": 3}}), 200, "no page number")

	// Cambio: lo leído con su versión. Hacerlo sede.
	got["headquarters"] = true
	code, changed, _ := h.raw("PUT", base+"/"+id, "clerk", got)
	if code != 200 || changed["headquarters"] != true || changed["version"] != got["version"].(float64)+1 {
		t.Fatalf("update: %d %v", code, changed)
	}
	h.must(status("clerk", "PUT", base+"/"+id, got), 409, "the version read before")
	v := changed["version"]
	for _, c := range []struct{ field, value string }{{"code", "Z-9"}, {"employer", globex}, {"facility", fw.NewUUID().String()},
		{"opened", "2019-01-01"}} {
		field := c.field
		code, problem, _ = h.raw("PUT", base+"/"+id, "clerk", map[string]any{field: c.value, "version": v})
		if errs, _ := problem["errors"].(map[string]any); code != 400 || errs[field] == nil || problem["errorCodes"].(map[string]any)[field].([]any)[0] != "immutable" {
			t.Fatalf("%s, which no use case changes: %d %v", field, code, problem)
		}
	}
	h.must(status("clerk", "PUT", base+"/"+id, map[string]any{"headquarters": false, "version": v}), 400, "stop being the headquarters")
	h.must(status("clerk", "PUT", base+"/"+id, map[string]any{"code": "x"}), 400, "without a version")
	// Lo que no cambia se compara por su valor: la identidad en otras mayúsculas, el código en
	// minúsculas y lo que llega como null.
	code, changed, _ = h.raw("PUT", base+"/"+id, "clerk", map[string]any{"employer": strings.ToUpper(acme), "code": " b-2 ",
		"closed": nil, "version": v})
	if code != 200 || changed["version"] != v {
		t.Fatalf("an update that changes nothing: %d %v", code, changed)
	}

	// Atómico: hacer sede a otro y cerrarlo antes de abrirse falla entero; la sede sigue donde estaba.
	_, other, _ := h.raw("GET", base+"?sortBy=code&employer="+acme+"&pageSize=1", "clerk", nil)
	first := other["items"].([]any)[0].(map[string]any)
	code, problem, _ = h.raw("PUT", base+"/"+first["id"].(string), "clerk", map[string]any{"headquarters": true, "closed": "2019-01-01",
		"version": first["version"]})
	if code != 422 || problem["code"] != "hr.work_center_close_before_open" {
		t.Fatalf("a failing change: %d %v", code, problem)
	}
	if _, got, _ = h.raw("GET", base+"/"+id, "clerk", nil); got["headquarters"] != true {
		t.Fatalf("nothing of a failing change is kept: %v", got)
	}

	// Cerrar por el PUT, y un cerrado no se reabre.
	code, changed, _ = h.raw("PUT", base+"/"+first["id"].(string), "clerk", map[string]any{"closed": "2024-06-30", "version": first["version"]})
	if code != 200 || changed["closed"] != "2024-06-30" || changed["active"] != false {
		t.Fatalf("close by the PUT: %d %v", code, changed)
	}
	h.must(status("clerk", "PUT", base+"/"+first["id"].(string), map[string]any{"closed": "", "version": changed["version"]}), 400, "reopen")

	// DELETE cierra: exige el permiso y el ámbito de escritura, y se puede repetir sin versión nueva.
	h.must(status("clerk", "DELETE", base+"/"+id+"?on=ayer", nil), 400, "a date that is not a date")
	h.must(status("clerk", "DELETE", base+"/"+id, nil), 204, "delete")
	_, closed, _ := h.raw("GET", base+"/"+id, "clerk", nil)
	if closed["active"] != false || closed["closed"] == "" || closed["headquarters"] != false || closed["version"] != v.(float64)+1 {
		t.Fatalf("delete closes: %v", closed)
	}
	h.must(status("clerk", "DELETE", base+"/"+id, nil), 204, "delete again")
	if _, again, _ := h.raw("GET", base+"/"+id, "clerk", nil); again["version"] != closed["version"] {
		t.Fatalf("delete again changes nothing: %v", again)
	}
	// Cerrar otra vez uno cerrado con una versión que ya no es la suya es un conflicto.
	wid, _ := hdomain.ParseWorkCenterID(id)
	if _, err := h.hr.Service.CloseWorkCenter.Handle(ctx, happ.CloseWorkCenter{ID: wid, Version: int64(v.(float64))}); !errors.Is(err, fw.ErrConflict) {
		t.Fatalf("close a closed work center with an old version: %v", err)
	}
	if _, page, _ = h.raw("GET", base+"?isActive=true&employer="+acme, "clerk", nil); page["totalCount"] != float64(2) {
		t.Fatalf("a closed work center is not active: %v", page)
	}
	s, _, _ := h.dir.Subject(context.Background(), h.ids["outsider"])
	s.Grants = append(s.Grants, authz.Grant{OrganizationID: fw.MustParseUUID(acme), Level: authz.ReadOnly})
	h.dir.Put(h.ids["outsider"], s)
	h.must(status("outsider", "GET", base+"/"+id, nil), 200, "read-only reads")
	h.must(status("outsider", "DELETE", base+"/"+id, nil), 403, "delete read-only")
	h.must(status("outsider", "PUT", base+"/"+id, map[string]any{"version": closed["version"]}), 403, "change read-only")
	s.Grants = s.Grants[:len(s.Grants)-1]
	h.dir.Put(h.ids["outsider"], s)

	// Permiso y ámbito.
	h.must(status("outsider", "GET", base+"/"+id, nil), 404, "another organization")
	h.must(status("outsider", "DELETE", base+"/"+id, nil), 404, "delete in another organization")
	h.must(status("reader", "GET", base, nil), 403, "without the permission")
	h.must(status("", "GET", base, nil), 401, "nobody")
}

func TestHR_WorkCenterMaintenanceContract_MemoryThenSQLite(t *testing.T) {
	h := compose(t)
	h.workCenterMaintenance("mem")
	h.idsFilter("1")
	ctx := context.Background()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "front.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{pinfra.Migrations(), ginfra.Migrations(), finfra.Migrations(), hinfra.Migrations()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.sw.Swap(ctx, db); err != nil {
		t.Fatal(err)
	}
	h.workCenterMaintenance("sql")
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
		o, err := h.parties.Service.RegisterOrganization.Handle(h.adminCtx, papp.RegisterOrganization{LegalName: name + " ids " + tag,
			Roles: []string{pdomain.RoleInternalOrganization.String()}})
		if err != nil {
			t.Fatal(err)
		}
		h.grant(user, authz.Full, o.ID)
		return o.ID
	}
	a, b, c := org("Uno", "clerk"), org("Dos", "clerk"), org("Ajena", "outsider")
	create := func(user, org, n string) string {
		f, err := h.fac.Service.Register.Handle(h.adminCtx, fapp.RegisterFacility{Organization: org, Type: fdomain.TypeBuilding.String(), Name: "Ids " + n + " " + tag})
		if err != nil {
			t.Fatal(err)
		}
		code, out, _ := h.raw("POST", hr.MaintenanceBase, user, map[string]any{"employer": org, "facility": f.ID, "code": "IDS-" + n, "opened": "2020-01-01"})
		h.must(code, 201, "create "+n)
		return out["id"].(string)
	}
	mine := [2]string{create("clerk", a, "1"), create("clerk", b, "2")}
	foreign := create("outsider", c, "3")
	testkit.CheckIDsFilter(t, testkit.IDsFilterCase{Base: hr.MaintenanceBase,
		Do: func(method, path string, body any) (int, map[string]any) {
			code, out, _ := h.raw(method, path, "clerk", body)
			return code, out
		},
		Denied: h.denied, Mine: mine, Foreign: foreign, And: "employer=" + a,
		Missing: fw.NewUUID().String(), Zero: fw.UUID{}.String(), Bad: "junk", Many: testkit.ManyUUIDs})
}
