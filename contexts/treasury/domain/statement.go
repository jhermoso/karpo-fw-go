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

// StatementKind es el nombre estable del tipo de agregado.
const StatementKind = "treasury.statement"

// MaxStatementLines limita los movimientos de un extracto.
const MaxStatementLines = 5000

// StatementID identifica un extracto bancario.
type StatementID struct{ fw.UUID }

// Contra qué se concilia una línea de extracto.
const (
	MatchRemittance = "remittance"     // una remesa de adeudos abonada por el banco
	MatchTransfers  = "transfer-order" // una orden de transferencias cargada por el banco
	MatchOther      = "other"          // cualquier otra cosa, explicada con una nota (comisiones, intereses, impuestos)
)

// Match es lo que explica una línea de un extracto.
type Match struct {
	Kind string
	ID   string
	Note string
}

// IsZero dice si la línea no está conciliada.
func (m Match) IsZero() bool { return m.Kind == "" }

// StatementLine es un movimiento de la cuenta tal como lo da el banco: positivo cuando el banco
// abona en la cuenta, negativo cuando carga en ella.
type StatementLine struct {
	No        int
	Date      vocab.Date
	ValueDate vocab.Date
	Amount    vocab.Decimal
	Concept   string
	Reference string
	Match     Match
}

// StatementState es el estado persistido de un extracto.
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

// Statement es lo que el banco dice que pasó en una cuenta en un periodo: sus saldos y sus
// movimientos, cada uno conciliado contra lo que lo explica (el BankStatementLine de C# no apuntaba
// a ninguna cuenta bancaria, y nada lo marcaba nunca como conciliado).
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

// ReconstituteStatement reconstruye un extracto.
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

// ImportStatement registra un extracto de una cuenta tal como lo dio el banco, sin nada conciliado.
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

// State devuelve el estado (las líneas son una copia).
func (s *Statement) State() StatementState {
	st := s.s
	st.Lines = slices.Clone(st.Lines)
	return st
}

// Pending devuelve los movimientos aún sin conciliar.
func (s *Statement) Pending() int {
	n := 0
	for _, l := range s.s.Lines {
		if l.Match.IsZero() {
			n++
		}
	}
	return n
}

// Line devuelve un movimiento por su número.
func (s *Statement) Line(no int) (StatementLine, error) {
	if no < 1 || no > len(s.s.Lines) {
		return StatementLine{}, fw.Violation("treasury.statement_line", "the statement has no such movement")
	}
	return s.s.Lines[no-1], nil
}

// Reconcile explica un movimiento: una vez, hasta que se libera.
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

// Release deshace la conciliación de un movimiento.
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

// AuditSnapshot implementa traits.Snapshotter.
func (s *Statement) AuditSnapshot() map[string]any {
	return map[string]any{"account": s.s.Account.String(), "from": s.s.From.String(), "to": s.s.To.String(), "closing": s.s.Closing.String(),
		"pending": s.Pending()}
}

// Campos del extracto.
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

// ReconciledAgainst selecciona los extractos con un movimiento conciliado contra algo.
func ReconciledAgainst(kind, id string) spec.Spec[*Statement] {
	return StmFieldLines.Any(LineFieldMatch.Eq(kind).And(LineFieldMatchRef.Eq(id)))
}

// Eventos de los extractos.
type (
	// StatementImported se lanza al registrar un extracto.
	StatementImported struct {
		fw.EventMeta
		Owner     string `json:"owner"`
		Account   string `json:"account"`
		From      string `json:"from"`
		To        string `json:"to"`
		Movements int    `json:"movements"`
		Closing   string `json:"closing"`
	}
	// StatementReconciled se lanza cuando se explica el último movimiento de un extracto.
	StatementReconciled struct {
		fw.EventMeta
		Owner   string `json:"owner"`
		Account string `json:"account"`
		To      string `json:"to"`
	}
)

// Implementaciones de EventType.
func (StatementImported) EventType() string   { return "treasury.statement_imported" }
func (StatementReconciled) EventType() string { return "treasury.statement_reconciled" }

// NewStatementID devuelve una identidad nueva.
func NewStatementID() StatementID { return StatementID{fw.NewUUID()} }

// ParseStatementID interpreta una identidad en texto.
func ParseStatementID(s string) (StatementID, error) {
	u, err := fw.ParseUUID(s)
	return StatementID{u}, err
}

// StatementRepository guarda extractos.
type StatementRepository = fw.Repository[StatementID, *Statement]
