package domain

import (
	"strings"

	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Kinds of record of whom a company trades with and what it keeps its books and its money in: the
// neutral shape the accounting and banking sources (Sage, Apiscore) map to.
const (
	KindCustomer        = "customer"
	KindSupplier        = "supplier"
	KindTaxRate         = "tax-rate"
	KindLedgerAccount   = "ledger-account"
	KindOwnAccount      = "own-account"      // a bank account of the company itself
	KindCustomerAccount = "customer-account" // an account a financial institution keeps for a customer
)

// Kinds of party a customer or a supplier is.
const (
	PartyPerson       = "person"
	PartyOrganization = "organization"
)

// Document types, by the code Parties gives them.
const (
	DocNational = "NIDN"
	DocTax      = "TXID"
	DocOther    = "OTHR"
)

// countries are the ways the sources write a country or a nationality, folded (the C#
// CountryAliasPolicy and its countries.csv).
var countries = map[string]string{
	"espana": "ES", "espanola": "ES", "espanol": "ES", "espnola": "ES", "espaonola": "ES", "espanaola": "ES", "espanolo": "ES", "eapanola": "ES",
	"esponola": "ES", "esp": "ES", "es": "ES", "spain": "ES",
	"eeuu de america": "US", "estados unidos": "US", "estad unidos america": "US", "est.uni. america": "US", "united states of ame": "US",
	"estadounidense": "US", "americana": "US",
	"mexico": "MX", "mexicana": "MX", "venezuela": "VE", "venezolana": "VE", "uruguay": "UY", "panama": "PA", "panamena": "PA",
	"reino unido": "GB", "inglesa": "GB", "polaca": "PL", "sueca": "SE", "guatemala": "GT", "honduras": "HN", "peru": "PE", "peruana": "PE",
	"peruna": "PE", "costa rica": "CR", "argentina": "AR", "paraguay": "PY", "armenia": "AM", "portugal": "PT", "portugues": "PT",
	"portuguesa": "PT", "japon": "JP", "el salvador": "SV", "andorra": "AD", "kenya": "KE", "alemana": "DE", "aleman": "DE", "deutsch": "DE",
	"rep. dominicana": "DO", "republica dominicana": "DO", "italia": "IT", "italiano": "IT", "italiana": "IT", "cuba": "CU", "luxemburgo": "LU",
	"rumania": "RO", "rumana": "RO", "india": "IN", "brasil": "BR", "nigeria": "NG", "rusia": "RU", "moldava": "MD", "bulgaria": "BG",
	"camerun": "CM", "camerunesa": "CM", "frances": "FR", "colombia": "CO", "jordania": "JO", "marruecos": "MA", "bolivia": "BO",
	"curazao": "CW", "suiza": "CH",
}

// foldN folds as Fold does, and also the ñ: the sources write ESPAÑOLA and ESPANOLA alike.
func foldN(s string) string { return strings.ReplaceAll(Fold(s), "ñ", "n") }

// Country returns the ISO code of a country or a nationality as the sources write it.
func Country(written string) (string, bool) {
	iso, ok := countries[foldN(written)]
	return iso, ok
}

// Document tells what a document number is, from the number itself, the type the source gives it
// and the nationality of its bearer: the type of document and the country that issued it, as
// Parties keeps them. A Spanish number with its check digits right is Spanish whatever the source
// calls it (the C# rule). What Parties would refuse as its own type for lack of data the files do
// not bring (the day a passport or a foreigner's card expires) is kept as "other", with its
// country: the number is what the customer is looked up by. ok is false when nothing can be kept.
func Document(number, written, nationality string) (typ, country string, ok bool) {
	n := vocab.NormalizeDocumentNumber(number)
	if n == "" {
		return "", "", false
	}
	if valid, _ := vocab.ValidCheckDigit("ES_DNI_MOD23", n); valid {
		return DocNational, "ES", true
	}
	if valid, _ := vocab.ValidCheckDigit("ES_CIF", n); valid {
		return DocTax, "ES", true
	}
	if valid, _ := vocab.ValidCheckDigit("ES_NIE_MOD23", n); valid {
		return DocOther, "ES", true
	}
	switch foldN(written) {
	case "dni", "cif", "nif", "tarjeta residente (nie)":
		return DocOther, "ES", true // Spanish by its type, with check digits Parties would refuse
	case "documento fiscal extranjero":
		if iso, found := Country(nationality); found {
			return DocTax, iso, true
		}
	case "pasaporte", "acreditacion":
		if iso, found := Country(nationality); found {
			return DocOther, iso, true
		}
	}
	return "", "", false
}

// CleanName keeps of a name its letters, its digits and - _ . ' ( ), and one space between words:
// the banking sources bring names with tabs, quotes and control characters.
func CleanName(s string) string {
	var b strings.Builder
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c > 0x7F && c != 0xFEFF, strings.ContainsRune("-_.'()", c):
			b.WriteRune(c)
		default:
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// AccountNumber writes an account number as it is compared: without spaces, in capitals.
func AccountNumber(s string) string {
	return strings.ToUpper(strings.Join(strings.Fields(s), ""))
}

// run is what the sources of this file share: the records and the messages they gather.
type run struct {
	records  []Record
	messages []Message
}

func (p *run) add(t *Table, r Row, kind, scope, key string, fields map[string]string) {
	p.records = append(p.records, Record{Kind: kind, Scope: scope, Key: key, File: t.File, Line: r.Line, Fields: fields})
}

func (p *run) reject(t *Table, r Row, kind, key, code, text string) {
	p.messages = append(p.messages, Message{Severity: SeverityError, Code: code, Text: text, File: t.File, Line: r.Line, Kind: kind, Key: key})
}

func (p *run) warn(t *Table, r Row, kind, key, code, text string) {
	p.messages = append(p.messages, Message{Severity: SeverityWarning, Code: code, Text: text, File: t.File, Line: r.Line, Kind: kind, Key: key})
}
