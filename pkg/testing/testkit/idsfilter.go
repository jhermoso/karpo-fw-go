package testkit

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// IDsFilterCase describe una lista del contrato de mantenimiento para CheckIDsFilter.
type IDsFilterCase struct {
	Base string
	// Do hace una petición como un usuario que puede leer la lista; devuelve el estado y el cuerpo.
	Do func(method, path string, body any) (int, map[string]any)
	// Denied hace una petición como un usuario sin el permiso de leerla; nil no lo prueba.
	Denied func(method, path string, body any) int
	// Mine son dos identidades que el usuario de Do ve.
	Mine [2]string
	// Foreign es la identidad de un registro de otra organización; vacía si la lista no tiene ámbito.
	Foreign string
	// And es un filtro «clave=valor» que deja Mine[0] y quita Mine[1]; vacío no lo prueba.
	And string
	// Missing es una identidad bien formada que no existe.
	Missing string
	// Zero es la identidad cero; vacía no lo prueba.
	Zero string
	// Bad es un valor mal formado.
	Bad string
	// Many genera n identidades distintas bien formadas, para el tope; nil no lo prueba.
	Many func(n int) []string
}

// CheckIDsFilter comprueba el filtro ids de una lista (docs/CONTRATO-MANTENIMIENTO.md) por GET y
// por POST …/search: lo pedido y nada más, inexistentes y ajenos omitidos, el AND con otro filtro,
// la lista vacía, el permiso y los errores type, format y range.
func CheckIDsFilter(t *testing.T, c IDsFilterCase) {
	t.Helper()
	items := func(what string, code int, page map[string]any) []string {
		t.Helper()
		if code != 200 {
			t.Fatalf("%s: status %d %v", what, code, page)
		}
		var out []string
		list, _ := page["items"].([]any)
		for _, it := range list {
			id, _ := it.(map[string]any)["id"].(string)
			out = append(out, strings.ToUpper(id))
		}
		slices.Sort(out)
		return out
	}
	want := func(ids ...string) []string {
		out := make([]string, 0, len(ids))
		for _, id := range ids {
			out = append(out, strings.ToUpper(id))
		}
		slices.Sort(out)
		return out
	}
	errorCode := func(what string, code int, page map[string]any, wantCode string) {
		t.Helper()
		codes, _ := page["errorCodes"].(map[string]any)
		got, _ := codes["ids"].([]any)
		if code != 400 || len(got) != 1 || got[0] != wantCode {
			t.Fatalf("%s: want 400 ids/%s, got %d %v", what, wantCode, code, page)
		}
	}
	search := func(ids any, extra map[string]any) (int, map[string]any) {
		filter := map[string]any{"ids": ids}
		for k, v := range extra {
			filter[k] = v
		}
		return c.Do("POST", c.Base+"/search", map[string]any{"filter": filter, "pagination": map[string]any{"pageSize": 1000}})
	}
	asked := []string{c.Mine[0], c.Mine[1], c.Missing}
	if c.Foreign != "" {
		asked = append(asked, c.Foreign)
	}

	// Lo pedido y nada más: el inexistente y el de otra organización se omiten sin error.
	code, page := c.Do("GET", c.Base+"?pageSize=1000&ids="+strings.Join(asked, ","), nil)
	if got := items("GET", code, page); !slices.Equal(got, want(c.Mine[:]...)) {
		t.Fatalf("GET ids: %v", got)
	}
	code, page = c.Do("GET", c.Base+"?ids="+c.Mine[0]+"&ids="+c.Mine[0]+"&ids="+c.Missing, nil)
	if got := items("GET repeated", code, page); !slices.Equal(got, want(c.Mine[0])) {
		t.Fatalf("GET repeated ids: %v", got)
	}
	code, page = search(asked, nil)
	if got := items("POST", code, page); !slices.Equal(got, want(c.Mine[:]...)) {
		t.Fatalf("POST ids: %v", got)
	}
	code, page = search(c.Mine[1]+" , "+c.Missing, nil)
	if got := items("POST string", code, page); !slices.Equal(got, want(c.Mine[1])) {
		t.Fatalf("POST ids as a string: %v", got)
	}
	if c.Foreign != "" {
		code, page = search([]string{c.Foreign}, nil)
		if got := items("POST foreign", code, page); len(got) != 0 {
			t.Fatalf("another organization's id: %v", got)
		}
	}

	// AND con otro filtro.
	if c.And != "" {
		code, page = c.Do("GET", c.Base+"?ids="+c.Mine[0]+","+c.Mine[1]+"&"+c.And, nil)
		if got := items("GET and", code, page); !slices.Equal(got, want(c.Mine[0])) {
			t.Fatalf("GET ids and %s: %v", c.And, got)
		}
		k, v, _ := strings.Cut(c.And, "=")
		code, page = search([]string{c.Mine[0], c.Mine[1]}, map[string]any{k: v})
		if got := items("POST and", code, page); !slices.Equal(got, want(c.Mine[0])) {
			t.Fatalf("POST ids and %s: %v", c.And, got)
		}
	}

	// Vacío: una página vacía, no la lista entera.
	for what, call := range map[string]func() (int, map[string]any){
		"GET empty":  func() (int, map[string]any) { return c.Do("GET", c.Base+"?ids=", nil) },
		"GET commas": func() (int, map[string]any) { return c.Do("GET", c.Base+"?ids=,%20,", nil) },
		"POST empty": func() (int, map[string]any) { return search([]string{}, nil) },
	} {
		code, page := call()
		if got := items(what, code, page); len(got) != 0 || page["totalCount"] != float64(0) {
			t.Fatalf("%s: %v", what, page)
		}
	}
	// null es no filtrar.
	code, page = search(nil, nil)
	if got := items("POST null", code, page); len(got) < 2 {
		t.Fatalf("POST ids null filters: %v", got)
	}

	// Errores.
	code, page = c.Do("GET", c.Base+"?ids="+c.Mine[0]+","+c.Bad, nil)
	errorCode("GET bad", code, page, "format")
	code, page = search([]string{c.Bad}, nil)
	errorCode("POST bad", code, page, "format")
	code, page = search([]any{c.Mine[0], 7}, nil)
	errorCode("POST number", code, page, "type")
	if c.Zero != "" {
		code, page = c.Do("GET", c.Base+"?ids="+c.Zero, nil)
		errorCode("GET zero", code, page, "format")
	}
	if c.Many != nil {
		code, page = search(c.Many(1001), nil)
		errorCode("POST 1001", code, page, "range")
		ids := append(c.Many(999), c.Mine[0])
		code, page = search(append(ids, c.Mine[0]), nil)
		if got := items("POST 1000", code, page); !slices.Equal(got, want(c.Mine[0])) {
			t.Fatalf("POST 1000 ids: %v", got)
		}
	}

	// Sin permiso, 403, también con la lista vacía.
	if c.Denied != nil {
		for _, path := range []string{c.Base + "?ids=", c.Base + "?ids=" + c.Mine[0]} {
			if code := c.Denied("GET", path, nil); code != 403 {
				t.Fatalf("GET %s without permission: %d", path, code)
			}
		}
		if code := c.Denied("POST", c.Base+"/search", map[string]any{"filter": map[string]any{"ids": []string{}}}); code != 403 {
			t.Fatalf("POST without permission: %d", code)
		}
	}
}

// ManyUUIDs devuelve n identidades distintas bien formadas que no existen.
func ManyUUIDs(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("00000000-0000-4000-8000-%012d", i+1)
	}
	return out
}
