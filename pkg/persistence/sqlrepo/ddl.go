package sqlrepo

import (
	"fmt"
	"regexp"
	"strings"
)

// Dialects enumera los nombres de dialecto con soporte de DDL en RenderDDL.
var Dialects = []string{"sqlite", "postgres", "sqlserver", "oracle", "mysql"}

var logicalTypes = map[string]map[string]string{
	"sqlite": {"uuid": "TEXT", "str": "TEXT", "text": "TEXT", "bool": "INTEGER", "ts": "TEXT", "date": "TEXT", "int": "INTEGER",
		"bigint": "INTEGER", "false": "0", "add": "ADD COLUMN %s", "addEnd": ""},
	"postgres": {"uuid": "UUID", "str": "VARCHAR(%s)", "text": "TEXT", "bool": "BOOLEAN", "ts": "TIMESTAMPTZ", "date": "DATE",
		"int": "INTEGER", "bigint": "BIGINT", "false": "FALSE", "add": "ADD COLUMN %s", "addEnd": ""},
	"sqlserver": {"uuid": "UNIQUEIDENTIFIER", "str": "NVARCHAR(%s)", "text": "NVARCHAR(MAX)", "bool": "BIT", "ts": "DATETIME2(7)",
		"date": "DATE", "int": "INT", "bigint": "BIGINT", "false": "0", "add": "ADD %s", "addEnd": ""},
	"oracle": {"uuid": "RAW(16)", "str": "VARCHAR2(%s CHAR)", "text": "CLOB", "bool": "NUMBER(1)", "ts": "TIMESTAMP(6) WITH TIME ZONE",
		"date": "DATE", "int": "NUMBER(10)", "bigint": "NUMBER(19)", "false": "0", "add": "ADD (%s", "addEnd": ")"},
	"mysql": {"uuid": "CHAR(36)", "str": "VARCHAR(%s)", "text": "LONGTEXT", "bool": "BOOLEAN", "ts": "DATETIME(6)", "date": "DATE",
		"int": "INT", "bigint": "BIGINT", "false": "FALSE", "add": "ADD COLUMN %s", "addEnd": ""},
}

var ddlPlaceholder = regexp.MustCompile(`\{(\w+)(?::([\w ]+))?\}`)

// RenderDDL escribe DDL portable para un dialecto: {uuid}, {str:N}, {text}, {bool}, {ts},
// {date}, {int} y {bigint} se convierten en los tipos del motor (los que espera el enlace de
// valores de cada dialecto: RAW(16) en Oracle, UNIQUEIDENTIFIER en SQL Server...), {false} en su
// literal falso y {add:column} ... {addEnd} en su cláusula ALTER TABLE ADD. Las columnas de texto
// opcionales deben admitir nulos: Oracle guarda "" como NULL.
//
// {str:N} admite N caracteres, contados como los cuenta el dominio (runas), en todos los motores:
// en Oracle es VARCHAR2(N CHAR), porque un VARCHAR2(N) a secas cuenta bytes y rechaza texto
// acentuado cerca de su límite. Quedan dos límites del motor. Un VARCHAR2 de Oracle nunca guarda
// más de 4000 bytes (MAX_STRING_SIZE=STANDARD), así que sólo N <= 1000 está garantizado para
// cualquier texto: por encima, use {text}. El NVARCHAR(N) de SQL Server cuenta unidades de
// código UTF-16, así que los caracteres fuera del plano multilingüe básico (emoji) ocupan dos.
func RenderDDL(dialect, ddl string) string {
	types := logicalTypes[dialect]
	return ddlPlaceholder.ReplaceAllStringFunc(ddl, func(m string) string {
		p := ddlPlaceholder.FindStringSubmatch(m)
		t, ok := types[p[1]]
		if !ok {
			return m
		}
		if strings.Contains(t, "%s") {
			t = fmt.Sprintf(t, p[2])
		}
		return t
	})
}

// RenderDDLAll genera las sentencias para cada dialecto de Dialects (un mapa de Migration.Up).
func RenderDDLAll(stmts ...string) map[string][]string {
	out := make(map[string][]string, len(Dialects))
	for _, d := range Dialects {
		for _, s := range stmts {
			out[d] = append(out[d], RenderDDL(d, s))
		}
	}
	return out
}
