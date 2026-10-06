package domain

import (
	"slices"
	"strings"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// StatementKind is the stable aggregate type name.
const StatementKind = "treasury.statement"

// MaxStatementLines bounds the movements of a statement.
const MaxStatementLines = 5000

// StatementID identifies a bank statement.
type StatementID struct{ fw.UUID }

// What a statement line is reconciled against.
const (
	MatchRemittance = "remittance"     // a remittance of direct debits credited by the bank
	MatchTransfers  = "transfer-order" // an order of credit transfers charged by the bank
	MatchOther      = "other"          // anything else, explained by a note (fees, interest, taxes)
)

// Match is what explains a line of a statement.
type Match struct {
	Kind string
	ID   string
	Note string
}

// IsZero reports whether the line is not reconciled.
func (m Match) IsZero() bool { return m.Kind == "" }

// StatementLine is a movement of the account as the bank reports it: positive when the bank
// credits the account, negative when it charges it.
type StatementLine struct {
	No        int
	Date      vocab.Date
	ValueDate vocab.Date
	Amount    vocab.Decimal
	Concept   string
	Reference string
	Match     Match
}

// StatementState is the persisted state of a statement.
type StatementState struct {
	Owner   OrganizationID
	Account AccountID
	From    vocab.Date
	To      vocab.Date
	Opening vocab.Decimal
	Closing vocab.Decimal
	Lines   []StatementLine
	Audit   traits.AuditStamp
}

// Statement is what the bank says happened in an account in a period: its balances and its
// movements, each reconciled against what explains it (the C# BankStatementLine pointed at no
// bank account, and nothing ever set it as matched).
type Statement struct {
	fw.BaseAggregateRoot[StatementID]
	traits.Audited
	s StatementState
}

func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > n {
		s = string([]rune(s)[:n])
	}
	return s
}

func validMatch(m Match) bool {
	switch m.Kind {
	case "":
		return m.ID == "" && m.Note == ""
	case MatchRemittance, MatchTransfers:
		return m.ID != "" && len(m.ID) <= 64 && utf8.RuneCountInString(m.Note) <= 200
	case MatchOther:
		return m.ID == "" && m.Note != "" && utf8.RuneCountInString(m.Note) <= 200
	}
	return false
}

// ReconstituteStatement rebuilds a statement.
func ReconstituteStatement(id StatementID, s StatementState) (*Statement, error) {
	base, err := fw.NewBaseAggregateRoot(StatementKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(!s.Owner.IsZero() && !s.Account.IsZero(), "account", "required", "the account and its owner are required")
	v.Require(!s.From.IsZero() && !s.To.IsZero() && !s.To.Before(s.From), "period", "range", "a period from a day to a day not before it")
	v.Require(cents(s.Opening) && cents(s.Closing), "balances", "cents", "balances in cents")
	v.Require(len(s.Lines) <= MaxStatementLines, "lines", "range", "at most 5000 movements")
	total := s.Opening
	s.Lines = slices.Clone(s.Lines)
	for i := range s.Lines {
		l := &s.Lines[i]
		l.Concept, l.Reference = clip(l.Concept, 500), clip(l.Reference, 60)
		if l.ValueDate.IsZero() {
			l.ValueDate = l.Date
		}
		v.Require(l.No == i+1, "lines", "order", "movements are numbered from 1")
		v.Require(!l.Date.IsZero() && !l.Date.Before(s.From) && !l.Date.After(s.To), "lines", "date", "movements dated within the period")
		v.Require(!l.Amount.IsZero() && cents(l.Amount), "lines", "amount", "amounts in cents, not zero")
		v.Require(validMatch(l.Match), "lines", "match", "a remittance, a transfer order or a note")
		total = total.Add(l.Amount)
	}
	v.Require(total.Equal(s.Closing), "closing", "balance", "the opening balance plus the movements is the closing balance")
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &Statement{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

func cents(d vocab.Decimal) bool { return d.Equal(d.Round(2)) }

// ImportStatement records a statement of an account as the bank gave it, with nothing reconciled.
func ImportStatement(id StatementID, s StatementState) (*Statement, error) {
	s.Lines = slices.Clone(s.Lines)
	for i := range s.Lines {
		s.Lines[i].No, s.Lines[i].Match = i+1, Match{}
	}
	st, err := ReconstituteStatement(id, s)
	if err != nil {
		return nil, err
	}
	st.Raise(StatementImported{EventMeta: st.NewEventMeta(), Owner: s.Owner.String(), Account: s.Account.String(), From: s.From.String(),
		To: s.To.String(), Movements: len(s.Lines), Closing: s.Closing.StringFixed(2)})
	return st, nil
}

// State returns the state (lines are a copy).
func (s *Statement) State() StatementState {
	st := s.s
	st.Lines = slices.Clone(st.Lines)
	return st
}

// Pending returns the movements not reconciled yet.
func (s *Statement) Pending() int {
	n := 0
	for _, l := range s.s.Lines {
		if l.Match.IsZero() {
			n++
		}
	}
	return n
}

// Line returns a movement by its number.
func (s *Statement) Line(no int) (StatementLine, error) {
	if no < 1 || no > len(s.s.Lines) {
		return StatementLine{}, fw.Violation("treasury.statement_line", "the statement has no such movement")
	}
	return s.s.Lines[no-1], nil
}

// Reconcile explains a movement: once, until it is released.
func (s *Statement) Reconcile(no int, m Match) error {
	l, err := s.Line(no)
	if err != nil {
		return err
	}
	if !l.Match.IsZero() {
		return fw.Violation("treasury.line_reconciled", "the movement is already reconciled")
	}
	m.Note = clip(m.Note, 200)
	if m.IsZero() || !validMatch(m) {
		return fw.Violation("treasury.match", "a remittance, a transfer order, or a note that explains the movement")
	}
	lines := slices.Clone(s.s.Lines)
	lines[no-1].Match = m
	s.s.Lines = lines
	if s.Pending() == 0 {
		s.Raise(StatementReconciled{EventMeta: s.NewEventMeta(), Owner: s.s.Owner.String(), Account: s.s.Account.String(), To: s.s.To.String()})
	}
	return nil
}

// Release undoes the reconciliation of a movement.
func (s *Statement) Release(no int) error {
	l, err := s.Line(no)
	if err != nil {
		return err
	}
	if l.Match.IsZero() {
		return fw.Violation("treasury.line_pending", "the movement is not reconciled")
	}
	lines := slices.Clone(s.s.Lines)
	lines[no-1].Match = Match{}
	s.s.Lines = lines
	return nil
}

// AuditSnapshot implements traits.Snapshotter.
func (s *Statement) AuditSnapshot() map[string]any {
	return map[string]any{"account": s.s.Account.String(), "from": s.s.From.String(), "to": s.s.To.String(), "closing": s.s.Closing.String(),
		"pending": s.Pending()}
}

// Statement fields.
var (
	StmFieldOwner     = spec.Comparable("owner", func(s *Statement) OrganizationID { return s.s.Owner })
	StmFieldAccount   = spec.Comparable("account_id", func(s *Statement) AccountID { return s.s.Account })
	StmFieldFrom      = spec.OrderedBy("period_from", func(s *Statement) vocab.Date { return s.s.From }, vocab.CompareDates)
	StmFieldTo        = spec.OrderedBy("period_to", func(s *Statement) vocab.Date { return s.s.To }, vocab.CompareDates)
	StmFieldPending   = spec.Ordered("pending", func(s *Statement) int { return s.Pending() })
	StmFieldLines     = spec.Collection("lines", func(s *Statement) []StatementLine { return s.s.Lines })
	LineFieldMatch    = spec.Comparable("match_kind", func(l StatementLine) string { return l.Match.Kind })
	LineFieldMatchRef = spec.Comparable("match_id", func(l StatementLine) string { return l.Match.ID })
)

// ReconciledAgainst selects the statements with a movement reconciled against something.
func ReconciledAgainst(kind, id string) spec.Spec[*Statement] {
	return StmFieldLines.Any(LineFieldMatch.Eq(kind).And(LineFieldMatchRef.Eq(id)))
}

// Events of statements.
type (
	// StatementImported is raised when a statement is recorded.
	StatementImported struct {
		fw.EventMeta
		Owner     string `json:"owner"`
		Account   string `json:"account"`
		From      string `json:"from"`
		To        string `json:"to"`
		Movements int    `json:"movements"`
		Closing   string `json:"closing"`
	}
	// StatementReconciled is raised when the last movement of a statement is explained.
	StatementReconciled struct {
		fw.EventMeta
		Owner   string `json:"owner"`
		Account string `json:"account"`
		To      string `json:"to"`
	}
)

// EventType implementations.
func (StatementImported) EventType() string   { return "treasury.statement_imported" }
func (StatementReconciled) EventType() string { return "treasury.statement_reconciled" }

// NewStatementID returns a new identity.
func NewStatementID() StatementID { return StatementID{fw.NewUUID()} }

// ParseStatementID parses a textual identity.
func ParseStatementID(s string) (StatementID, error) {
	u, err := fw.ParseUUID(s)
	return StatementID{u}, err
}

// StatementRepository stores statements.
type StatementRepository = fw.Repository[StatementID, *Statement]
