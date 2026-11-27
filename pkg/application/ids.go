package application

import (
	"fmt"
	"strings"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// ParseIDs interpreta el filtro ids de una lista (docs/CONTRATO-MANTENIMIENTO.md): parte cada
// valor por comas, recorta, descarta los vacíos e interpreta el resto con parse. Un valor que no
// se interpreta, o que da la identidad cero, añade a v un solo error ("ids", "format") que nombra
// el primero. Después quita los duplicados por el valor interpretado y, si quedan más de
// domain.MaxPageSize, añade ("ids", "range"). Devuelve los ids en orden de primera aparición, o
// nil si ha registrado algún error.
func ParseIDs[T comparable](v *domain.Validation, raw []string, parse func(string) (T, error)) []T {
	var zero T
	var out []T
	seen := map[T]bool{}
	for _, chunk := range raw {
		for _, s := range strings.Split(chunk, ",") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			id, err := parse(s)
			if err != nil || id == zero {
				v.Add("ids", "format", fmt.Sprintf("invalid id: %s", s))
				return nil
			}
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	if len(out) > domain.MaxPageSize {
		v.Add("ids", "range", fmt.Sprintf("at most %d ids", domain.MaxPageSize))
		return nil
	}
	if out == nil {
		out = []T{}
	}
	return out
}
