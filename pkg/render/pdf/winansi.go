package pdf

import "strings"

// winAnsiHigh son los caracteres de 0x80 a 0x9F en WinAnsiEncoding, que no coinciden con Latin-1.
// De 0xA0 a 0xFF el código es el del propio carácter (á es 0xE1, ñ es 0xF1, º es 0xBA).
var winAnsiHigh = map[rune]byte{
	'€': 0x80, '‚': 0x82, 'ƒ': 0x83, '„': 0x84, '…': 0x85, '†': 0x86, '‡': 0x87, 'ˆ': 0x88, '‰': 0x89,
	'Š': 0x8A, '‹': 0x8B, 'Œ': 0x8C, 'Ž': 0x8E, '‘': 0x91, '’': 0x92, '“': 0x93, '”': 0x94, '•': 0x95,
	'–': 0x96, '—': 0x97, '˜': 0x98, '™': 0x99, 'š': 0x9A, '›': 0x9B, 'œ': 0x9C, 'ž': 0x9E, 'Ÿ': 0x9F,
}

// latinBase da la letra base de las letras latinas con diacrítico que WinAnsi no tiene (polaco,
// checo, húngaro, rumano, turco, croata, letón, lituano…): Łódź sale como Lodz, mejor que como ?ód?.
var latinBase = func() map[rune]rune {
	const from = "ĀāĂăĄąĆćĈĉĊċČčĎďĐđĒēĔĕĖėĘęĚěĜĝĞğĠġĢģĤĥĦħĨĩĪīĬĭĮįİıĴĵĶķĹĺĻļĽľĿŀŁłŃńŅņŇňŌōŎŏŐőŔŕŖŗŘřŚśŜŝŞşŢţŤťŦŧŨũŪūŬŭŮůŰűŲųŴŵŶŷŹźŻżȘșȚț"
	const to = "AaAaAaCcCcCcCcDdDdEeEeEeEeEeGgGgGgGgHhHhIiIiIiIiIiJjKkLlLlLlLlLlNnNnNnOoOoOoRrRrRrSsSsSsTtTtTtUuUuUuUuUuUuWwYyZzZzSsTt"
	m := map[rune]rune{}
	t := []rune(to)
	for k, r := range []rune(from) {
		m[r] = t[k]
	}
	return m
}()

// Encode pasa s a WinAnsiEncoding. Una letra latina que no tiene código sale como su letra base;
// lo demás que no tiene código, como '?'; los caracteres de
// control, como espacios.
func Encode(s string) []byte {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		switch {
		case r < 0x20:
			out = append(out, ' ')
		case r < 0x7F || (r >= 0xA0 && r <= 0xFF):
			out = append(out, byte(r))
		default:
			if b, ok := winAnsiHigh[r]; ok {
				out = append(out, b)
			} else if base, ok := latinBase[r]; ok {
				out = append(out, byte(base))
			} else {
				out = append(out, '?')
			}
		}
	}
	return out
}

// Width devuelve el ancho de s, en puntos, escrito con la fuente y el tamaño dados.
func Width(f Font, size float64, s string) float64 {
	table := &helvetica
	if f == Bold {
		table = &helveticaBold
	}
	units := 0
	for _, c := range Encode(s) {
		if c >= 32 {
			units += int(table[c-32])
		}
	}
	return float64(units) * size / 1000
}

// Wrap parte s en líneas que caben en width, por los espacios. Una palabra más ancha que la línea
// se corta donde haga falta.
func Wrap(f Font, size, width float64, s string) []string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(s) {
		for Width(f, size, word) > width && len([]rune(word)) > 1 {
			if line != "" {
				lines, line = append(lines, line), ""
			}
			r := []rune(word)
			n := len(r) - 1
			for n > 1 && Width(f, size, string(r[:n])) > width {
				n--
			}
			lines, word = append(lines, string(r[:n])), string(r[n:])
		}
		switch {
		case line == "":
			line = word
		case Width(f, size, line+" "+word) <= width:
			line += " " + word
		default:
			lines, line = append(lines, line), word
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

// Anchos de los caracteres 32 a 255 en WinAnsiEncoding, en milésimas del tamaño de la fuente
// (las métricas AFM de Adobe de las dos fuentes base 14).
var helvetica = [224]uint16{
	278, 278, 355, 556, 556, 889, 667, 191, 333, 333, 389, 584, 278, 333, 278, 278, // 32
	556, 556, 556, 556, 556, 556, 556, 556, 556, 556, 278, 278, 584, 584, 584, 556, // 48
	1015, 667, 667, 722, 722, 667, 611, 778, 722, 278, 500, 667, 556, 833, 722, 778, // 64
	667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611, 278, 278, 278, 469, 556, // 80
	333, 556, 556, 500, 556, 556, 278, 556, 556, 222, 222, 500, 222, 833, 556, 556, // 96
	556, 556, 333, 500, 278, 556, 500, 722, 500, 500, 500, 334, 260, 334, 584, 350, // 112
	556, 350, 222, 556, 333, 1000, 556, 556, 333, 1000, 667, 333, 1000, 350, 611, 350, // 128
	350, 222, 222, 333, 333, 350, 556, 1000, 333, 1000, 500, 333, 944, 350, 500, 667, // 144
	278, 333, 556, 556, 556, 556, 260, 556, 333, 737, 370, 556, 584, 333, 737, 333, // 160
	400, 584, 333, 333, 333, 556, 537, 278, 333, 333, 365, 556, 834, 834, 834, 611, // 176
	667, 667, 667, 667, 667, 667, 1000, 722, 667, 667, 667, 667, 278, 278, 278, 278, // 192
	722, 722, 778, 778, 778, 778, 778, 584, 778, 722, 722, 722, 722, 667, 667, 611, // 208
	556, 556, 556, 556, 556, 556, 889, 500, 556, 556, 556, 556, 278, 278, 278, 278, // 224
	556, 556, 556, 556, 556, 556, 556, 584, 611, 556, 556, 556, 556, 500, 556, 500, // 240
}

var helveticaBold = [224]uint16{
	278, 333, 474, 556, 556, 889, 722, 238, 333, 333, 389, 584, 278, 333, 278, 278, // 32
	556, 556, 556, 556, 556, 556, 556, 556, 556, 556, 333, 333, 584, 584, 584, 611, // 48
	975, 722, 722, 722, 722, 667, 611, 778, 722, 278, 556, 722, 611, 833, 722, 778, // 64
	667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611, 333, 278, 333, 584, 556, // 80
	333, 556, 611, 556, 611, 556, 333, 611, 611, 278, 278, 556, 278, 889, 611, 611, // 96
	611, 611, 389, 556, 333, 611, 556, 778, 556, 556, 500, 389, 280, 389, 584, 350, // 112
	556, 350, 278, 556, 500, 1000, 556, 556, 333, 1000, 667, 333, 1000, 350, 611, 350, // 128
	350, 278, 278, 500, 500, 350, 556, 1000, 333, 1000, 556, 333, 944, 350, 500, 667, // 144
	278, 333, 556, 556, 556, 556, 280, 556, 333, 737, 370, 556, 584, 333, 737, 333, // 160
	400, 584, 333, 333, 333, 611, 556, 278, 333, 333, 365, 556, 834, 834, 834, 611, // 176
	722, 722, 722, 722, 722, 722, 1000, 722, 667, 667, 667, 667, 278, 278, 278, 278, // 192
	722, 722, 778, 778, 778, 778, 778, 584, 778, 722, 722, 722, 722, 667, 667, 611, // 208
	556, 556, 556, 556, 556, 556, 889, 556, 556, 556, 556, 556, 278, 278, 278, 278, // 224
	611, 611, 611, 611, 611, 611, 611, 584, 611, 611, 611, 611, 611, 556, 611, 556, // 240
}
