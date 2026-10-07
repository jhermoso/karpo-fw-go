package domain

import (
	"encoding/csv"
	"errors"
	"io"
	"strings"
	"time"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Table is a delimited file with a header. The C# had three hand-written parsers, one per source.
type Table struct {
	File   string
	header map[string]int
	Rows   []Row
}

// Row is a line of a table.
type Row struct {
	Line   int
	values []string
	table  *Table
}

// Fold lowers s and removes the accents and the repeated spaces, to compare names.
func Fold(s string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(strings.Join(strings.Fields(s), " ")) {
		switch c {
		case 'á', 'à', 'ä', 'â', 'ã':
			c = 'a'
		case 'é', 'è', 'ë', 'ê':
			c = 'e'
		case 'í', 'ì', 'ï', 'î':
			c = 'i'
		case 'ó', 'ò', 'ö', 'ô', 'õ':
			c = 'o'
		case 'ú', 'ù', 'ü', 'û':
			c = 'u'
		case 'ç':
			c = 'c'
		}
		b.WriteRune(c)
	}
	return b.String()
}

func column(s string) string {
	return strings.NewReplacer(" ", "", "_", "", "-", "").Replace(Fold(s))
}

// ParseTable reads a delimited file: the first line names the columns, quoted values may hold the
// delimiter and line breaks, empty lines are skipped.
func ParseTable(f File, delimiter rune) (*Table, error) {
	rd := csv.NewReader(strings.NewReader(strings.TrimPrefix(f.Content, string(rune(0xFEFF)))))
	rd.Comma, rd.FieldsPerRecord, rd.TrimLeadingSpace = delimiter, -1, true
	t := &Table{File: f.Name, header: map[string]int{}}
	for first := true; ; {
		rec, err := rd.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fw.Violation("imports.unreadable_file", f.Name+": "+err.Error())
		}
		line, _ := rd.FieldPos(0)
		if first {
			first = false
			for i, h := range rec {
				if _, dup := t.header[column(h)]; !dup && column(h) != "" {
					t.header[column(h)] = i
				}
			}
			continue
		}
		if len(rec) == 1 && strings.TrimSpace(rec[0]) == "" {
			continue
		}
		t.Rows = append(t.Rows, Row{Line: line, values: rec, table: t})
	}
	if len(t.header) == 0 {
		return nil, fw.Violation("imports.unreadable_file", f.Name+": the file has no header")
	}
	return t, nil
}

// Has reports whether the table has one of the columns.
func (t *Table) Has(names ...string) bool {
	for _, n := range names {
		if _, ok := t.header[column(n)]; ok {
			return true
		}
	}
	return false
}

// Get returns the value of the first of the columns the table has, trimmed. Names are compared
// without case, accents, spaces, dashes or underscores.
func (r Row) Get(names ...string) string {
	for _, n := range names {
		if i, ok := r.table.header[column(n)]; ok {
			if i < len(r.values) {
				return strings.TrimSpace(r.values[i])
			}
			return ""
		}
	}
	return ""
}

// Truth reads a yes/no value as the sources write it.
func Truth(s string) bool {
	switch Fold(s) {
	case "si", "yes", "true", "1", "s", "y", "x":
		return true
	}
	return false
}

// Day reads a date as the sources write it and returns it as YYYY-MM-DD ("" for an empty value).
func Day(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", true
	}
	for _, layout := range []string{"2006-01-02", "02/01/2006", "02.01.2006", "02-01-2006", "2006-01-02T15:04:05", time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("2006-01-02"), true
		}
	}
	return "", false
}
