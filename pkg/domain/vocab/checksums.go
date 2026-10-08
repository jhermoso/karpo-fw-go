package vocab

import (
	"strconv"
	"strings"
)

// Check-digit algorithms of national identification numbers, keyed as in the C#
// country_document_rule.ChecksumKey column, so the country rules of Karpo keep working.
// Each algorithm receives a normalized number (upper case, no spaces, dashes, dots or slashes).
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

// CheckDigitKeys returns the known algorithm keys.
func CheckDigitKeys() []string {
	out := make([]string, 0, len(checkDigits))
	for k := range checkDigits {
		out = append(out, k)
	}
	return out
}

// ValidCheckDigit validates number with the algorithm key. known is false for an unknown key
// (a rule that names an algorithm nobody implemented must not pass silently).
func ValidCheckDigit(key, number string) (valid, known bool) {
	fn, ok := checkDigits[key]
	if !ok {
		return false, false
	}
	return fn(NormalizeDocumentNumber(number)), true
}

// NormalizeDocumentNumber upper-cases a document number and removes spaces, dashes, dots,
// slashes and plus signs.
func NormalizeDocumentNumber(s string) string {
	return strings.ToUpper(strings.NewReplacer(" ", "", "-", "", ".", "", "/", "", "+", "").Replace(strings.TrimSpace(s)))
}

func atoi64(s string) int64 { n, _ := strconv.ParseInt(s, 10, 64); return n }

// ptNIF: Portuguese NIF, 9 digits, weights 9..2, mod 11.
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

// itCodiceFiscale: Italian fiscal code, 16 characters, odd/even tables, mod 26.
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

// frSPI: French tax number (numéro fiscal), 13 digits, first 10 mod 511 = last 3.
func frSPI(n string) bool {
	return len(n) == 13 && digits(n) && n[0] <= '3' && atoi64(n[:10])%511 == atoi64(n[10:])
}

// deIdNr: German tax id, 11 digits: in the first 10, exactly one digit repeats (2 or 3 times),
// and ISO 7064 MOD 11,10 holds.
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

// nlBSN: Dutch citizen number, 9 digits, "11 test" with the last weight -1.
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

// beNN: Belgian national number, 11 digits, 97 - (first 9 mod 97) = last 2, with the "2" prefix
// for people born from 2000.
func beNN(n string) bool {
	if len(n) != 11 || !digits(n) || atoi64(n) == 0 {
		return false
	}
	check := atoi64(n[9:])
	return 97-atoi64(n[:9])%97 == check || 97-atoi64("2"+n[:9])%97 == check
}

// elAFM: Greek tax number, 9 digits, sum of d_i * 2^(8-i) mod 11 mod 10.
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

// iso7064Mod11_10 is ISO 7064 MOD 11,10 over all the digits (Croatian OIB, German IdNr).
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
