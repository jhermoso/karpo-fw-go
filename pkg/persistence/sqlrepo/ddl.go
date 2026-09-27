package sqlrepo

import (
	"fmt"
	"regexp"
	"strings"
)

// Dialects lists the dialect names with DDL support in RenderDDL.
var Dialects = []string{"sqlite", "postgres", "sqlserver", "oracle", "mysql"}

var logicalTypes = map[string]map[string]string{
	"sqlite": {"uuid": "TEXT", "str": "TEXT", "text": "TEXT", "bool": "INTEGER", "ts": "TEXT", "date": "TEXT", "int": "INTEGER",
		"bigint": "INTEGER", "false": "0", "add": "ADD COLUMN %s", "addEnd": ""},
	"postgres": {"uuid": "UUID", "str": "VARCHAR(%s)", "text": "TEXT", "bool": "BOOLEAN", "ts": "TIMESTAMPTZ", "date": "DATE",
		"int": "INTEGER", "bigint": "BIGINT", "false": "FALSE", "add": "ADD COLUMN %s", "addEnd": ""},
	"sqlserver": {"uuid": "UNIQUEIDENTIFIER", "str": "NVARCHAR(%s)", "text": "NVARCHAR(MAX)", "bool": "BIT", "ts": "DATETIME2(7)",
		"date": "DATE", "int": "INT", "bigint": "BIGINT", "false": "0", "add": "ADD %s", "addEnd": ""},
	"oracle": {"uuid": "RAW(16)", "str": "VARCHAR2(%s)", "text": "CLOB", "bool": "NUMBER(1)", "ts": "TIMESTAMP(6) WITH TIME ZONE",
		"date": "DATE", "int": "NUMBER(10)", "bigint": "NUMBER(19)", "false": "0", "add": "ADD (%s", "addEnd": ")"},
	"mysql": {"uuid": "CHAR(36)", "str": "VARCHAR(%s)", "text": "LONGTEXT", "bool": "BOOLEAN", "ts": "DATETIME(6)", "date": "DATE",
		"int": "INT", "bigint": "BIGINT", "false": "FALSE", "add": "ADD COLUMN %s", "addEnd": ""},
}

var ddlPlaceholder = regexp.MustCompile(`\{(\w+)(?::([\w ]+))?\}`)

// RenderDDL writes portable DDL for a dialect: {uuid}, {str:N}, {text}, {bool}, {ts}, {date},
// {int} and {bigint} become the engine's types (the ones the value binding of each dialect
// expects: RAW(16) on Oracle, UNIQUEIDENTIFIER on SQL Server...), {false} its false literal and
// {add:column} ... {addEnd} its ALTER TABLE ADD clause. Optional text columns must be nullable:
// Oracle stores "" as NULL.
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

// RenderDDLAll renders statements for every dialect of Dialects (a Migration.Up map).
func RenderDDLAll(stmts ...string) map[string][]string {
	out := make(map[string][]string, len(Dialects))
	for _, d := range Dialects {
		for _, s := range stmts {
			out[d] = append(out[d], RenderDDL(d, s))
		}
	}
	return out
}
