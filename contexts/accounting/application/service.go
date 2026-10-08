// Package application holds the Accounting use cases (with permissions and company scope), the
// poster that turns drafts into numbered entries, the subscriptions that post what the other
// contexts publish, and the trial balance and ledger queries.
package application

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/accounting/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/orchestration"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Permissions (the C# defined 28 Accounting.* codes and enforced none).
var (
	PermAccountRead  = authz.MustPermission("Accounting.Account.Read")
	PermAccountWrite = authz.MustPermission("Accounting.Account.Update")
	PermLedgerRead   = authz.MustPermission("Accounting.Ledger.Read")
	PermLedgerWrite  = authz.MustPermission("Accounting.Ledger.Update")
	PermEntryRead    = authz.MustPermission("Accounting.Entry.Read")
	PermEntryCreate  = authz.MustPermission("Accounting.Entry.Create")
	PermEntryReverse = authz.MustPermission("Accounting.Entry.Reverse")
)

// Deps are the ports the use cases need; Parked, Parking, Recorder and Audit are optional.
type Deps struct {
	Accounts domain.AccountRepository
	Ledgers  domain.LedgerRepository
	Entries  domain.EntryRepository
	Counters domain.CounterRepository
	Parked   domain.ParkedRepository
	Parking  *Parking // what stands before the consumer and keeps what cannot be posted yet
	UoW      fw.UnitOfWork
	Recorder app.EventRecorder
	Audit    app.AuditLog
}

// Service exposes the use cases.
type Service struct {
	CreateAccount     app.CommandHandler[CreateAccount, AccountDTO]
	RenameAccount     app.CommandHandler[RenameAccount, AccountDTO]
	DeactivateAccount app.CommandHandler[DeactivateAccount, AccountDTO]
	SearchAccounts    app.QueryHandler[SearchAccounts, []AccountDTO]

	OpenLedger   app.CommandHandler[OpenLedger, LedgerDTO]
	SetProfile   app.CommandHandler[SetProfile, LedgerDTO]
	ClosePeriod  app.CommandHandler[ChangePeriod, LedgerDTO]
	ReopenPeriod app.CommandHandler[ChangePeriod, LedgerDTO]
	GetLedger    app.QueryHandler[GetLedger, LedgerDTO]

	PostEntry     app.CommandHandler[PostEntry, EntryDTO]
	ReverseEntry  app.CommandHandler[ReverseEntry, EntryDTO]
	GetEntry      app.QueryHandler[GetEntry, EntryDTO]
	SearchEntries app.QueryHandler[SearchEntries, fw.Page[EntryDTO]]
	TrialBalance  app.QueryHandler[TrialBalance, []BalanceRow]
	AccountLedger app.QueryHandler[AccountLedger, []Movement]

	RetryParked   app.CommandHandler[RetryParked, RetriedDTO]
	DiscardParked app.CommandHandler[DiscardParked, ParkedDTO]
	SearchParked  app.QueryHandler[SearchParked, fw.Page[ParkedDTO]]
}

type scope struct {
	global bool
	ac     *authz.Context
	orgs   []domain.OrganizationID
}

func scopeOf(ctx context.Context) scope {
	ac, ok := authz.FromContext(ctx)
	if !ok {
		return scope{}
	}
	s := scope{global: ac.GlobalAdmin, ac: ac}
	for _, u := range ac.EffectiveOrganizations {
		s.orgs = append(s.orgs, domain.OrganizationID{UUID: u})
	}
	return s
}

// check returns a uniform 404 outside the company's scope and 403 when it is read-only.
func (s scope) check(kind string, id fmt.Stringer, org domain.OrganizationID, write bool) error {
	if !s.global && !slices.Contains(s.orgs, org) {
		return fw.NotFound(kind, id)
	}
	if write && !s.global && (s.ac == nil || !s.ac.CanWrite(org.UUID)) {
		return fmt.Errorf("%w: read-only in your organization scope", fw.ErrForbidden)
	}
	return nil
}

type service struct {
	Deps
	accounts *orchestration.Orchestrator[domain.AccountID, *domain.Account]
	ledgers  *orchestration.Orchestrator[domain.LedgerID, *domain.Ledger]
	entries  *orchestration.Orchestrator[domain.EntryID, *domain.Entry]
}

func newService(d Deps) service {
	var opts []orchestration.Option
	if d.Recorder != nil {
		opts = append(opts, orchestration.WithOutbox(d.Recorder))
	}
	if d.Audit != nil {
		opts = append(opts, orchestration.WithAuditLog(d.Audit))
	}
	return service{Deps: d,
		accounts: orchestration.New[domain.AccountID, *domain.Account](d.Accounts, d.UoW, opts...),
		ledgers:  orchestration.New[domain.LedgerID, *domain.Ledger](d.Ledgers, d.UoW, opts...),
		entries:  orchestration.New[domain.EntryID, *domain.Entry](d.Entries, d.UoW, opts...),
	}
}

func parseID(v *fw.Validation, field, s string) fw.UUID {
	u, err := fw.ParseUUID(s)
	v.Require(err == nil && !u.IsZero(), field, "format", field+" must be an id")
	return u
}

func parseDecimal(v *fw.Validation, field, s string) vocab.Decimal {
	if s == "" {
		return vocab.DecimalFromInt(0)
	}
	d, err := vocab.ParseDecimal(s)
	v.Require(err == nil, field, "format", field+" must be a decimal number")
	return d
}

func money(d vocab.Decimal) string { return d.StringFixed(2) }

// NewService wires the use cases.
func NewService(d Deps) *Service {
	s := newService(d)
	svc := &Service{}
	s.chartUseCases(svc)
	s.entryUseCases(svc)
	s.parkedUseCases(svc)
	return svc
}

func guard[In, Out any](p authz.Permission, fn func(context.Context, In) (Out, error), mw ...app.Middleware[In, Out]) app.Handler[In, Out] {
	return app.Chain[In, Out](app.HandlerFunc[In, Out](fn), append([]app.Middleware[In, Out]{pipeline.RequirePermission[In, Out](p)}, mw...)...)
}

func retry[In, Out any]() app.Middleware[In, Out] {
	return pipeline.RetryOnConflict[In, Out](5, 10*time.Millisecond)
}

// ledgerOf returns the ledger of a company.
func (s service) ledgerOf(ctx context.Context, company domain.OrganizationID) (*domain.Ledger, error) {
	ls, err := s.Ledgers.Find(ctx, domain.LedFieldCompany.Eq(company))
	if err != nil {
		return nil, err
	}
	if len(ls) == 0 {
		return nil, fw.Violation("accounting.no_ledger", "the company has no ledger")
	}
	return ls[0], nil
}

// post turns a draft into a numbered entry: the ledger decides the period (open), every account
// exists in the company chart as an active detail account, the number is taken in the same unit
// of work. A draft with a source already posted returns that entry (idempotency).
func (s service) post(ctx context.Context, d domain.Draft) (*domain.Entry, error) {
	if d.Source.ID != "" {
		found, err := s.Entries.Find(ctx, spec.And(domain.EntFieldCompany.Eq(d.Company), domain.EntFieldSrcType.Eq(d.Source.Type),
			domain.EntFieldSrcID.Eq(d.Source.ID)))
		if err != nil || len(found) > 0 {
			if len(found) > 0 {
				return found[0], nil
			}
			return nil, err
		}
	}
	l, err := s.ledgerOf(ctx, d.Company)
	if err != nil {
		return nil, err
	}
	lines := domain.Normalize(d.Lines)
	var codes []string
	for _, x := range lines {
		if !slices.Contains(codes, x.Account) {
			codes = append(codes, x.Account)
		}
	}
	if len(codes) > 0 {
		as, err := s.Accounts.Find(ctx, domain.AccFieldCompany.Eq(d.Company).And(domain.AccFieldCode.In(codes...)))
		if err != nil {
			return nil, err
		}
		for _, c := range codes {
			k := slices.IndexFunc(as, func(a *domain.Account) bool { return a.State().Code == c })
			if k < 0 {
				return nil, fw.Violation("accounting.unknown_account", "the account "+c+" is not in the chart")
			}
			if st := as[k].State(); !st.Postable || !st.Active {
				return nil, fw.Violation("accounting.account_not_postable", "the account "+c+" is a header or inactive")
			}
		}
	}
	year := l.PeriodOf(d.Date).Year
	cs, err := s.Counters.Find(ctx, spec.And(domain.CntFieldCompany.Eq(d.Company), domain.CntFieldYear.Eq(year)))
	if err != nil {
		return nil, err
	}
	var c *domain.Counter
	if len(cs) > 0 {
		c = cs[0]
	} else if c, err = domain.ReconstituteCounter(domain.NewCounterID(), d.Company, year, 0); err != nil {
		return nil, err
	}
	n := c.Next()
	e, err := domain.Post(domain.NewEntryID(), d, l, n)
	if err != nil {
		return nil, err
	}
	if err := s.Counters.Save(ctx, c); err != nil {
		return nil, err
	}
	if err := s.entries.Create(ctx, e); err != nil {
		return nil, err
	}
	return e, nil
}

// reverse posts the reversal of an entry and links both.
func (s service) reverse(ctx context.Context, id domain.EntryID, on vocab.Date, src domain.Source) (*domain.Entry, error) {
	e, err := s.Entries.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	d, err := e.Reversal(on, src)
	if err != nil {
		return nil, err
	}
	r, err := s.post(ctx, d)
	if err != nil {
		return nil, err
	}
	if _, err := s.entries.Update(ctx, r.ID(), func(_ context.Context, r *domain.Entry) error { r.LinkReversal(id); return nil }); err != nil {
		return nil, err
	}
	if _, err := s.entries.Update(ctx, id, func(_ context.Context, e *domain.Entry) error { return e.MarkReversed(r.ID()) }); err != nil {
		return nil, err
	}
	return s.Entries.Get(ctx, r.ID())
}
