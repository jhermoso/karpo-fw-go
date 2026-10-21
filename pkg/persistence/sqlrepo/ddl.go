package sqlrepo

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Dialects enumera los nombres de dialecto con soporte de DDL en RenderDDL.
var Dialects = []string{"sqlite", "postgres", "sqlserver", "oracle", "mysql"}

var logicalTypes = map[string]map[string]string{
	"sqlite": {"uuid": "TEXT", "str": "TEXT", "text": "TEXT", "bool": "INTEGER", "ts": "TEXT", "date": "TEXT", "int": "INTEGER",
		"bigint": "INTEGER", "dec": "TEXT", "false": "0", "add": "ADD COLUMN %s", "addEnd": ""},
	"postgres": {"uuid": "UUID", "str": "VARCHAR(%s)", "text": "TEXT", "bool": "BOOLEAN", "ts": "TIMESTAMPTZ", "date": "DATE",
		"int": "INTEGER", "bigint": "BIGINT", "dec": "NUMERIC(%s)", "false": "FALSE", "add": "ADD COLUMN %s", "addEnd": ""},
	"sqlserver": {"uuid": "UNIQUEIDENTIFIER", "str": "NVARCHAR(%s)", "text": "NVARCHAR(MAX)", "bool": "BIT", "ts": "DATETIME2(7)",
		"date": "DATE", "int": "INT", "bigint": "BIGINT", "dec": "DECIMAL(%s)", "false": "0", "add": "ADD %s", "addEnd": ""},
	"oracle": {"uuid": "RAW(16)", "str": "VARCHAR2(%s CHAR)", "text": "CLOB", "bool": "NUMBER(1)", "ts": "TIMESTAMP(6) WITH TIME ZONE",
		"date": "DATE", "int": "NUMBER(10)", "bigint": "NUMBER(19)", "dec": "NUMBER(%s)", "false": "0", "add": "ADD (%s", "addEnd": ")"},
	"mysql": {"uuid": "CHAR(36)", "str": "VARCHAR(%s)", "text": "LONGTEXT", "bool": "BOOLEAN", "ts": "DATETIME(6)", "date": "DATE",
		"int": "INT", "bigint": "BIGINT", "dec": "DECIMAL(%s)", "false": "FALSE", "add": "ADD COLUMN %s", "addEnd": ""},
}

var ddlPlaceholder = regexp.MustCompile(`\{(\w+)(?::([^{}]*))?\}`)

// MaxDecimalPrecision es la precisión máxima de una columna {dec:p,s}: la que cabe en el decimal
// de C# (28 cifras significativas).
const MaxDecimalPrecision = 28

// RenderDDL escribe DDL portable para un dialecto: {uuid}, {str:N}, {text}, {bool}, {ts},
// {date}, {int}, {bigint} y {dec:p,s} se convierten en los tipos del motor (los que espera el
// enlace de valores de cada dialecto: RAW(16) en Oracle, UNIQUEIDENTIFIER en SQL Server...),
// {false} en su literal falso y {add:column} ... {addEnd} en su cláusula ALTER TABLE ADD. Las
// columnas de texto opcionales deben admitir nulos: Oracle guarda "" como NULL.
//
// {str:N} admite N caracteres, contados como los cuenta el dominio (runas), en todos los motores:
// en Oracle es VARCHAR2(N CHAR), porque un VARCHAR2(N) a secas cuenta bytes y rechaza texto
// acentuado cerca de su límite. Quedan dos límites del motor. Un VARCHAR2 de Oracle nunca guarda
// más de 4000 bytes (MAX_STRING_SIZE=STANDARD), así que sólo N <= 1000 está garantizado para
// cualquier texto: por encima, use {text}. El NVARCHAR(N) de SQL Server cuenta unidades de
// código UTF-16, así que los caracteres fuera del plano multilingüe básico (emoji) ocupan dos.
//
// {dec:p,s} es un decimal exacto de p cifras, s de ellas decimales (1 <= p <= 28, 0 <= s <= p):
// NUMERIC(p,s) en PostgreSQL, DECIMAL(p,s) en SQL Server y MySQL, NUMBER(p,s) en Oracle y TEXT en
// SQLite, porque el NUMERIC de SQLite convierte a coma flotante y pierde cifras. Las columnas
// decimales se declaran también en Mapping.Decimals, para que se comparen y ordenen por valor.
//
// RenderDDL entra en pánico ante un marcador mal formado (como regexp.MustCompile: el DDL es una
// constante del programa); RenderDDLChecked devuelve el error.
func RenderDDL(dialect, ddl string) string {
	out, err := RenderDDLChecked(dialect, ddl)
	if err != nil {
		panic(err)
	}
	return out
}

// RenderDDLChecked es RenderDDL con el error de un marcador mal formado ({dec:p,s} fuera de rango
// o sin precisión y escala) en lugar del pánico.
func RenderDDLChecked(dialect, ddl string) (string, error) {
	types := logicalTypes[dialect]
	var firstErr error
	out := ddlPlaceholder.ReplaceAllStringFunc(ddl, func(m string) string {
		p := ddlPlaceholder.FindStringSubmatch(m)
		arg := p[2]
		if p[1] == "dec" {
			prec, scale, err := parseDecimalMarker(arg)
			if err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("sqlrepo: %s: %w", m, err)
				}
				return m
			}
			arg = strconv.Itoa(prec) + "," + strconv.Itoa(scale)
		}
		t, ok := types[p[1]]
		if !ok {
			return m
		}
		if strings.Contains(t, "%s") {
			t = fmt.Sprintf(t, arg)
		}
		return t
	})
	if firstErr != nil {
		return "", firstErr
	}
	return out, nil
}

// parseDecimalMarker lee la precisión y la escala de un marcador {dec:p,s}.
func parseDecimalMarker(arg string) (prec, scale int, err error) {
	ps := strings.Split(arg, ",")
	if len(ps) != 2 {
		return 0, 0, fmt.Errorf("a decimal column needs precision and scale, as in {dec:18,2}")
	}
	prec, err1 := strconv.Atoi(strings.TrimSpace(ps[0]))
	scale, err2 := strconv.Atoi(strings.TrimSpace(ps[1]))
	if err1 != nil || err2 != nil {
		return 0, 0, fmt.Errorf("precision and scale must be integers")
	}
	if prec < 1 || prec > MaxDecimalPrecision {
		return 0, 0, fmt.Errorf("precision %d out of range 1..%d", prec, MaxDecimalPrecision)
	}
	if scale < 0 || scale > prec {
		return 0, 0, fmt.Errorf("scale %d out of range 0..%d", scale, prec)
	}
	return prec, scale, nil
}

// RenderDDLAll genera las sentencias para cada dialecto de Dialects (un mapa de Migration.Up).
// Como RenderDDL, entra en pánico ante un marcador mal formado.
func RenderDDLAll(stmts ...string) map[string][]string {
	out := make(map[string][]string, len(Dialects))
	for _, d := range Dialects {
		for _, s := range stmts {
			out[d] = append(out[d], RenderDDL(d, s))
		}
	}
	return out
}
