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

// Tipos de una columna.
const (
	Text     = "text"
	Number   = "number"
	Date     = "date"
	DateTime = "datetime"
	Boolean  = "boolean"
)

// Column es una columna de una lista: de dónde sale su valor, qué la encabeza y cómo se muestra.
type Column struct {
	Field  string
	Header string
	Type   string
}

// Row es una fila de una lista: el valor de cada campo. Un valor es un texto, un número (cualquier
// entero o float, o un decimal dado como texto), un bool, un time o nil.
type Row map[string]any

// Words son las palabras que usa un fichero para lo que no son datos.
type Words struct {
	Yes, No string
}

// Spanish son las palabras de las exportaciones del C#.
var Spanish = Words{Yes: "Sí", No: "No"}

// Cell presenta el valor de una columna como texto y dice si es un número.
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

// number dice si un texto de una columna numérica es un número decimal simple.
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
	return s, digits > 0 && digits <= 15 // pasados quince dígitos, una hoja lo redondearía
}

// Writer escribe una lista fila a fila.
type Writer interface {
	Write(r Row) error
	// Close termina el fichero; después no se puede escribir nada.
	Close() error
}

// guard impide que una hoja de cálculo ejecute un texto como fórmula (el C# lo escribía tal como
// venía): lo que empieza como una se escribe tras un apóstrofo.
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

// NewCSV empieza un fichero CSV: UTF-8 con su marca (para que una hoja de cálculo lea los acentos),
// comas, CRLF en todas las máquinas y la cabecera primero.
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

// SheetName ajusta un título a una hoja: 31 caracteres, sin los que no puede llevar el nombre de
// una hoja.
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
			return -1 // no se admite en XML
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

// NewXLSX empieza un libro con una hoja, escrito sobre la marcha (el C# lo montaba entero en
// memoria): textos como textos, números como números, la cabecera primero.
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

// line escribe una fila de celdas: cada una un texto, o un número cuando lo dice su segundo valor.
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

// NewWriter empieza un fichero de un formato.
func NewWriter(format string, out io.Writer, title string, columns []Column, words Words) (Writer, error) {
	if format == XLSX {
		return NewXLSX(out, title, columns, words)
	}
	return NewCSV(out, columns, words)
}

// ContentType devuelve el tipo de medio de un formato.
func ContentType(format string) string {
	if format == XLSX {
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	}
	return "text/csv; charset=utf-8"
}
