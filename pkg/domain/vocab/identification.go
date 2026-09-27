package vocab

import (
	"strconv"
	"strings"
)

// DocumentKind is the kind of an identification document.
type DocumentKind string

// Document kinds. TaxID accepts any Spanish NIF form (DNI, NIE, CIF, K/L/M NIF).
const (
	NationalID     DocumentKind = "national_id"     // DNI
	ResidentCard   DocumentKind = "resident_card"   // NIE
	TaxID          DocumentKind = "tax_id"          // NIF / CIF
	Passport       DocumentKind = "passport"        // pasaporte
	SocialSecurity DocumentKind = "social_security" // NSS
	OtherDocument  DocumentKind = "other"
)

// Identification is an identification document number of a given kind, issued by a country.
// Values are normalized (upper case, no spaces or dashes) before validating. Country rules are
// pluggable (RegisterDocumentRule); Spanish rules with check digits are built in.
type Identification struct {
	Kind    DocumentKind `json:"kind"`
	Country CountryCode  `json:"country"`
	Number  string       `json:"number"`
}

// DocumentRule validates a normalized document number and returns an error message ("" = valid).
type DocumentRule func(number string) string

var documentRules = map[string]DocumentRule{}

func ruleKey(c CountryCode, k DocumentKind) string { return c.String() + "/" + string(k) }

// RegisterDocumentRule sets the validation rule of a document kind for a country. Call it
// during initialization only (it is not safe for concurrent use with NewIdentification).
func RegisterDocumentRule(country CountryCode, kind DocumentKind, rule DocumentRule) {
	documentRules[ruleKey(country, kind)] = rule
}

func init() {
	RegisterDocumentRule(Spain, NationalID, esDNI)
	RegisterDocumentRule(Spain, ResidentCard, esNIE)
	RegisterDocumentRule(Spain, TaxID, esNIF)
	RegisterDocumentRule(Spain, SocialSecurity, esNSS)
}

// NewIdentification normalizes and validates a document number. Kinds without a country rule
// are checked with a generic rule (3..20 letters or digits); passports require 5..20.
func NewIdentification(kind DocumentKind, country CountryCode, number string) (Identification, error) {
	n := strings.ToUpper(strings.NewReplacer(" ", "", "-", "", ".", "").Replace(strings.TrimSpace(number)))
	if country.IsZero() {
		return Identification{}, invalid("country", "required", "country is required")
	}
	rule, ok := documentRules[ruleKey(country, kind)]
	if !ok {
		rule = genericDocument(kind)
	}
	if msg := rule(n); msg != "" {
		return Identification{}, invalid("number", string(kind), msg)
	}
	return Identification{Kind: kind, Country: country, Number: n}, nil
}

// IsZero reports whether the identification is absent.
func (i Identification) IsZero() bool { return i.Number == "" }

// String returns "ES/tax_id:B12345674".
func (i Identification) String() string {
	return i.Country.String() + "/" + string(i.Kind) + ":" + i.Number
}

func alnum(s string) bool {
	for _, r := range s {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return s != ""
}

func digits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

func genericDocument(kind DocumentKind) DocumentRule {
	lo := 3
	if kind == Passport {
		lo = 5
	}
	return func(n string) string {
		if len(n) < lo || len(n) > 20 || !alnum(n) {
			return "document number must have " + strconv.Itoa(lo) + " to 20 letters or digits"
		}
		return ""
	}
}

const dniLetters = "TRWAGMYFPDXBNJZSQVHLCKE"

func dniLetter(n int) byte { return dniLetters[n%23] }

func esDNI(n string) string {
	if len(n) != 9 || !digits(n[:8]) {
		return "DNI must be 8 digits and a letter"
	}
	num, _ := strconv.Atoi(n[:8])
	if n[8] != dniLetter(num) {
		return "DNI check letter does not match"
	}
	return ""
}

func esNIE(n string) string {
	if len(n) != 9 || strings.IndexByte("XYZ", n[0]) < 0 || !digits(n[1:8]) {
		return "NIE must be X/Y/Z, 7 digits and a letter"
	}
	num, _ := strconv.Atoi(strconv.Itoa(strings.IndexByte("XYZ", n[0])) + n[1:8])
	if n[8] != dniLetter(num) {
		return "NIE check letter does not match"
	}
	return ""
}

// esCIF validates the NIF of legal entities: letter + 7 digits + check (digit or letter from
// "JABCDEFGHI"). P, Q, R, S, N, W require a letter; A, B, E, H require a digit.
func esCIF(n string) string {
	if len(n) != 9 || strings.IndexByte("ABCDEFGHJNPQRSUVW", n[0]) < 0 || !digits(n[1:8]) {
		return "CIF must be an entity letter, 7 digits and a check character"
	}
	sum := 0
	for i := 0; i < 7; i++ {
		d := int(n[1+i] - '0')
		if i%2 == 0 { // odd positions (1st, 3rd...) are doubled
			d *= 2
			d = d/10 + d%10
		}
		sum += d
	}
	control := (10 - sum%10) % 10
	letter, digit := "JABCDEFGHI"[control], byte('0'+control)
	switch {
	case strings.IndexByte("PQRSNW", n[0]) >= 0:
		if n[8] != letter {
			return "CIF check character must be the letter " + string(letter)
		}
	case strings.IndexByte("ABEH", n[0]) >= 0:
		if n[8] != digit {
			return "CIF check character must be the digit " + string(digit)
		}
	default:
		if n[8] != letter && n[8] != digit {
			return "CIF check character does not match"
		}
	}
	return ""
}

// esNIF accepts any Spanish tax id: DNI, NIE, CIF or K/L/M NIF (K/L/M + 7 digits + DNI letter).
func esNIF(n string) string {
	if len(n) != 9 {
		return "NIF must have 9 characters"
	}
	switch c := n[0]; {
	case c >= '0' && c <= '9':
		return esDNI(n)
	case c == 'X' || c == 'Y' || c == 'Z':
		return esNIE(n)
	case c == 'K' || c == 'L' || c == 'M':
		if !digits(n[1:8]) {
			return "NIF K/L/M must be followed by 7 digits and a letter"
		}
		num, _ := strconv.Atoi(n[1:8])
		if n[8] != dniLetter(num) {
			return "NIF check letter does not match"
		}
		return ""
	default:
		return esCIF(n)
	}
}

// esNSS validates the Spanish social security number: province (2) + number (8) + control (2),
// control = value mod 97, where value = province*10^7 + number when number < 10^7, else the
// concatenation province‖number.
func esNSS(n string) string {
	if len(n) != 12 || !digits(n) {
		return "NSS must have 12 digits"
	}
	province, _ := strconv.ParseInt(n[:2], 10, 64)
	number, _ := strconv.ParseInt(n[2:10], 10, 64)
	control, _ := strconv.ParseInt(n[10:], 10, 64)
	value := province*100_000_000 + number
	if number < 10_000_000 {
		value = province*10_000_000 + number
	}
	if value%97 != control {
		return "NSS control digits do not match"
	}
	return ""
}
