package application

import (
	"context"
	"slices"
	"strings"

	"github.com/jhermoso/karpo-fw-go/contexts/treasury/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Statement permissions: recording what the bank says and explaining it are separate.
var (
	PermStatementRead      = authz.MustPermission("Treasury.Statement.Read")
	PermStatementImport    = authz.MustPermission("Treasury.Statement.Import")
	PermStatementReconcile = authz.MustPermission("Treasury.Statement.Reconcile")
)

// MatchWindowDays is how far the date of a movement may be from the date of the remittance or
// transfer order proposed for it.
const MatchWindowDays = 5

// MovementInput is a movement of a statement recorded by hand: positive when the bank credits
// the account, negative when it charges it.
type MovementInput struct {
	Date      vocab.Date `json:"date"`
	ValueDate vocab.Date `json:"valueDate,omitzero"`
	Amount    string     `json:"amount"`
	Concept   string     `json:"concept,omitempty"`
	Reference string     `json:"reference,omitempty"`
}

// ImportStatement records the statement of an account in a period.
type ImportStatement struct {
	Account string          `json:"account"`
	From    vocab.Date      `json:"from"`
	To      vocab.Date      `json:"to"`
	Opening string          `json:"opening"`
	Closing string          `json:"closing"`
	Lines   []MovementInput `json:"lines"`
}

// ImportNorma43 records the statements of a Cuaderno 43 file of an organization: each account of
// the file must be one of its accounts.
type ImportNorma43 struct {
	Owner   string `json:"owner"`
	Content string `json:"content"`
}

// ReconcileLine explains a movement: against a remittance, a transfer order, or with a note.
type ReconcileLine struct {
	ID   domain.StatementID `json:"-"`
	Line int                `json:"line"`
	Kind string             `json:"kind"`
	Ref  string             `json:"ref,omitempty"`
	Note string             `json:"note,omitempty"`
}

// ReleaseLine undoes the reconciliation of a movement.
type ReleaseLine struct {
	ID   domain.StatementID `json:"-"`
	Line int                `json:"line"`
}

// AutoReconcile explains every movement that has exactly one candidate: a remittance or a
// transfer order of the account with the same amount and a close date, not reconciled yet.
type AutoReconcile struct {
	ID domain.StatementID `json:"-"`
}

// GetStatement loads a statement with its movements.
type GetStatement struct{ ID domain.StatementID }

// SearchStatements searches the statements of the caller's scope.
type SearchStatements struct {
	Owner, Account string
	Pending        bool // with movements to reconcile
	Page, Size     int
}

// MovementDTO is the transport form of a movement.
type MovementDTO struct {
	Line      int    `json:"line"`
	Date      string `json:"date"`
	ValueDate string `json:"valueDate"`
	Amount    string `json:"amount"`
	Concept   string `json:"concept,omitempty"`
	Reference string `json:"reference,omitempty"`
	MatchKind string `json:"matchKind,omitempty"`
	MatchRef  string `json:"matchRef,omitempty"`
	MatchNote string `json:"matchNote,omitempty"`
}

// StatementDTO is the transport form of a statement.
type StatementDTO struct {
	ID        string        `json:"id"`
	Owner     string        `json:"owner"`
	Account   string        `json:"account"`
	From      string        `json:"from"`
	To        string        `json:"to"`
	Opening   string        `json:"opening"`
	Closing   string        `json:"closing"`
	Movements int           `json:"movements"`
	Pending   int           `json:"pending"`
	Matched   int           `json:"matched,omitempty"` // movements explained by the last automatic run
	Lines     []MovementDTO `json:"lines,omitempty"`
	Version   int64         `json:"version"`
}

func statementDTO(s *domain.Statement, lines bool) StatementDTO {
	st := s.State()
	d := StatementDTO{ID: s.ID().String(), Owner: st.Owner.String(), Account: st.Account.String(), From: st.From.String(), To: st.To.String(),
		Opening: st.Opening.StringFixed(2), Closing: st.Closing.StringFixed(2), Movements: len(st.Lines), Pending: s.Pending(), Version: s.Version()}
	if lines {
		for _, l := range st.Lines {
			d.Lines = append(d.Lines, MovementDTO{Line: l.No, Date: l.Date.String(), ValueDate: l.ValueDate.String(), Amount: l.Amount.StringFixed(2),
				Concept: l.Concept, Reference: l.Reference, MatchKind: l.Match.Kind, MatchRef: l.Match.ID, MatchNote: l.Match.Note})
		}
	}
	return d
}

func statementAmount(v *fw.Validation, field, s string) vocab.Decimal {
	d, err := vocab.ParseDecimal(strings.TrimSpace(s))
	v.Require(err == nil, field, "format", field+" must be a decimal number")
	return d
}

// record stores a statement of an account after checking it continues the previous ones: periods
// do not overlap, and the opening balance is the closing balance of the statement before.
func (s service) record(ctx context.Context, acc *domain.Account, st domain.StatementState) (*domain.Statement, error) {
	st.Owner, st.Account = acc.State().Owner, acc.ID()
	stm, err := domain.ImportStatement(domain.NewStatementID(), st)
	if err != nil {
		return nil, err
	}
	previous, err := s.Statements.Find(ctx, domain.StmFieldAccount.Eq(acc.ID()), domain.StmFieldFrom.Asc())
	if err != nil {
		return nil, err
	}
	var before *domain.Statement
	for _, p := range previous {
		ps := p.State()
		if !ps.To.Before(st.From) && !st.To.Before(ps.From) {
			return nil, fw.Violation("treasury.statement_overlap", "the account already has a statement of "+ps.From.String()+" to "+ps.To.String())
		}
		if ps.To.Before(st.From) {
			before = p
		}
	}
	if before != nil && !before.State().Closing.Equal(st.Opening) {
		return nil, fw.Violation("treasury.statement_gap", "the opening balance is not the closing balance of the previous statement ("+
			before.State().Closing.StringFixed(2)+" on "+before.State().To.String()+")")
	}
	return stm, s.statements.Create(ctx, stm)
}

// candidate is something a movement may be reconciled against.
type candidate struct {
	kind, id string
	amount   vocab.Decimal // as the bank would report it: positive credits the account
	date     vocab.Date
}

// candidates lists the remittances and transfer orders of an account sent to the bank.
func (s service) candidates(ctx context.Context, owner domain.OrganizationID, account domain.AccountID) ([]candidate, error) {
	sent := []int{int(domain.RemGenerated), int(domain.RemSettled)}
	var out []candidate
	rems, err := s.Remittances.Find(ctx, spec.And(domain.RemFieldCreditor.Eq(owner), domain.RemFieldStatus.In(sent...)), domain.RemFieldDate.Asc())
	if err != nil {
		return nil, err
	}
	for _, r := range rems {
		if r.State().Account == account {
			out = append(out, candidate{kind: domain.MatchRemittance, id: r.ID().String(), amount: r.Total(), date: r.State().CollectionDate})
		}
	}
	trfs, err := s.Transfers.Find(ctx, spec.And(domain.TrfFieldDebtor.Eq(owner), domain.TrfFieldStatus.In(sent...)), domain.TrfFieldDate.Asc())
	if err != nil {
		return nil, err
	}
	for _, o := range trfs {
		if o.State().Account == account {
			out = append(out, candidate{kind: domain.MatchTransfers, id: o.ID().String(), amount: o.Total().Neg(), date: o.State().ExecutionDate})
		}
	}
	return out, nil
}

func (s service) taken(ctx context.Context, kind, id string) (bool, error) {
	return s.Statements.Exists(ctx, domain.ReconciledAgainst(kind, id))
}

func (s service) statementUseCases(svc *Service) {
	d := s.Deps
	account := func(ctx context.Context, id domain.AccountID) (*domain.Account, error) {
		acc, err := d.Accounts.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		return acc, scopeOf(ctx).check(domain.AccountKind, acc.ID(), acc.State().Owner, true)
	}

	svc.ImportStatement = guard(PermStatementImport, func(ctx context.Context, c ImportStatement) (StatementDTO, error) {
		var v fw.Validation
		aid := domain.AccountID{UUID: parseID(&v, "account", c.Account)}
		st := domain.StatementState{From: c.From, To: c.To, Opening: statementAmount(&v, "opening", c.Opening), Closing: statementAmount(&v, "closing", c.Closing)}
		for _, l := range c.Lines {
			st.Lines = append(st.Lines, domain.StatementLine{Date: l.Date, ValueDate: l.ValueDate, Amount: statementAmount(&v, "lines.amount", l.Amount),
				Concept: l.Concept, Reference: l.Reference})
		}
		if err := v.Err(); err != nil {
			return StatementDTO{}, err
		}
		acc, err := account(ctx, aid)
		if err != nil {
			return StatementDTO{}, err
		}
		stm, err := s.record(ctx, acc, st)
		if err != nil {
			return StatementDTO{}, err
		}
		return statementDTO(stm, true), nil
	}, retry[ImportStatement, StatementDTO](), pipeline.Transactional[ImportStatement, StatementDTO](d.UoW))

	// A file is recorded whole or not at all.
	svc.ImportNorma43 = guard(PermStatementImport, func(ctx context.Context, c ImportNorma43) ([]StatementDTO, error) {
		var v fw.Validation
		owner := domain.OrganizationID{UUID: parseID(&v, "owner", c.Owner)}
		v.Require(strings.TrimSpace(c.Content) != "", "content", "required", "the content of the file is required")
		if err := v.Err(); err != nil {
			return nil, err
		}
		if err := scopeOf(ctx).check("parties.party", owner, owner, true); err != nil {
			return nil, err
		}
		parsed, err := domain.ParseNorma43(c.Content)
		if err != nil {
			return nil, err
		}
		accounts, err := d.Accounts.Find(ctx, domain.AccFieldOwner.Eq(owner))
		if err != nil {
			return nil, err
		}
		out := []StatementDTO{}
		for _, p := range parsed {
			i := slices.IndexFunc(accounts, func(a *domain.Account) bool { return p.Matches(a.State().IBAN) })
			if i < 0 {
				return nil, fw.Violation("treasury.unknown_account", "the organization has no account "+p.Bank+" "+p.Office+" "+p.Number)
			}
			stm, err := s.record(ctx, accounts[i], domain.StatementState{From: p.From, To: p.To, Opening: p.Opening, Closing: p.Closing, Lines: p.Lines})
			if err != nil {
				return nil, err
			}
			out = append(out, statementDTO(stm, false))
		}
		return out, nil
	}, retry[ImportNorma43, []StatementDTO](), pipeline.Transactional[ImportNorma43, []StatementDTO](d.UoW))

	update := func(ctx context.Context, id domain.StatementID, fn func(context.Context, *domain.Statement) error) (*domain.Statement, error) {
		sc := scopeOf(ctx)
		return s.statements.Update(ctx, id, func(ctx context.Context, stm *domain.Statement) error {
			if err := sc.check(domain.StatementKind, stm.ID(), stm.State().Owner, true); err != nil {
				return err
			}
			return fn(ctx, stm)
		})
	}

	// Against a remittance or a transfer order the amounts must be the same, it must have been sent
	// to the bank from that account, and nothing else may be reconciled against it.
	svc.Reconcile = guard(PermStatementReconcile, func(ctx context.Context, c ReconcileLine) (StatementDTO, error) {
		kind := strings.TrimSpace(c.Kind)
		var v fw.Validation
		v.Require(slices.Contains([]string{domain.MatchRemittance, domain.MatchTransfers, domain.MatchOther}, kind), "kind", "enum",
			"remittance, transfer-order or other")
		ref := ""
		if kind != domain.MatchOther && kind != "" {
			ref = parseID(&v, "ref", c.Ref).String()
		}
		if err := v.Err(); err != nil {
			return StatementDTO{}, err
		}
		stm, err := update(ctx, c.ID, func(ctx context.Context, stm *domain.Statement) error {
			line, err := stm.Line(c.Line)
			if err != nil {
				return err
			}
			if kind != domain.MatchOther {
				cands, err := s.candidates(ctx, stm.State().Owner, stm.State().Account)
				if err != nil {
					return err
				}
				i := slices.IndexFunc(cands, func(x candidate) bool { return x.kind == kind && x.id == ref })
				if i < 0 {
					return fw.Violation("treasury.match_unknown", "nothing like that was sent to the bank from this account")
				}
				if !cands[i].amount.Equal(line.Amount) {
					return fw.Violation("treasury.match_amount", "the movement is of "+line.Amount.StringFixed(2)+" and what it is reconciled against of "+
						cands[i].amount.StringFixed(2))
				}
				used, err := s.taken(ctx, kind, ref)
				if err != nil {
					return err
				}
				if used {
					return fw.Violation("treasury.match_taken", "another movement is already reconciled against it")
				}
			}
			return stm.Reconcile(c.Line, domain.Match{Kind: kind, ID: ref, Note: c.Note})
		})
		if err != nil {
			return StatementDTO{}, err
		}
		return statementDTO(stm, true), nil
	}, retry[ReconcileLine, StatementDTO](), pipeline.Transactional[ReconcileLine, StatementDTO](d.UoW))

	svc.Release = guard(PermStatementReconcile, func(ctx context.Context, c ReleaseLine) (StatementDTO, error) {
		stm, err := update(ctx, c.ID, func(_ context.Context, stm *domain.Statement) error { return stm.Release(c.Line) })
		if err != nil {
			return StatementDTO{}, err
		}
		return statementDTO(stm, true), nil
	}, retry[ReleaseLine, StatementDTO](), pipeline.Transactional[ReleaseLine, StatementDTO](d.UoW))

	svc.AutoReconcile = guard(PermStatementReconcile, func(ctx context.Context, c AutoReconcile) (StatementDTO, error) {
		matched := 0
		stm, err := update(ctx, c.ID, func(ctx context.Context, stm *domain.Statement) error {
			matched = 0
			cands, err := s.candidates(ctx, stm.State().Owner, stm.State().Account)
			if err != nil {
				return err
			}
			free := cands[:0:0]
			for _, x := range cands {
				used, err := s.taken(ctx, x.kind, x.id)
				if err != nil {
					return err
				}
				if !used {
					free = append(free, x)
				}
			}
			for _, l := range stm.State().Lines {
				if !l.Match.IsZero() {
					continue
				}
				var fit []int
				for i, x := range free {
					if days := x.date.DaysUntil(l.Date); x.amount.Equal(l.Amount) && days >= -MatchWindowDays && days <= MatchWindowDays {
						fit = append(fit, i)
					}
				}
				if len(fit) != 1 {
					continue // nothing, or a choice a person must make
				}
				x := free[fit[0]]
				if err := stm.Reconcile(l.No, domain.Match{Kind: x.kind, ID: x.id}); err != nil {
					return err
				}
				free = slices.Delete(free, fit[0], fit[0]+1)
				matched++
			}
			return nil
		})
		if err != nil {
			return StatementDTO{}, err
		}
		out := statementDTO(stm, true)
		out.Matched = matched
		return out, nil
	}, retry[AutoReconcile, StatementDTO](), pipeline.Transactional[AutoReconcile, StatementDTO](d.UoW))

	svc.GetStatement = guard(PermStatementRead, func(ctx context.Context, q GetStatement) (StatementDTO, error) {
		stm, err := d.Statements.Get(ctx, q.ID)
		if err != nil {
			return StatementDTO{}, err
		}
		if err := scopeOf(ctx).check(domain.StatementKind, stm.ID(), stm.State().Owner, false); err != nil {
			return StatementDTO{}, err
		}
		return statementDTO(stm, true), nil
	})

	svc.SearchStatements = guard(PermStatementRead, func(ctx context.Context, q SearchStatements) (fw.Page[StatementDTO], error) {
		var v fw.Validation
		parts := []spec.Specification[*domain.Statement]{within(scopeOf(ctx), domain.StmFieldOwner)}
		if q.Owner != "" {
			parts = append(parts, domain.StmFieldOwner.Eq(domain.OrganizationID{UUID: parseID(&v, "owner", q.Owner)}))
		}
		if q.Account != "" {
			parts = append(parts, domain.StmFieldAccount.Eq(domain.AccountID{UUID: parseID(&v, "account", q.Account)}))
		}
		if q.Pending {
			parts = append(parts, domain.StmFieldPending.Gt(0))
		}
		if err := v.Err(); err != nil {
			return fw.Page[StatementDTO]{}, err
		}
		page, err := d.Statements.FindPage(ctx, spec.And(parts...), fw.NewPageRequest(q.Page, q.Size, domain.StmFieldFrom.Asc()))
		if err != nil {
			return fw.Page[StatementDTO]{}, err
		}
		return fw.MapPage(page, func(s *domain.Statement) StatementDTO { return statementDTO(s, false) }), nil
	})
}
