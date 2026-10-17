package vocab

import (
	"strconv"
	"strings"
)

// Algoritmos de dígito de control de los números de identificación nacionales, con las mismas
// claves que la columna country_document_rule.ChecksumKey de C#, para que las reglas por país de
// Karpo sigan funcionando. Cada algoritmo recibe un número normalizado (en mayúsculas, sin espacios,
// guiones, puntos ni barras).
var checkDigits = map[string]func(string) bool{
	"ES_DNI_MOD23":   func(n string) bool { return esDNI(n) == "" },
	"ES_NIE_MOD23":   func(n string) bool { return esNIE(n) == "" },
	"ES_CIF":         func(n string) bool { return esCIF(n) == "" },
	"ES_NIF":         func(n string) bool { return esNIF(n) == "" },
	"ES_NSS_MOD97":   func(n string) bool { return esNSS(n) == "" },
	"PT_NIF_MOD11":   ptNIF,
	"IT_CF":          itCodiceFiscale,
	"FR_SPI_MOD511":  frSPI,
	"DE_IDNR":        deIdNr,
	"NL_BSN_MOD11":   nlBSN,
	"BE_NN_MOD97":    beNN,
	"EL_AFM_MOD11":   elAFM,
	"HR_OIB_ISO7064": func(n string) bool { return len(n) == 11 && digits(n) && iso7064Mod11_10(n) },
}

// CheckDigitKeys devuelve las claves de algoritmo conocidas.
func CheckDigitKeys() []string {
	out := make([]string, 0, len(checkDigits))
	for k := range checkDigits {
		out = append(out, k)
	}
	return out
}

// ValidCheckDigit valida number con el algoritmo key. known es false si la clave es desconocida
// (una regla que nombra un algoritmo que nadie ha implementado no debe pasar en silencio).
func ValidCheckDigit(key, number string) (valid, known bool) {
	fn, ok := checkDigits[key]
	if !ok {
		return false, false
	}
	return fn(NormalizeDocumentNumber(number)), true
}

// NormalizeDocumentNumber pasa a mayúsculas un número de documento y le quita espacios, guiones,
// puntos, barras y signos más.
func NormalizeDocumentNumber(s string) string {
	return strings.ToUpper(strings.NewReplacer(" ", "", "-", "", ".", "", "/", "", "+", "").Replace(strings.TrimSpace(s)))
}

func atoi64(s string) int64 { n, _ := strconv.ParseInt(s, 10, 64); return n }

// ptNIF: NIF portugués, 9 dígitos, pesos 9..2, módulo 11.
func ptNIF(n string) bool {
	if len(n) != 9 || !digits(n) || n[0] == '0' {
		return false
	}
	sum := 0
	for i := range 8 {
		sum += (9 - i) * int(n[i]-'0')
	}
	return int(n[8]-'0') == (11-sum%11)%11%10
}

// itCodiceFiscale: código fiscal italiano, 16 caracteres, tablas de impares/pares, módulo 26.
func itCodiceFiscale(n string) bool {
	if len(n) != 16 || !alnum(n) {
		return false
	}
	odd := [...]int{1, 0, 5, 7, 9, 13, 15, 17, 19, 21, 2, 4, 18, 20, 11, 3, 6, 8, 12, 14, 16, 10, 22, 25, 24, 23}
	val := func(c byte, isOdd bool) int {
		i := int(c - 'A')
		if c >= '0' && c <= '9' {
			i = int(c - '0')
		}
		if isOdd {
			return odd[i]
		}
		return i
	}
	code := 0
	for i := range 15 {
		code += val(n[i], i%2 == 0)
	}
	return n[15] == byte('A'+code%26)
}

// frSPI: número fiscal francés (numéro fiscal), 13 dígitos, los 10 primeros módulo 511 = los 3 últimos.
func frSPI(n string) bool {
	return len(n) == 13 && digits(n) && n[0] <= '3' && atoi64(n[:10])%511 == atoi64(n[10:])
}

// deIdNr: identificador fiscal alemán, 11 dígitos: en los 10 primeros se repite exactamente un dígito
// (2 o 3 veces) y se cumple ISO 7064 MOD 11,10.
func deIdNr(n string) bool {
	if len(n) != 11 || !digits(n) || n[0] == '0' {
		return false
	}
	var counts [10]int
	for i := range 10 {
		counts[n[i]-'0']++
	}
	repeated, times := 0, 0
	for _, c := range counts {
		if c > 1 {
			repeated++
			times = c
		}
	}
	return repeated == 1 && (times == 2 || times == 3) && iso7064Mod11_10(n)
}

// nlBSN: número de ciudadano neerlandés, 9 dígitos, «prueba del 11» con el último peso -1.
func nlBSN(n string) bool {
	if len(n) != 9 || !digits(n) || atoi64(n) == 0 {
		return false
	}
	sum := 0
	for i := range 8 {
		sum += (9 - i) * int(n[i]-'0')
	}
	return (sum-int(n[8]-'0'))%11 == 0
}

// beNN: número nacional belga, 11 dígitos, 97 - (los 9 primeros módulo 97) = los 2 últimos, con el
// prefijo "2" para los nacidos desde 2000.
func beNN(n string) bool {
	if len(n) != 11 || !digits(n) || atoi64(n) == 0 {
		return false
	}
	check := atoi64(n[9:])
	return 97-atoi64(n[:9])%97 == check || 97-atoi64("2"+n[:9])%97 == check
}

// elAFM: número fiscal griego, 9 dígitos, suma de d_i * 2^(8-i) módulo 11 módulo 10.
func elAFM(n string) bool {
	if len(n) != 9 || !digits(n) {
		return false
	}
	sum := 0
	for i := range 8 {
		sum = sum*2 + int(n[i]-'0')
	}
	return int(n[8]-'0') == sum*2%11%10
}

// iso7064Mod11_10 es ISO 7064 MOD 11,10 sobre todos los dígitos (OIB croata, IdNr alemán).
func iso7064Mod11_10(n string) bool {
	check := 5
	for i := range len(n) {
		c := check
		if c == 0 {
			c = 10
		}
		check = (c*2%11 + int(n[i]-'0')) % 10
	}
	return check == 1
}
