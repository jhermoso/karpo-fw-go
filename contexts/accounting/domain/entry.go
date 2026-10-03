package domain

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// EntryKind is the stable aggregate type name.
const EntryKind = "accounting.entry"

// MaxLines bounds the lines of an entry.
const MaxLines = 1000

// Line is a line of an entry: an account debited or credited, optionally for a party (the
// subledger of customers, employees…).
type Line struct {
	Account     string
	Party       PartyID
	Debit       vocab.Decimal
	Credit      vocab.Decimal
	Description string
}

// Source is the fact an entry comes from: the type of the event and its id (idempotency), and a
// key to find it again (to reverse it).
type Source struct {
	Type string
	ID   string
	Key  string
}

// EntryState is the persisted state of an entry.
type EntryState struct {
	Company     OrganizationID
	Number      int64
	Year        int
	Month       int
	Date        vocab.Date
	Description string
	Source      Source
	Lines       []Line
	Reverses    EntryID // this entry reverses another
	ReversedBy  EntryID // this entry was reversed
	Audit       traits.AuditStamp
}

// Entry is a journal entry with its lines (the C# kept header and lines as separate aggregates,
// so nothing could enforce that debits equal credits).
type Entry struct {
	fw.BaseAggregateRoot[EntryID]
	traits.Audited
	s EntryState
}

// Balanced lines: each line debits or credits a positive amount in cents, the entry has at least
// two lines, and debits equal credits.
func checkLines(v *fw.Validation, lines []Line) {
	v.Require(len(lines) >= 2 && len(lines) <= MaxLines, "lines", "count", "an entry has from 2 lines")
	debit, credit := vocab.DecimalFromInt(0), vocab.DecimalFromInt(0)
	for i, l := range lines {
		one := (l.Debit.IsPositive() && l.Credit.IsZero()) || (l.Credit.IsPositive() && l.Debit.IsZero())
		v.Require(one && l.Debit.Equal(l.Debit.Round(2)) && l.Credit.Equal(l.Credit.Round(2)), fmt.Sprintf("lines[%d]", i), "amount",
			"a line debits or credits a positive amount in cents")
		v.Require(ValidCode(l.Account), fmt.Sprintf("lines[%d].account", i), "format", "an account code")
		v.Require(utf8.RuneCountInString(l.Description) <= 200, fmt.Sprintf("lines[%d].description", i), "length", "at most 200 characters")
		debit, credit = debit.Add(l.Debit), credit.Add(l.Credit)
	}
	v.Require(debit.Equal(credit), "lines", "balance", "debits ("+debit.StringFixed(2)+") must equal credits ("+credit.StringFixed(2)+")")
}

// ReconstituteEntry rebuilds an entry.
func ReconstituteEntry(id EntryID, s EntryState) (*Entry, error) {
	base, err := fw.NewBaseAggregateRoot(EntryKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(!s.Company.IsZero(), "company", "required", "an entry belongs to a company")
	v.Require(!s.Date.IsZero() && s.Year >= 1990 && s.Month >= 1 && s.Month <= 12, "date", "required", "a date in a fiscal period")
	s.Description = strings.TrimSpace(s.Description)
	v.Require(s.Description != "" && utf8.RuneCountInString(s.Description) <= 300, "description", "length", "a description of 1 to 300 characters")
	v.Require(len(s.Source.Type) <= 80 && len(s.Source.ID) <= 80 && len(s.Source.Key) <= 120, "source", "length", "a source within limits")
	checkLines(&v, s.Lines)
	if err := v.Err(); err != nil {
		return nil, err
	}
	s.Lines = slices.Clone(s.Lines)
	return &Entry{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// Draft is an entry to post: its lines may carry signed amounts (a negative debit is a credit),
// as automatic entries of credits and corrective invoices produce them.
type Draft struct {
	Company     OrganizationID
	Date        vocab.Date
	Description string
	Source      Source
	Lines       []Line
}

// Normalize turns negative amounts into the opposite side and drops zero lines.
func Normalize(lines []Line) []Line {
	out := make([]Line, 0, len(lines))
	zero := vocab.DecimalFromInt(0)
	for _, l := range lines {
		d, c := l.Debit.Sub(l.Credit), zero
		if d.IsNegative() {
			d, c = zero, d.Neg()
		}
		if d.IsZero() && c.IsZero() {
			continue
		}
		l.Debit, l.Credit = d, c
		out = append(out, l)
	}
	return out
}

// Post creates a posted entry with its number in a period of a ledger.
func Post(id EntryID, d Draft, l *Ledger, number int64) (*Entry, error) {
	p := l.PeriodOf(d.Date)
	if l.IsClosed(p) {
		return nil, fw.Violation("accounting.period_closed", fmt.Sprintf("the period %d/%d is closed", p.Month, p.Year))
	}
	if d.Source.ID == "" {
		d.Source.ID = id.String() // manual entries are their own source (unique per company)
	}
	e, err := ReconstituteEntry(id, EntryState{Company: d.Company, Number: number, Year: p.Year, Month: p.Month, Date: d.Date,
		Description: d.Description, Source: d.Source, Lines: Normalize(d.Lines)})
	if err != nil {
		return nil, err
	}
	e.Raise(EntryPosted{EventMeta: e.NewEventMeta(), Company: d.Company.String(), Number: number, Year: p.Year, Source: d.Source.Type})
	return e, nil
}

// State returns the state (lines are a copy).
func (e *Entry) State() EntryState {
	s := e.s
	s.Lines = slices.Clone(s.Lines)
	return s
}

// Total returns the debits (equal to the credits).
func (e *Entry) Total() vocab.Decimal {
	t := vocab.DecimalFromInt(0)
	for _, l := range e.s.Lines {
		t = t.Add(l.Debit)
	}
	return t
}

// Reversal returns the draft that reverses the entry (debits and credits swapped) on a date.
func (e *Entry) Reversal(on vocab.Date, src Source) (Draft, error) {
	if !e.s.ReversedBy.IsZero() {
		return Draft{}, fw.Violation("accounting.already_reversed", "the entry is already reversed")
	}
	if !e.s.Reverses.IsZero() {
		return Draft{}, fw.Violation("accounting.reversal_of_reversal", "a reversal is not reversed: post the entry again")
	}
	lines := make([]Line, len(e.s.Lines))
	for i, l := range e.s.Lines {
		l.Debit, l.Credit = l.Credit, l.Debit
		lines[i] = l
	}
	desc := "Anulación del asiento " + fmt.Sprint(e.s.Number) + ": " + e.s.Description
	if utf8.RuneCountInString(desc) > 300 {
		desc = string([]rune(desc)[:300])
	}
	return Draft{Company: e.s.Company, Date: on, Description: desc, Source: src, Lines: lines}, nil
}

// MarkReversed links the entry with its reversal.
func (e *Entry) MarkReversed(by EntryID) error {
	if !e.s.ReversedBy.IsZero() {
		return fw.Violation("accounting.already_reversed", "the entry is already reversed")
	}
	e.s.ReversedBy = by
	return nil
}

// LinkReversal records the entry this one reverses.
func (e *Entry) LinkReversal(of EntryID) { e.s.Reverses = of }

// AuditSnapshot implements traits.Snapshotter.
func (e *Entry) AuditSnapshot() map[string]any {
	return map[string]any{"number": e.s.Number, "year": e.s.Year, "total": e.Total().String(), "reversedBy": e.s.ReversedBy.String()}
}

// Entry fields.
var (
	EntFieldCompany  = spec.Comparable("company", func(e *Entry) OrganizationID { return e.s.Company })
	EntFieldDate     = spec.OrderedBy("entry_date", func(e *Entry) vocab.Date { return e.s.Date }, vocab.CompareDates)
	EntFieldNumber   = spec.Ordered("entry_number", func(e *Entry) int64 { return e.s.Number })
	EntFieldYear     = spec.Comparable("fiscal_year", func(e *Entry) int { return e.s.Year })
	EntFieldSrcType  = spec.Comparable("source_type", func(e *Entry) string { return e.s.Source.Type })
	EntFieldSrcID    = spec.Comparable("source_id", func(e *Entry) string { return e.s.Source.ID })
	EntFieldSrcKey   = spec.Comparable("source_key", func(e *Entry) string { return e.s.Source.Key })
	EntFieldReversed = spec.Comparable("reversed", func(e *Entry) bool { return !e.s.ReversedBy.IsZero() })
)

// CounterKind is the stable aggregate type name.
const CounterKind = "accounting.entry_counter"

// Counter numbers the entries of a company and fiscal year, without gaps (optimistic concurrency).
type Counter struct {
	fw.BaseAggregateRoot[CounterID]
	company OrganizationID
	year    int
	last    int64
}

// ReconstituteCounter rebuilds a counter.
func ReconstituteCounter(id CounterID, company OrganizationID, year int, last int64) (*Counter, error) {
	base, err := fw.NewBaseAggregateRoot(CounterKind, id)
	if err != nil {
		return nil, err
	}
	if company.IsZero() || year < 1990 || last < 0 {
		var v fw.Validation
		v.Add("counter", "invalid", "a counter needs a company, a year and a non-negative number")
		return nil, v.Err()
	}
	return &Counter{BaseAggregateRoot: base, company: company, year: year, last: last}, nil
}

// Next returns the next number.
func (c *Counter) Next() int64 { c.last++; return c.last }

// Values returns the key and the last number.
func (c *Counter) Values() (OrganizationID, int, int64) { return c.company, c.year, c.last }

// Counter fields.
var (
	CntFieldCompany = spec.Comparable("company", func(c *Counter) OrganizationID { return c.company })
	CntFieldYear    = spec.Comparable("fiscal_year", func(c *Counter) int { return c.year })
)

// EntryPosted is raised when an entry is posted.
type EntryPosted struct {
	fw.EventMeta
	Company string `json:"company"`
	Number  int64  `json:"number"`
	Year    int    `json:"year"`
	Source  string `json:"source,omitempty"`
}

// EventType implements domain.Event.
func (EntryPosted) EventType() string { return "accounting.entry_posted" }

// Repositories of the context.
type (
	AccountRepository = fw.Repository[AccountID, *Account]
	LedgerRepository  = fw.Repository[LedgerID, *Ledger]
	EntryRepository   = fw.Repository[EntryID, *Entry]
	CounterRepository = fw.Repository[CounterID, *Counter]
)

// New identities and parsing.
func NewAccountID() AccountID { return AccountID{fw.NewUUID()} }
func NewLedgerID() LedgerID   { return LedgerID{fw.NewUUID()} }
func NewEntryID() EntryID     { return EntryID{fw.NewUUID()} }
func NewCounterID() CounterID { return CounterID{fw.NewUUID()} }

// ParseAccountID parses a textual identity.
func ParseAccountID(s string) (AccountID, error) { u, err := fw.ParseUUID(s); return AccountID{u}, err }

// ParseLedgerID parses a textual identity.
func ParseLedgerID(s string) (LedgerID, error) { u, err := fw.ParseUUID(s); return LedgerID{u}, err }

// ParseEntryID parses a textual identity.
func ParseEntryID(s string) (EntryID, error) { u, err := fw.ParseUUID(s); return EntryID{u}, err }
