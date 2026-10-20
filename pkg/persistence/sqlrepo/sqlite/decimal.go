package sqlite

import (
	"fmt"
	"strings"

	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// Decimales en SQLite. Una columna {dec:p,s} es TEXT con el número escrito con punto ("-12.5"),
// porque el NUMERIC de SQLite lo convertiría a coma flotante y perdería cifras. Como el orden del
// texto no es el numérico ("10" < "9", "-1" < "-10"), las comparaciones y la ordenación de las
// columnas decimales usan una clave de texto cuyo orden byte a byte sí es el numérico, exacta para
// cualquier número de cifras:
//
//	cero:      "1"
//	positivo:  "2" + cifras enteras (3 dígitos) + parte entera + parte decimal
//	negativo:  "0" + (999 - cifras enteras) + parte entera y decimal complementadas + "~"
//
// La parte entera va sin ceros a la izquierda y la decimal sin ceros a la derecha, así que
// "2.50" y "2.5" tienen la misma clave. En los negativos cada cifra d se escribe como la letra
// 'j'-d (0 es "j", 9 es "a"), lo que invierte el orden, y la "~" final (mayor que cualquier letra)
// hace que un número más corto sea mayor: -1.5 > -1.54.
//
// La clave de la columna la calcula SQLite (DecimalKeyExpr) y la del valor comparado, Go
// (DecimalKey); las pruebas comprueban que coinciden. La expresión no aprovecha índices sobre la
// columna: en SQLite (pruebas y desarrollo) se acepta a cambio de no depender de extensiones ni
// de funciones registradas en el driver.

// DecimalExpr devuelve el placeholder sin cambios: el decimal se guarda como texto.
func (Dialect) DecimalExpr(ph string, _, _ int) string { return ph }

// DecimalKeyExpr devuelve la expresión SQL de la clave de orden numérico de una columna decimal
// (NULL si la columna es NULL).
func (Dialect) DecimalKeyExpr(col string) string {
	abs := "ltrim(" + col + ", '-')"
	dot := "instr(" + abs + ", '.')"
	ip := "ltrim(CASE WHEN " + dot + " > 0 THEN substr(" + abs + ", 1, " + dot + " - 1) ELSE " + abs + " END, '0')"
	fp := "CASE WHEN " + dot + " > 0 THEN rtrim(substr(" + abs + ", " + dot + " + 1), '0') ELSE '' END"
	complement := "(" + ip + ") || (" + fp + ")"
	for d := '0'; d <= '9'; d++ {
		complement = fmt.Sprintf("replace(%s, '%c', '%c')", complement, d, 'j'-(d-'0'))
	}
	return "(CASE WHEN (" + ip + ") = '' AND (" + fp + ") = '' THEN '1'" +
		" WHEN substr(" + col + ", 1, 1) = '-' THEN '0' || printf('%03d', 999 - length(" + ip + ")) || " + complement + " || '~'" +
		" ELSE '2' || printf('%03d', length(" + ip + ")) || (" + ip + ") || (" + fp + ") END)"
}

// DecimalKey devuelve la clave de orden numérico de v, la misma que DecimalKeyExpr calcula para
// una columna que guarda v.
func (Dialect) DecimalKey(v vocab.Decimal) string {
	s := v.String()
	neg := strings.HasPrefix(s, "-")
	ip, fp, _ := strings.Cut(strings.TrimPrefix(s, "-"), ".")
	ip, fp = strings.TrimLeft(ip, "0"), strings.TrimRight(fp, "0")
	switch {
	case ip == "" && fp == "":
		return "1"
	case neg:
		digits := []byte(ip + fp)
		for i, c := range digits {
			digits[i] = 'j' - (c - '0')
		}
		return fmt.Sprintf("0%03d%s~", 999-len(ip), digits)
	default:
		return fmt.Sprintf("2%03d%s%s", len(ip), ip, fp)
	}
}

var _ sqlrepo.TextDecimals = Dialect{}
