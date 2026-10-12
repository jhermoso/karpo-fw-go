package vocab

import (
	"strings"
	"unicode/utf8"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// MaxPremisesPiece is the longest a piece of Premises may be.
const MaxPremisesPiece = 10

// Premises says where in the street an address is, in the pieces Spanish forms ask for one by
// one: "Mayor 5 bis, bloque 2, portal A, escalera B, 3.º izquierda". All optional. An address
// that has them keeps the name of the street alone in its first line.
type Premises struct {
	Number    string // street number: "5", "5-7", "s/n", "km 12,5"
	Qualifier string // what follows the number: "bis", "duplicado"
	Block     string
	Portal    string
	Staircase string
	Floor     string
	Door      string // door or letter: "izquierda", "B"
}

// IsZero reports whether no piece is given.
func (p Premises) IsZero() bool { return p == Premises{} }

// Normalized trims the pieces and collapses their spaces. Each piece holds MaxPremisesPiece
// characters at most; the errors name the field under prefix ("address.floor").
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

// String renders the pieces the Spanish way, to follow the name of the street.
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
