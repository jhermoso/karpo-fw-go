package pdf_test

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/jhermoso/karpo-fw-go/pkg/render/pdf"
)

func sample() []byte {
	d := pdf.New()
	p := d.AddPage()
	p.Text(50, 60, pdf.Bold, 14, "Factura FA-2026-000001")
	p.Text(50, 80, pdf.Regular, 10, "Formación (ñ, ü, º, ª): 1.234,56 €")
	p.TextRight(545, 100, pdf.Regular, 10, "185,41 €")
	p.Line(50, 110, 545, 110, 0.5)
	p.Rect(450, 700, 95, 95, 0.5)
	d.AddPage().Text(50, 60, pdf.Regular, 10, "Segunda página")
	return d.Bytes()
}

// El fichero empieza por la cabecera, acaba en %%EOF y la tabla de referencias cruzadas apunta a
// cada objeto: en su offset empieza «N 0 obj».
func TestDocument_Structure(t *testing.T) {
	b := sample()
	if !bytes.HasPrefix(b, []byte("%PDF-1.4\n")) {
		t.Fatalf("header: %q", b[:10])
	}
	if !bytes.HasSuffix(b, []byte("%%EOF\n")) {
		t.Fatalf("trailer: %q", b[len(b)-10:])
	}
	m := regexp.MustCompile(`startxref\n(\d+)\n%%EOF\n$`).FindSubmatch(b)
	if m == nil {
		t.Fatal("no startxref")
	}
	start, _ := strconv.Atoi(string(m[1]))
	if !bytes.HasPrefix(b[start:], []byte("xref\n0 ")) {
		t.Fatalf("startxref %d does not point to the xref table", start)
	}
	lines := strings.Split(string(b[start:]), "\n")
	var count int
	if _, err := fmt.Sscanf(lines[1], "0 %d", &count); err != nil {
		t.Fatal(err)
	}
	if count != 1+4+2*2 { // el objeto libre, catálogo, páginas, dos fuentes y dos por página
		t.Fatalf("objects: %d", count)
	}
	if lines[2] != "0000000000 65535 f " {
		t.Fatalf("free entry: %q", lines[2])
	}
	for n := 1; n < count; n++ {
		entry := lines[2+n]
		if len(entry)+1 != 20 || !strings.HasSuffix(entry, " 00000 n ") {
			t.Fatalf("entry %d: %q", n, entry)
		}
		off, _ := strconv.Atoi(entry[:10])
		if want := fmt.Sprintf("%d 0 obj\n", n); !bytes.HasPrefix(b[off:], []byte(want)) {
			t.Fatalf("object %d at %d: %q", n, off, b[off:off+12])
		}
	}
	if !bytes.Contains(b, []byte("/Count 2")) || !bytes.Contains(b, []byte("/BaseFont /Helvetica-Bold /Encoding /WinAnsiEncoding")) {
		t.Fatal("pages or fonts")
	}
	if bytes.Contains(b, []byte("FlateDecode")) {
		t.Fatal("streams are not compressed")
	}
}

// La longitud declarada de cada flujo es la de su contenido.
func TestDocument_StreamLengths(t *testing.T) {
	b := sample()
	re := regexp.MustCompile(`<< /Length (\d+) >>\nstream\n`)
	for _, loc := range re.FindAllSubmatchIndex(b, -1) {
		n, _ := strconv.Atoi(string(b[loc[2]:loc[3]]))
		if !bytes.HasPrefix(b[loc[1]+n:], []byte("endstream")) {
			t.Fatalf("stream at %d is not %d bytes long", loc[1], n)
		}
	}
}

// Las tildes, la eñe, la diéresis, los ordinales y el euro van en WinAnsi, un byte cada uno.
func TestEncode_WinAnsi(t *testing.T) {
	got := pdf.Encode("áéíóúñÑü€ºª")
	want := []byte{0xE1, 0xE9, 0xED, 0xF3, 0xFA, 0xF1, 0xD1, 0xFC, 0x80, 0xBA, 0xAA}
	if !bytes.Equal(got, want) {
		t.Fatalf("encode: % X", got)
	}
	if got := pdf.Encode("Łódź, Gdańsk, Ostrava-Poruba, Kraków, Iaşi, Győr, Košice, Ağrı"); string(got) != "L\xf3dz, Gdansk, Ostrava-Poruba, Krak\xf3w, Iasi, Gyor, Ko\x9aice, Agri" {
		t.Fatalf("transliteration: %q", got)
	}
	// La ó y la š sí están en WinAnsi y se quedan: Łódź sale como Lódz.
	if got := pdf.Encode("Łódź"); string(got) != "L\xf3dz" {
		t.Fatalf("Łódź: %q", got)
	}
	if got := pdf.Encode("a→b"); string(got) != "a?b" {
		t.Fatalf("unknown: %q", got)
	}
	b := sample()
	if !bytes.Contains(b, append([]byte("(Formaci\xF3n \\(\xF1, \xFC, \xBA, \xAA\\): 1.234,56 "), 0x80, ')')) {
		t.Fatal("the text is not in WinAnsi or not escaped")
	}
}

// El texto alineado a la derecha acaba en x: empieza en x menos su ancho.
func TestTextRight(t *testing.T) {
	w := pdf.Width(pdf.Regular, 10, "185,41 €") // 5 cifras (556), coma (278), espacio (278) y euro (556)
	if want := (5*556 + 278 + 278 + 556) * 10 / 1000.0; w != want {
		t.Fatalf("width: %v, want %v", w, want)
	}
	if !bytes.Contains(sample(), []byte("BT /F1 10 Tf 506.08 741.89 Td (185,41 \x80) Tj ET")) {
		t.Fatal("right aligned text")
	}
}

func TestWrap(t *testing.T) {
	lines := pdf.Wrap(pdf.Regular, 10, 100, "Hora de consultoría sobre la facturación electrónica")
	if len(lines) < 2 {
		t.Fatalf("wrap: %q", lines)
	}
	for _, l := range lines {
		if pdf.Width(pdf.Regular, 10, l) > 100 {
			t.Fatalf("too wide: %q", l)
		}
	}
	if got := pdf.Wrap(pdf.Regular, 10, 30, "Supercalifragilístico"); len(got) < 2 {
		t.Fatalf("long word: %q", got)
	}
}
