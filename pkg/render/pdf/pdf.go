// Package pdf escribe documentos PDF sencillos con la biblioteca estándar: páginas A4, texto en
// posiciones absolutas con las fuentes base 14 Helvetica y Helvetica-Bold (WinAnsiEncoding, así
// que salen bien á, ñ, ü, €, º y ª), líneas y rectángulos. No comprime los flujos de contenido: el
// texto se puede buscar en el fichero, y las pruebas lo hacen.
//
// Las coordenadas son puntos (1/72 de pulgada) y se miden desde la esquina superior izquierda de la
// página, como se lee: y crece hacia abajo. El paquete las convierte a las de PDF, que empiezan
// abajo.
package pdf

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

// Tamaño de una página A4, en puntos.
const (
	A4Width  = 595.28
	A4Height = 841.89
)

// Font es una de las dos fuentes del documento.
type Font int

// Fuentes disponibles.
const (
	Regular Font = iota // Helvetica
	Bold                // Helvetica-Bold
)

var fontNames = [...]string{Regular: "Helvetica", Bold: "Helvetica-Bold"}

// Document es un documento en construcción.
type Document struct {
	pages []*Page
}

// New crea un documento vacío.
func New() *Document { return &Document{} }

// Page es una página A4 del documento.
type Page struct {
	content bytes.Buffer
}

// AddPage añade una página al final y la devuelve.
func (d *Document) AddPage() *Page {
	p := &Page{}
	d.pages = append(d.pages, p)
	return p
}

// Pages devuelve el número de páginas.
func (d *Document) Pages() int { return len(d.pages) }

// num escribe un número con como mucho dos decimales y sin ceros sobrantes.
func num(f float64) string {
	s := strconv.FormatFloat(f, 'f', 2, 64)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "-0" || s == "" {
		return "0"
	}
	return s
}

// Text escribe s con su línea base en (x, y).
func (p *Page) Text(x, y float64, f Font, size float64, s string) {
	fmt.Fprintf(&p.content, "BT /F%d %s Tf %s %s Td (%s) Tj ET\n", f+1, num(size), num(x), num(A4Height-y), escape(Encode(s)))
}

// TextRight escribe s de forma que acabe en x: así se alinean los importes por la derecha.
func (p *Page) TextRight(x, y float64, f Font, size float64, s string) {
	p.Text(x-Width(f, size, s), y, f, size, s)
}

// Line traza una línea de (x1, y1) a (x2, y2) con el grosor dado.
func (p *Page) Line(x1, y1, x2, y2, width float64) {
	fmt.Fprintf(&p.content, "%s w %s %s m %s %s l S\n", num(width), num(x1), num(A4Height-y1), num(x2), num(A4Height-y2))
}

// Rect traza el borde de un rectángulo cuya esquina superior izquierda es (x, y).
func (p *Page) Rect(x, y, w, h, width float64) {
	fmt.Fprintf(&p.content, "%s w %s %s %s %s re S\n", num(width), num(x), num(A4Height-y-h), num(w), num(h))
}

// escape protege los caracteres especiales de una cadena literal de PDF.
func escape(b []byte) string {
	var sb strings.Builder
	for _, c := range b {
		switch c {
		case '(', ')', '\\':
			sb.WriteByte('\\')
			sb.WriteByte(c)
		case '\n', '\r':
			sb.WriteByte(' ')
		default:
			sb.WriteByte(c)
		}
	}
	return sb.String()
}

// Bytes devuelve el documento terminado. Un documento sin páginas sale con una en blanco.
func (d *Document) Bytes() []byte {
	pages := d.pages
	if len(pages) == 0 {
		pages = []*Page{{}}
	}
	// Objetos: 1 catálogo, 2 árbol de páginas, 3 y 4 fuentes; después, por cada página, la
	// página y su flujo de contenido.
	var objects []string
	kids := make([]string, len(pages))
	for i := range pages {
		kids[i] = fmt.Sprintf("%d 0 R", 5+2*i)
	}
	objects = append(objects,
		"<< /Type /Catalog /Pages 2 0 R >>",
		fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(pages)))
	for _, name := range fontNames {
		objects = append(objects, "<< /Type /Font /Subtype /Type1 /BaseFont /"+name+" /Encoding /WinAnsiEncoding >>")
	}
	for i, p := range pages {
		objects = append(objects,
			fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %s %s] /Resources << /Font << /F1 3 0 R /F2 4 0 R >> >> /Contents %d 0 R >>",
				num(A4Width), num(A4Height), 6+2*i),
			fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", p.content.Len(), p.content.String()))
	}

	var out bytes.Buffer
	out.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n") // el comentario binario avisa de que el fichero no es texto
	offsets := make([]int, len(objects))
	for i, o := range objects {
		offsets[i] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets {
		fmt.Fprintf(&out, "%010d 00000 n \n", off) // cada entrada mide 20 bytes justos
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return out.Bytes()
}

// Page devuelve la página i (desde 0): sirve para escribir después lo que depende del total, como
// «página 1 de 3».
func (d *Document) Page(i int) *Page { return d.pages[i] }
