package vocab

import (
	"strings"
	"unicode/utf8"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// MaxPremisesPiece es la longitud máxima de una pieza de Premises.
const MaxPremisesPiece = 10

// Premises dice en qué punto de la calle está una dirección, en las piezas que los formularios
// españoles piden una a una: "Mayor 5 bis, bloque 2, portal A, escalera B, 3.º izquierda". Todas
// opcionales. Una dirección que las tiene deja sólo el nombre de la calle en su primera línea.
type Premises struct {
	Number    string // número de la calle: "5", "5-7", "s/n", "km 12,5"
	Qualifier string // lo que sigue al número: "bis", "duplicado"
	Block     string
	Portal    string
	Staircase string
	Floor     string
	Door      string // puerta o letra: "izquierda", "B"
}

// IsZero dice si no se ha dado ninguna pieza.
func (p Premises) IsZero() bool { return p == Premises{} }

// Normalized recorta las piezas y colapsa sus espacios. Cada pieza admite como mucho
// MaxPremisesPiece caracteres; los errores nombran el campo bajo prefix ("address.floor").
func (p Premises) Normalized(prefix string) (Premises, error) {
	clean := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	p = Premises{Number: clean(p.Number), Qualifier: clean(p.Qualifier), Block: clean(p.Block), Portal: clean(p.Portal),
		Staircase: clean(p.Staircase), Floor: clean(p.Floor), Door: clean(p.Door)}
	if prefix != "" {
		prefix += "."
	}
	var v domain.Validation
	for _, f := range []struct{ name, value string }{{"number", p.Number}, {"numberQualifier", p.Qualifier}, {"block", p.Block},
		{"portal", p.Portal}, {"staircase", p.Staircase}, {"floor", p.Floor}, {"door", p.Door}} {
		v.Require(utf8.RuneCountInString(f.value) <= MaxPremisesPiece, prefix+f.name, "length", "too long")
	}
	return p, v.Err()
}

// String escribe las piezas a la manera española, para ir detrás del nombre de la calle.
func (p Premises) String() string {
	out := strings.TrimSpace(p.Number + " " + p.Qualifier)
	for _, part := range []struct{ label, value string }{{"bloque", p.Block}, {"portal", p.Portal}, {"escalera", p.Staircase},
		{"planta", p.Floor}, {"puerta", p.Door}} {
		if part.value == "" {
			continue
		}
		if out != "" {
			out += ", "
		}
		out += part.label + " " + part.value
	}
	return out
}
