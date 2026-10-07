package domain

import (
	"archive/zip"
	"bufio"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// Types of a column.
const (
	Text     = "text"
	Number   = "number"
	Date     = "date"
	DateTime = "datetime"
	Boolean  = "boolean"
)

// Column is a column of a list: where its value comes from, what heads it and how it is shown.
type Column struct {
	Field  string
	Header string
	Type   string
}

// Row is a row of a list: the value of each field. A value is text, a number (any integer or
// float, or a decimal given as text), a bool, a time or nil.
type Row map[string]any

// Words are the words a file uses for what is not data.
type Words struct {
	Yes, No string
}

// Spanish are the words of the C# exports.
var Spanish = Words{Yes: "Sí", No: "No"}

// Cell renders the value of a column as text and tells whether it is a number.
func Cell(c Column, v any, w Words) (string, bool) {
	switch x := v.(type) {
	case nil:
		return "", false
	case bool:
		if x {
			return w.Yes, false
		}
		return w.No, false
	case time.Time:
		if x.IsZero() {
			return "", false
		}
		if c.Type == DateTime {
			return x.UTC().Format("2006-01-02 15:04:05"), false
		}
		return x.Format("2006-01-02"), false
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprint(x), true
	case float32:
		return strconv.FormatFloat(float64(x), 'f', -1, 32), true
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), true
	case fmt.Stringer:
		return number(c, x.String())
	case string:
		return number(c, x)
	}
	return fmt.Sprint(v), false
}

// number tells whether a text of a number column is a plain decimal number.
func number(c Column, s string) (string, bool) {
	if c.Type != Number || s == "" {
		return s, false
	}
	digits := 0
	for i, r := range s {
		switch {
		case r >= '0' && r <= '9':
			digits++
		case r == '-' && i == 0, r == '.' && strings.Count(s, ".") == 1:
		default:
			return s, false
		}
	}
	return s, digits > 0 && digits <= 15 // beyond fifteen digits a sheet would round it
}

// Writer writes a list row by row.
type Writer interface {
	Write(r Row) error
	// Close ends the file; nothing may be written after it.
	Close() error
}

// guard keeps a spreadsheet from running a text as a formula (the C# wrote it as it came): what
// starts like one is written after an apostrophe.
func guard(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

type csvWriter struct {
	w       *bufio.Writer
	columns []Column
	words   Words
}

func csvField(s string) string {
	if strings.ContainsAny(s, ",\"\r\n") || strings.TrimSpace(s) != s {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}

func (c *csvWriter) line(fields []string) error {
	for i, f := range fields {
		if i > 0 {
			c.w.WriteByte(',')
		}
		c.w.WriteString(csvField(f))
	}
	_, err := c.w.WriteString("\r\n")
	return err
}

// NewCSV starts a CSV file: UTF-8 with its mark so a spreadsheet reads the accents, commas,
// CRLF on every machine, and the header first.
func NewCSV(out io.Writer, columns []Column, words Words) (Writer, error) {
	c := &csvWriter{w: bufio.NewWriter(out), columns: columns, words: words}
	c.w.WriteString(string(rune(0xFEFF)))
	headers := make([]string, len(columns))
	for i, col := range columns {
		headers[i] = guard(col.Header)
	}
	return c, c.line(headers)
}

func (c *csvWriter) Write(r Row) error {
	fields := make([]string, len(c.columns))
	for i, col := range c.columns {
		s, isNumber := Cell(col, r[col.Field], c.words)
		if !isNumber {
			s = guard(s)
		}
		fields[i] = s
	}
	return c.line(fields)
}

func (c *csvWriter) Close() error { return c.w.Flush() }

type xlsxWriter struct {
	zip     *zip.Writer
	sheet   *bufio.Writer
	columns []Column
	words   Words
	row     int
}

// SheetName makes a title fit a sheet: 31 characters, without the ones a sheet name cannot have.
func SheetName(title string) string {
	name := strings.TrimSpace(strings.Map(func(r rune) rune {
		if strings.ContainsRune(`[]:*?/\'`, r) || r < 0x20 {
			return ' '
		}
		return r
	}, title))
	if name == "" {
		name = "Export"
	}
	return clip(name, 31)
}

func ref(col, row int) string {
	name := ""
	for col++; col > 0; col = (col - 1) / 26 {
		name = string(rune('A'+(col-1)%26)) + name
	}
	return name + strconv.Itoa(row)
}

func escape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
			return -1 // not allowed in XML
		}
		return r
	}, s)))
	return b.String()
}

func (x *xlsxWriter) part(name, content string) error {
	w, err := x.zip.Create(name)
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`+"\n"+content)
	return err
}

// NewXLSX starts a workbook with one sheet, written as it goes (the C# built it whole in memory):
// texts as texts, numbers as numbers, the header first.
func NewXLSX(out io.Writer, title string, columns []Column, words Words) (Writer, error) {
	x := &xlsxWriter{zip: zip.NewWriter(out), columns: columns, words: words}
	parts := [][2]string{
		{"[Content_Types].xml", `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
			`<Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>` +
			`<Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>`},
		{"_rels/.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`},
		{"xl/workbook.xml", `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" ` +
			`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="` + escape(SheetName(title)) +
			`" sheetId="1" r:id="rId1"/></sheets></workbook>`},
		{"xl/_rels/workbook.xml.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`},
	}
	for _, p := range parts {
		if err := x.part(p[0], p[1]); err != nil {
			return nil, err
		}
	}
	w, err := x.zip.Create("xl/worksheets/sheet1.xml")
	if err != nil {
		return nil, err
	}
	x.sheet = bufio.NewWriter(w)
	x.sheet.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n" +
		`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`)
	cells := make([][2]string, len(columns))
	for i, c := range columns {
		cells[i] = [2]string{c.Header, ""}
	}
	return x, x.line(cells)
}

// line writes a row of cells: each a text, or a number when its second value says so.
func (x *xlsxWriter) line(cells [][2]string) error {
	x.row++
	fmt.Fprintf(x.sheet, `<row r="%d">`, x.row)
	for i, c := range cells {
		switch {
		case c[0] == "":
		case c[1] == "n":
			fmt.Fprintf(x.sheet, `<c r="%s"><v>%s</v></c>`, ref(i, x.row), c[0])
		default:
			fmt.Fprintf(x.sheet, `<c r="%s" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, ref(i, x.row), escape(c[0]))
		}
	}
	_, err := x.sheet.WriteString(`</row>`)
	return err
}

func (x *xlsxWriter) Write(r Row) error {
	cells := make([][2]string, len(x.columns))
	for i, col := range x.columns {
		s, isNumber := Cell(col, r[col.Field], x.words)
		cells[i] = [2]string{s, ""}
		if isNumber {
			cells[i][1] = "n"
		}
	}
	return x.line(cells)
}

func (x *xlsxWriter) Close() error {
	x.sheet.WriteString(`</sheetData></worksheet>`)
	if err := x.sheet.Flush(); err != nil {
		return err
	}
	return x.zip.Close()
}

// NewWriter starts a file of a format.
func NewWriter(format string, out io.Writer, title string, columns []Column, words Words) (Writer, error) {
	if format == XLSX {
		return NewXLSX(out, title, columns, words)
	}
	return NewCSV(out, columns, words)
}

// ContentType returns the media type of a format.
func ContentType(format string) string {
	if format == XLSX {
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	}
	return "text/csv; charset=utf-8"
}
