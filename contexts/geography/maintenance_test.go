package geography_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/jhermoso/karpo-fw-go/contexts/geography"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/testing/testkit"
)

// raw hace una petición y devuelve el estado, el cuerpo como JSON genérico y las cabeceras.
func (e *env) raw(method, path, auth string, body any) (int, map[string]any, http.Header) {
	e.t.Helper()
	var buf bytes.Buffer
	switch b := body.(type) {
	case nil:
	case string:
		buf.WriteString(b)
	default:
		_ = json.NewEncoder(&buf).Encode(b)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, &buf)
	req.Header.Set("Authorization", auth)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out, res.Header
}

// maintenance ejercita el contrato de mantenimiento, el que lee el motor de Angular de Karpo: los
// países, que sólo se leen (alta, cambio y baja dan 409 con la vía), y los festivos: alta con
// Location, lectura, lista por GET y por POST …/search con filtros, orden y 400 con lo que no es
// un orden; cambio del nombre con la versión (409 la de antes, 400 sin ninguna, 400 el lugar o el
// día); errores como diccionario; baja idempotente que exige el permiso. El año 2031 no lo usa
// ningún otro escenario.
func (e *env) maintenance() {
	t := e.t
	ctx := context.Background()
	field := func(page map[string]any, name string) string {
		var out []string
		for _, it := range page["items"].([]any) {
			out = append(out, it.(map[string]any)[name].(string))
		}
		return strings.Join(out, ",")
	}

	// Países: la lista, con la página del motor.
	cb := geography.CountriesBase
	code, page, _ := e.raw("GET", cb+"?search=spain", e.reader, nil)
	if code != 200 || field(page, "id") != "ES" || page["totalCount"] != float64(1) || page["total"] != float64(1) || page["pageNumber"] != float64(1) {
		t.Fatalf("countries: %d %v", code, page)
	}
	code, page, _ = e.raw("GET", cb+"?eurozone=true&sortBy=alpha2&sortOrder=desc&pageSize=2&page=1", e.reader, nil)
	if code != 200 || len(page["items"].([]any)) != 2 || page["pageSize"] != float64(2) || page["totalCount"].(float64) < 2 {
		t.Fatalf("countries of the eurozone: %d %v", code, page)
	}
	ids := strings.Split(field(page, "id"), ",")
	if ids[0] < ids[1] {
		t.Fatalf("descending: %v", ids)
	}
	code, page, _ = e.raw("POST", cb+"/search", e.reader, map[string]any{"filter": map[string]any{"search": "ESP", "eu": true},
		"pagination": map[string]any{"pageSize": 0, "orderBy": "name"}})
	if code != 200 || field(page, "id") != "ES" || page["pageSize"] != float64(fw.DefaultPageSize) {
		t.Fatalf("countries by POST: %d %v", code, page)
	}
	// El orden por alpha3 y por name, en los dos sentidos. En un empate, alpha2 ascendente (en la
	// semilla no hay dos nombres ni dos alfa-3 iguales, así que esa rama no llega a darse).
	items := func(q string) []map[string]any {
		code, page, _ := e.raw("GET", cb+q, e.reader, nil)
		if code != 200 {
			t.Fatalf("%s: %d %v", q, code, page)
		}
		var out []map[string]any
		for _, it := range page["items"].([]any) {
			out = append(out, it.(map[string]any))
		}
		if len(out) < 2 || page["totalCount"] != float64(len(out)) {
			t.Fatalf("%s: %d items of %v", q, len(out), page["totalCount"])
		}
		return out
	}
	for _, c := range []struct{ by, order string }{{"alpha3", "asc"}, {"alpha3", "desc"}, {"name", "asc"}, {"name", "desc"}} {
		q := "?pageSize=1000&sortBy=" + c.by + "&sortOrder=" + c.order
		list := items(q)
		for i := 1; i < len(list); i++ {
			a, b := strings.ToLower(list[i-1][c.by].(string)), strings.ToLower(list[i][c.by].(string))
			cmp := strings.Compare(a, b)
			if c.order == "desc" {
				cmp = -cmp
			}
			if cmp > 0 || cmp == 0 && list[i-1]["id"].(string) >= list[i]["id"].(string) {
				t.Fatalf("%s: %v before %v", q, list[i-1], list[i])
			}
		}
	}
	// eu=true y sepa=true: sólo los que lo son, España dentro y Estados Unidos fuera.
	for _, flag := range []string{"eu", "sepa"} {
		list := items("?pageSize=1000&" + flag + "=true")
		seen := map[string]bool{}
		for _, it := range list {
			if it[flag] != true {
				t.Fatalf("%s=true: %v", flag, it)
			}
			seen[it["id"].(string)] = true
		}
		if !seen["ES"] || seen["US"] {
			t.Fatalf("%s=true: %v", flag, seen)
		}
	}
	if eu, sepa := len(items("?pageSize=1000&eu=true")), len(items("?pageSize=1000&sepa=true")); sepa <= eu {
		t.Fatalf("SEPA (%d) should be wider than the EU (%d)", sepa, eu)
	}
	for _, bad := range []string{"?sortBy=numeric", "?sortOrder=up", "?page=0", "?pageSize=1001", "?eu=si"} {
		e.must(func() int { s, _, _ := e.raw("GET", cb+bad, e.reader, nil); return s }(), 400, bad)
	}
	e.must(func() int {
		s, _, _ := e.raw("POST", cb+"/search", e.reader, map[string]any{"pagination": map[string]any{"pageNumber": 0}})
		return s
	}(), 400, "page 0 by POST")
	code, got, _ := e.raw("GET", cb+"/es", e.reader, nil)
	if code != 200 || got["id"] != "ES" || got["alpha3"] != "ESP" || got["postalCodePattern"] == nil {
		t.Fatalf("country: %d %v", code, got)
	}
	e.must(func() int { s, _, _ := e.raw("GET", cb+"/XYZ", e.reader, nil); return s }(), 400, "not a country code")
	// Los países no se mantienen por aquí: 409 con la vía, y antes el permiso.
	for _, c := range []struct{ method, path string }{{"POST", cb}, {"PUT", cb + "/ES"}, {"DELETE", cb + "/ES"}} {
		code, problem, _ := e.raw(c.method, c.path, e.reader, map[string]any{"alpha2": "ES"})
		if code != 409 || !strings.Contains(problem["detail"].(string), "migrations") {
			t.Fatalf("%s %s: %d %v", c.method, c.path, code, problem)
		}
		e.must(func() int { s, _, _ := e.raw(c.method, c.path, e.keeper, nil); return s }(), 403, c.method+" without the permission")
	}

	// Festivos: el alta, con una propiedad de más (se ignora).
	es, err := e.mod.Ports.Country(ctx, "ES")
	if err != nil {
		t.Fatal(err)
	}
	spain := es.Boundary
	hb := geography.HolidaysBase
	code, created, header := e.raw("POST", hb, e.keeper, map[string]any{"boundary": spain, "date": "2031-12-25", "name": "  Navidad  ", "createdBy": "nadie"})
	if code != 201 || created["name"] != "Navidad" || created["boundaryName"] != "Spain" || created["version"] != float64(1) ||
		header.Get("Location") != hb+"/"+created["id"].(string) {
		t.Fatalf("create: %d %v %v", code, created, header)
	}
	id := created["id"].(string)
	for date, name := range map[string]string{"2031-01-01": "Año Nuevo", "2031-01-06": "Epifanía", "2031-10-12": "Fiesta Nacional"} {
		e.must(func() int {
			s, _, _ := e.raw("POST", hb, e.keeper, map[string]any{"boundary": spain, "date": date, "name": name})
			return s
		}(), 201, name)
	}
	e.must(func() int {
		s, _, _ := e.raw("POST", hb, e.keeper, map[string]any{"boundary": spain, "date": "2031-12-25", "name": "Otra"})
		return s
	}(), 409, "the same day twice")
	code, problem, header := e.raw("POST", hb, e.keeper, map[string]any{"boundary": spain, "date": "25/12/2031", "name": ""})
	errs, _ := problem["errors"].(map[string]any)
	codes, _ := problem["errorCodes"].(map[string]any)
	if code != 400 || errs["date"] == nil || codes["date"].([]any)[0] != "format" || !strings.HasPrefix(header.Get("Content-Type"), "application/problem+json") {
		t.Fatalf("validation: %d %v", code, problem)
	}
	e.must(func() int { s, _, _ := e.raw("POST", hb, e.keeper, "no es json"); return s }(), 400, "not JSON")
	e.must(func() int { s, _, _ := e.raw("POST", hb, e.keeper, strings.Repeat(" ", 2<<20)); return s }(), 413, "too large")
	e.must(func() int {
		s, _, _ := e.raw("POST", hb, e.reader, map[string]any{"boundary": spain, "date": "2031-05-01", "name": "Trabajo"})
		return s
	}(), 403, "create without the permission")

	// Lectura.
	code, got, _ = e.raw("GET", hb+"/"+id, e.keeper, nil)
	if code != 200 || got["name"] != "Navidad" || got["date"] != "2031-12-25" || got["boundary"] != spain {
		t.Fatalf("get: %d %v", code, got)
	}
	e.must(func() int { s, _, _ := e.raw("GET", hb+"/nada", e.keeper, nil); return s }(), 400, "not an id")
	e.must(func() int { s, _, _ := e.raw("GET", hb+"/"+fw.NewUUID().String(), e.keeper, nil); return s }(), 404, "nobody knows it")

	// Lista por GET y por POST.
	q := "?boundary=" + spain + "&year=2031"
	code, page, _ = e.raw("GET", hb+q+"&sortBy=date&sortOrder=desc&pageSize=2&page=2", e.keeper, nil)
	if code != 200 || field(page, "date") != "2031-01-06,2031-01-01" || page["totalCount"] != float64(4) || page["totalPages"] != float64(2) ||
		page["pageNumber"] != float64(2) || page["pageSize"] != float64(2) {
		t.Fatalf("list: %d %v", code, page)
	}
	if _, page, _ = e.raw("GET", hb+q+"&search=epif", e.keeper, nil); field(page, "name") != "Epifanía" {
		t.Fatalf("search: %v", page)
	}
	for _, bad := range []string{"&sortBy=boundary", "&sortOrder=up", "&page=0", "&pageSize=1001", "&year=dos", "&year=3000"} {
		e.must(func() int { s, _, _ := e.raw("GET", hb+"?boundary="+spain+bad, e.keeper, nil); return s }(), 400, bad)
	}
	e.must(func() int { s, _, _ := e.raw("GET", hb+"?boundary=nada", e.keeper, nil); return s }(), 400, "not a boundary")
	code, page, _ = e.raw("POST", hb+"/search", e.keeper, map[string]any{"filter": map[string]any{"boundary": spain, "year": 2031},
		"pagination": map[string]any{"pageSize": 3, "orderBy": "name", "ascending": true}})
	if code != 200 || field(page, "name") != "Año Nuevo,Epifanía,Fiesta Nacional" || page["pageNumber"] != float64(1) {
		t.Fatalf("search by POST: %d %v", code, page)
	}
	for _, n := range []int{0, -1} {
		e.must(func() int {
			s, _, _ := e.raw("POST", hb+"/search", e.keeper, map[string]any{"pagination": map[string]any{"pageNumber": n}})
			return s
		}(), 400, "page out of range by POST")
	}
	e.must(func() int {
		s, _, _ := e.raw("POST", hb+"/search", e.keeper, map[string]any{"filter": map[string]any{"name": []string{"a"}}})
		return s
	}(), 400, "a filter that is not a single value")
	e.must(func() int { s, _, _ := e.raw("GET", hb, e.reader, nil); return s }(), 403, "list without the permission")

	// Cambio: lo leído con su versión; el nombre, recortado.
	got["name"] = " Natividad   del Señor "
	code, changed, _ := e.raw("PUT", hb+"/"+id, e.keeper, got)
	if code != 200 || changed["name"] != "Natividad del Señor" || changed["version"] != got["version"].(float64)+1 {
		t.Fatalf("update: %d %v", code, changed)
	}
	e.must(func() int { s, _, _ := e.raw("PUT", hb+"/"+id, e.keeper, got); return s }(), 409, "the version read before")
	delete(got, "version")
	code, problem, _ = e.raw("PUT", hb+"/"+id, e.keeper, got)
	if errs, _ := problem["errors"].(map[string]any); code != 400 || errs["version"] == nil {
		t.Fatalf("without a version: %d %v", code, problem)
	}
	v := changed["version"]
	for f, val := range map[string]string{"date": "2031-12-26", "boundary": fw.NewUUID().String()} {
		code, problem, _ = e.raw("PUT", hb+"/"+id, e.keeper, map[string]any{f: val, "version": v})
		if errs, _ := problem["errors"].(map[string]any); code != 400 || errs[f] == nil {
			t.Fatalf("%s does not change: %d %v", f, code, problem)
		}
	}
	code, problem, _ = e.raw("PUT", hb+"/"+id, e.keeper, map[string]any{"name": " ", "version": v})
	if errs, _ := problem["errors"].(map[string]any); code != 400 || errs["name"] == nil {
		t.Fatalf("no name: %d %v", code, problem)
	}
	// Lo que no cambia se compara por su valor, y un cambio sin cambios no gasta versión, pero
	// exige el permiso de escribir.
	code, changed, _ = e.raw("PUT", hb+"/"+id, e.keeper, map[string]any{"boundary": strings.ToUpper(spain), "date": "2031-12-25", "name": "Natividad del Señor ", "version": v})
	if code != 200 || changed["version"] != v {
		t.Fatalf("an update that changes nothing: %d %v", code, changed)
	}
	e.must(func() int { s, _, _ := e.raw("PUT", hb+"/"+id, e.reader, map[string]any{"version": v}); return s }(), 403, "update without the permission")

	// Baja: lo borra; repetirla no es un error, pero exige el permiso.
	e.must(func() int { s, _, _ := e.raw("DELETE", hb+"/"+id, e.reader, nil); return s }(), 403, "delete without the permission")
	e.must(func() int { s, _, _ := e.raw("DELETE", hb+"/"+id, e.keeper, nil); return s }(), 204, "delete")
	e.must(func() int { s, _, _ := e.raw("DELETE", hb+"/"+id, e.keeper, nil); return s }(), 204, "delete again")
	e.must(func() int { s, _, _ := e.raw("DELETE", hb+"/"+id, e.reader, nil); return s }(), 403, "delete again without the permission")
	e.must(func() int { s, _, _ := e.raw("GET", hb+"/"+id, e.keeper, nil); return s }(), 404, "deleted")
	e.must(func() int { s, _, _ := e.raw("DELETE", hb+"/nada", e.keeper, nil); return s }(), 400, "delete not an id")
	e.must(func() int { s, _, _ := e.raw("GET", hb, "", nil); return s }(), 401, "nobody")
}

// idsFilter: el filtro ids de las listas de festivos y de países (B-15,
// docs/CONTRATO-MANTENIMIENTO.md). Los festivos no tienen ámbito: basta el permiso. Los países se
// piden por su alfa-2; uno bien formado que no existe sólo no está.
func (e *env) idsFilter(year int) {
	t := e.t
	es, err := e.mod.Ports.Country(context.Background(), "ES")
	if err != nil {
		t.Fatal(err)
	}
	hb := geography.HolidaysBase
	create := func(day, name string) string {
		code, out, _ := e.raw("POST", hb, e.keeper, map[string]any{"boundary": es.Boundary, "date": fmt.Sprintf("%d-%s", year, day), "name": name})
		e.must(code, 201, "create "+name)
		return out["id"].(string)
	}
	mine := [2]string{create("03-03", "Ids uno"), create("04-04", "Ids dos")}
	testkit.CheckIDsFilter(t, testkit.IDsFilterCase{Base: hb,
		Do: func(method, path string, body any) (int, map[string]any) {
			code, out, _ := e.raw(method, path, e.keeper, body)
			return code, out
		},
		Denied: func(method, path string, body any) int {
			code, _, _ := e.raw(method, path, e.reader, body)
			return code
		},
		Mine: mine, And: "search=uno",
		Missing: fw.NewUUID().String(), Zero: fw.UUID{}.String(), Bad: "junk", Many: testkit.ManyUUIDs})

	cb := geography.CountriesBase
	alpha2 := func(what string, code int, page map[string]any) string {
		t.Helper()
		if code != 200 {
			t.Fatalf("%s: %d %v", what, code, page)
		}
		var out []string
		for _, it := range page["items"].([]any) {
			out = append(out, it.(map[string]any)["id"].(string))
		}
		return strings.Join(out, ",")
	}
	code, page, _ := e.raw("GET", cb+"?ids=es,FR,XX,es", e.reader, nil)
	if got := alpha2("countries by ids", code, page); got != "ES,FR" {
		t.Fatalf("countries by ids: %s", got)
	}
	code, page, _ = e.raw("POST", cb+"/search", e.reader, map[string]any{"filter": map[string]any{"ids": []string{"pt", "es"}, "search": "spain"}})
	if got := alpha2("countries by POST", code, page); got != "ES" {
		t.Fatalf("countries by ids and search: %s", got)
	}
	for _, q := range []string{"?ids=XX", "?ids="} {
		code, page, _ = e.raw("GET", cb+q, e.reader, nil)
		if got := alpha2(q, code, page); got != "" || page["totalCount"] != float64(0) {
			t.Fatalf("%s: %v", q, page)
		}
	}
	for _, bad := range []string{"ESP", "1A", "e", "ñ"} {
		code, page, _ = e.raw("GET", cb+"?ids=ES,"+url.QueryEscape(bad), e.reader, nil)
		codes, _ := page["errorCodes"].(map[string]any)
		if got, _ := codes["ids"].([]any); code != 400 || len(got) != 1 || got[0] != "format" {
			t.Fatalf("%s: %d %v", bad, code, page)
		}
	}
	code, page, _ = e.raw("POST", cb+"/search", e.reader, map[string]any{"filter": map[string]any{"ids": []any{"ES", true}}})
	if codes, _ := page["errorCodes"].(map[string]any); code != 400 || codes["ids"] == nil {
		t.Fatalf("a boolean id: %d %v", code, page)
	}
	// Sin permiso de leer países: 403, con la lista vacía, con un alfa-2 mal formado y con una
	// bandera que no vale, y en POST con un id que no es cadena: el permiso va antes de leer o
	// validar nada (docs/CONTRATO-MANTENIMIENTO.md).
	for _, q := range []string{"?ids=", "?ids=ESP", "?eu=x", "?pageSize=0"} {
		e.must(func() int { s, _, _ := e.raw("GET", cb+q, e.keeper, nil); return s }(), 403, "countries "+q+" without the permission")
	}
	code, page, _ = e.raw("POST", cb+"/search", e.keeper, map[string]any{"filter": map[string]any{"ids": []any{1}}})
	if code != 403 {
		t.Fatalf("a number as id without the permission: %d %v", code, page)
	}
	code, page, _ = e.raw("POST", cb+"/search", e.reader, map[string]any{"filter": map[string]any{"ids": strings.Repeat("ES,", 1001)}})
	if got := alpha2("the same code 1001 times", code, page); got != "ES" {
		t.Fatalf("repeated codes: %s", got)
	}
}
