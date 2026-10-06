package domain

import (
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Norma43Account is the statement of one account inside a Cuaderno 43 file (the AEB/CECA format
// Spanish banks give statements in): the account as the bank identifies it (bank, office and
// number, without the IBAN check digits), the period, the balances and the movements.
type Norma43Account struct {
	Bank    string
	Office  string
	Number  string
	From    vocab.Date
	To      vocab.Date
	Opening vocab.Decimal
	Closing vocab.Decimal
	Lines   []StatementLine
}

// Matches reports whether a Spanish IBAN is this account.
func (a Norma43Account) Matches(iban vocab.IBAN) bool {
	s := iban.String()
	return len(s) == 24 && strings.HasPrefix(s, "ES") && s[4:8] == a.Bank && s[8:12] == a.Office && s[14:24] == a.Number
}

func n43Error(line int, what string) error {
	return fw.Violation("treasury.norma43", "line "+strconv.Itoa(line)+": "+what)
}

// n43Record gives the fields of a record by their 1-based positions in the 80-column layout.
type n43Record []rune

func (r n43Record) at(from, to int) string {
	if from > len(r) {
		return ""
	}
	return string(r[from-1 : min(to, len(r))])
}

func (r n43Record) text(from, to int) string { return strings.TrimSpace(r.at(from, to)) }

func (r n43Record) digits(from, to int) (string, bool) {
	s := r.at(from, to)
	if len(s) != to-from+1 {
		return s, false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return s, false
		}
	}
	return s, true
}

// amount reads an amount of 14 digits with two decimals and its sign key (1 debit, 2 credit).
func (r n43Record) amount(key, from int) (vocab.Decimal, bool) {
	d, ok := r.digits(from, from+13)
	k := r.at(key, key)
	if !ok || (k != "1" && k != "2") {
		return vocab.Decimal{}, false
	}
	v, err := vocab.ParseDecimal(d[:12] + "." + d[12:])
	if err != nil {
		return vocab.Decimal{}, false
	}
	if k == "1" {
		v = v.Neg()
	}
	return v, true
}

func (r n43Record) date(from int) (vocab.Date, bool) {
	d, ok := r.digits(from, from+5)
	if !ok {
		return vocab.Date{}, false
	}
	yy, _ := strconv.Atoi(d[:2])
	mm, _ := strconv.Atoi(d[2:4])
	dd, _ := strconv.Atoi(d[4:])
	day, err := vocab.NewDate(2000+yy, time.Month(mm), dd)
	return day, err == nil
}

// ParseNorma43 reads a Cuaderno 43 file: for each account its header (record 11), its movements
// (22, with their complementary concepts in 23) and its totals (33), which are checked against
// the movements. The text may come in UTF-8 or in the ISO 8859-1 banks still use.
func ParseNorma43(content string) ([]Norma43Account, error) {
	var out []Norma43Account
	var cur *Norma43Account
	debits, credits := 0, 0
	debit, credit := vocab.DecimalFromInt(0), vocab.DecimalFromInt(0)
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	for i, raw := range lines {
		n := i + 1
		var rec n43Record
		if utf8.ValidString(raw) {
			rec = n43Record(raw)
		} else {
			for j := 0; j < len(raw); j++ { // ISO 8859-1: a byte is a code point
				rec = append(rec, rune(raw[j]))
			}
		}
		if strings.TrimSpace(string(rec)) == "" {
			continue
		}
		switch rec.at(1, 2) {
		case "11":
			if cur != nil {
				return nil, n43Error(n, "an account starts before the previous one ends")
			}
			bank, ok1 := rec.digits(3, 6)
			office, ok2 := rec.digits(7, 10)
			number, ok3 := rec.digits(11, 20)
			from, ok4 := rec.date(21)
			to, ok5 := rec.date(27)
			opening, ok6 := rec.amount(33, 34)
			if !(ok1 && ok2 && ok3 && ok4 && ok5 && ok6) {
				return nil, n43Error(n, "malformed account header")
			}
			if currency := rec.at(48, 50); currency != "978" {
				return nil, n43Error(n, "only accounts in euros (978) are supported, not "+currency)
			}
			cur = &Norma43Account{Bank: bank, Office: office, Number: number, From: from, To: to, Opening: opening}
			debits, credits, debit, credit = 0, 0, vocab.DecimalFromInt(0), vocab.DecimalFromInt(0)
		case "22":
			if cur == nil {
				return nil, n43Error(n, "a movement outside an account")
			}
			date, ok1 := rec.date(11)
			value, ok2 := rec.date(17)
			amount, ok3 := rec.amount(28, 29)
			if !(ok1 && ok2 && ok3) || amount.IsZero() {
				return nil, n43Error(n, "malformed movement")
			}
			if amount.IsNegative() {
				debits, debit = debits+1, debit.Add(amount.Neg())
			} else {
				credits, credit = credits+1, credit.Add(amount)
			}
			ref := strings.TrimLeft(rec.text(43, 52), "0")
			if ref == "" {
				ref = strings.TrimLeft(rec.text(53, 64), "0")
			}
			cur.Lines = append(cur.Lines, StatementLine{No: len(cur.Lines) + 1, Date: date, ValueDate: value, Amount: amount, Reference: ref,
				Concept: strings.TrimSpace(rec.text(53, 64) + " " + rec.text(65, 80))})
		case "23":
			if cur == nil || len(cur.Lines) == 0 {
				return nil, n43Error(n, "a concept without a movement")
			}
			l := &cur.Lines[len(cur.Lines)-1]
			if rec.at(3, 4) == "01" {
				l.Concept = "" // the complementary concepts replace the references of the movement
			}
			l.Concept = strings.TrimSpace(l.Concept + " " + rec.text(5, 42) + " " + rec.text(43, 80))
		case "33":
			if cur == nil {
				return nil, n43Error(n, "totals outside an account")
			}
			nd, ok1 := rec.digits(21, 25)
			td, ok2 := rec.digits(26, 39)
			nc, ok3 := rec.digits(40, 44)
			tc, ok4 := rec.digits(45, 58)
			closing, ok5 := rec.amount(59, 60)
			if !(ok1 && ok2 && ok3 && ok4 && ok5) || rec.at(3, 20) != cur.Bank+cur.Office+cur.Number {
				return nil, n43Error(n, "malformed account totals")
			}
			wantD, _ := vocab.ParseDecimal(td[:12] + "." + td[12:])
			wantC, _ := vocab.ParseDecimal(tc[:12] + "." + tc[12:])
			if a, _ := strconv.Atoi(nd); a != debits || !wantD.Equal(debit) {
				return nil, n43Error(n, "the debits do not add up to the totals of the account")
			}
			if a, _ := strconv.Atoi(nc); a != credits || !wantC.Equal(credit) {
				return nil, n43Error(n, "the credits do not add up to the totals of the account")
			}
			if !cur.Opening.Sub(debit).Add(credit).Equal(closing) {
				return nil, n43Error(n, "the closing balance is not the opening balance plus the movements")
			}
			cur.Closing = closing
			out, cur = append(out, *cur), nil
		case "88":
			if cur != nil {
				return nil, n43Error(n, "the file ends inside an account")
			}
		case "24": // equivalence of the amount in another currency: not kept
		default:
			return nil, n43Error(n, "unknown record "+rec.at(1, 2))
		}
	}
	if cur != nil {
		return nil, n43Error(len(lines), "the last account has no totals")
	}
	if len(out) == 0 {
		return nil, fw.Violation("treasury.norma43", "the file has no account")
	}
	return out, nil
}
