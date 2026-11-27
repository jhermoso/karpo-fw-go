package distribution

import (
	"net/url"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// IDsFromQuery lee el filtro ids de una lista por GET: ?ids=a,b y ?ids=a&ids=b. present es false
// si la clave no aparece; ?ids= cuenta como presente y vacío. No parte ni interpreta: eso lo hace
// la aplicación (application.ParseIDs).
func IDsFromQuery(v url.Values) (raw []string, present bool) {
	raw, present = v["ids"]
	return raw, present
}

// IDsFromFilter lee y borra filter["ids"] de una búsqueda por POST: un array de cadenas o una
// cadena con comas. Ausente o null es present=false. Un elemento que no es una cadena da un error
// de validación ("ids", "type").
func IDsFromFilter(filter map[string]any) (raw []string, present bool, err error) {
	x, ok := filter["ids"]
	delete(filter, "ids")
	if !ok || x == nil {
		return nil, false, nil
	}
	switch t := x.(type) {
	case string:
		return []string{t}, true, nil
	case []any:
		raw = make([]string, 0, len(t))
		for _, e := range t {
			s, ok := e.(string)
			if !ok {
				return nil, false, idsTypeError()
			}
			raw = append(raw, s)
		}
		return raw, true, nil
	default:
		return nil, false, idsTypeError()
	}
}

func idsTypeError() error {
	var v domain.Validation
	v.Add("ids", "type", "ids must be strings")
	return v.Err()
}
