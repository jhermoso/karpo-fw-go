// Package application holds the Treasury use cases (with permissions and organization scope), the
// ports Treasury consumes (Receivables due items, Parties identities) and the translation to its
// Published Language.
package application

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/treasury/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/treasury/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/application/orchestration"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Permissions (the C# bank endpoints required authentication only). Generating the file and
// settling are permissions of their own, apart from preparing.
var (
	PermAccountRead    = authz.MustPermission("Treasury.Account.Read")
	PermAccountUpdate  = authz.MustPermission("Treasury.Account.Update")
	PermMandateRead    = authz.MustPermission("Treasury.Mandate.Read")
	PermMandateUpdate  = authz.MustPermission("Treasury.Mandate.Update")
	PermRemittanceRead = authz.MustPermission("Treasury.Remittance.Read")
	PermRemittanceEdit = authz.MustPermission("Treasury.Remittance.Update")
	PermRemittanceSend = authz.MustPermission("Treasury.Remittance.Generate")
	PermRemittanceBank = authz.MustPermission("Treasury.Remittance.Settle")
)

// Deps are the ports the use cases need; Recorder and Audit are optional.
type Deps struct {
	Accounts    domain.AccountRepository
	Mandates    domain.MandateRepository
	Remittances domain.RemittanceRepository
	Transfers   domain.TransferOrderRepository
	Receivables domain.Receivables
	Payables    domain.Payables // optional: without it there are no transfer proposals
	Identities  domain.Identities
	UoW         fw.UnitOfWork
	Recorder    app.EventRecorder
	Audit       app.AuditLog
}

// Service exposes the use cases.
type Service struct {
	OpenAccount    app.CommandHandler[OpenAccount, AccountDTO]
	CloseAccount   app.CommandHandler[CloseAccount, AccountDTO]
	SearchAccounts app.QueryHandler[SearchAccounts, []AccountDTO]

	RegisterMandate app.CommandHandler[RegisterMandate, MandateDTO]
	RevokeMandate   app.CommandHandler[RevokeMandate, MandateDTO]
	SearchMandates  app.QueryHandler[SearchMandates, []MandateDTO]

	Propose    app.CommandHandler[ProposeRemittance, RemittanceDTO]
	RemoveItem app.CommandHandler[RemoveItem, RemittanceDTO]
	Generate   app.CommandHandler[GenerateRemittance, RemittanceDTO]
	File       app.QueryHandler[GetFile, []byte]
	Settle     app.CommandHandler[SettleRemittance, RemittanceDTO]
	Return     app.CommandHandler[ReturnDebit, RemittanceDTO]
	Cancel     app.CommandHandler[CancelRemittance, RemittanceDTO]
	Get        app.QueryHandler[GetRemittance, RemittanceDTO]
	Search     app.QueryHandler[SearchRemittances, fw.Page[RemittanceDTO]]

	ProposeTransfers     app.CommandHandler[ProposeTransfers, TransferOrderDTO]
	RemoveTransfer       app.CommandHandler[RemoveTransfer, TransferOrderDTO]
	GenerateTransfers    app.CommandHandler[GenerateTransfers, TransferOrderDTO]
	TransferFile         app.QueryHandler[GetTransferFile, []byte]
	SettleTransfers      app.CommandHandler[SettleTransfers, TransferOrderDTO]
	RejectTransfer       app.CommandHandler[RejectTransfer, TransferOrderDTO]
	CancelTransfers      app.CommandHandler[CancelTransfers, TransferOrderDTO]
	GetTransferOrder     app.QueryHandler[GetTransferOrder, TransferOrderDTO]
	SearchTransferOrders app.QueryHandler[SearchTransferOrders, fw.Page[TransferOrderDTO]]
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

// check returns a uniform 404 outside the organization's scope and 403 when it is read-only.
func (s scope) check(kind string, id fmt.Stringer, org domain.OrganizationID, write bool) error {
	if !s.global && !slices.Contains(s.orgs, org) {
		return fw.NotFound(kind, id)
	}
	if write && !s.global && (s.ac == nil || !s.ac.CanWrite(org.UUID)) {
		return fmt.Errorf("%w: read-only in your organization scope", fw.ErrForbidden)
	}
	return nil
}

func within[T any](s scope, field spec.Field[T, domain.OrganizationID]) spec.Spec[T] {
	switch {
	case s.global:
		return spec.All[T]()
	case len(s.orgs) == 0:
		return spec.None[T]()
	}
	return field.In(s.orgs...)
}

type service struct {
	Deps
	accounts    *orchestration.Orchestrator[domain.AccountID, *domain.Account]
	mandates    *orchestration.Orchestrator[domain.MandateID, *domain.Mandate]
	remittances *orchestration.Orchestrator[domain.RemittanceID, *domain.Remittance]
	transfers   *orchestration.Orchestrator[domain.TransferOrderID, *domain.TransferOrder]
}

func parseID(v *fw.Validation, field, s string) fw.UUID {
	u, err := fw.ParseUUID(s)
	v.Require(err == nil && !u.IsZero(), field, "format", field+" must be an id")
	return u
}

func parseIBAN(v *fw.Validation, field, s string) vocab.IBAN {
	i, err := vocab.NewIBAN(s)
	v.Merge(field, err)
	return i
}

func dateText(d vocab.Date) string {
	if d.IsZero() {
		return ""
	}
	return d.String()
}

// NewService wires the use cases.
func NewService(d Deps) *Service {
	var opts []orchestration.Option
	if d.Recorder != nil {
		opts = append(opts, orchestration.WithOutbox(d.Recorder))
	}
	if d.Audit != nil {
		opts = append(opts, orchestration.WithAuditLog(d.Audit))
	}
	s := service{Deps: d,
		accounts:    orchestration.New[domain.AccountID, *domain.Account](d.Accounts, d.UoW, opts...),
		mandates:    orchestration.New[domain.MandateID, *domain.Mandate](d.Mandates, d.UoW, opts...),
		remittances: orchestration.New[domain.RemittanceID, *domain.Remittance](d.Remittances, d.UoW, opts...),
		transfers:   orchestration.New[domain.TransferOrderID, *domain.TransferOrder](d.Transfers, d.UoW, opts...),
	}
	svc := &Service{}
	s.accountUseCases(svc)
	s.remittanceUseCases(svc)
	s.transferUseCases(svc)
	return svc
}

func guard[In, Out any](p authz.Permission, fn func(context.Context, In) (Out, error), mw ...app.Middleware[In, Out]) app.Handler[In, Out] {
	return app.Chain[In, Out](app.HandlerFunc[In, Out](fn), append([]app.Middleware[In, Out]{pipeline.RequirePermission[In, Out](p)}, mw...)...)
}

func retry[In, Out any]() app.Middleware[In, Out] {
	return pipeline.RetryOnConflict[In, Out](5, 10*time.Millisecond)
}

// OpenAccount registers a bank account of an internal organization.
type OpenAccount struct {
	Owner          string     `json:"owner"`
	IBAN           string     `json:"iban"`
	BIC            string     `json:"bic,omitempty"`
	Alias          string     `json:"alias"`
	Collections    bool       `json:"collections,omitempty"`
	Payments       bool       `json:"payments,omitempty"`
	CreditorSuffix string     `json:"creditorSuffix,omitempty"`
	Opened         vocab.Date `json:"opened"`
}

// CloseAccount closes an account.
type CloseAccount struct {
	ID domain.AccountID `json:"-"`
	On vocab.Date       `json:"on"`
}

// SearchAccounts lists the accounts of an organization.
type SearchAccounts struct{ Owner string }

// AccountDTO is the transport form of an account.
type AccountDTO struct {
	ID             string `json:"id"`
	Owner          string `json:"owner"`
	IBAN           string `json:"iban"`
	BIC            string `json:"bic,omitempty"`
	Alias          string `json:"alias"`
	Currency       string `json:"currency"`
	Collections    bool   `json:"collections"`
	Payments       bool   `json:"payments"`
	CreditorSuffix string `json:"creditorSuffix"`
	Opened         string `json:"opened"`
	Closed         string `json:"closed,omitempty"`
}

func accountDTO(a *domain.Account) AccountDTO {
	s := a.State()
	return AccountDTO{ID: a.ID().String(), Owner: s.Owner.String(), IBAN: s.IBAN.String(), BIC: s.BIC, Alias: s.Alias, Currency: s.Currency.String(),
		Collections: s.Collections, Payments: s.Payments, CreditorSuffix: s.CreditorSuffix, Opened: s.Opened.String(), Closed: dateText(s.Closed)}
}

// RegisterMandate registers a SEPA mandate signed by a debtor.
type RegisterMandate struct {
	Creditor  string     `json:"creditor"`
	Debtor    string     `json:"debtor"`
	IBAN      string     `json:"iban"`
	Reference string     `json:"reference"`
	Scheme    string     `json:"scheme"`
	Signed    vocab.Date `json:"signed"`
}

// RevokeMandate revokes a mandate.
type RevokeMandate struct {
	ID domain.MandateID `json:"-"`
	On vocab.Date       `json:"on"`
}

// SearchMandates lists mandates of a creditor, optionally of a debtor.
type SearchMandates struct{ Creditor, Debtor string }

// MandateDTO is the transport form of a mandate.
type MandateDTO struct {
	ID        string `json:"id"`
	Creditor  string `json:"creditor"`
	Debtor    string `json:"debtor"`
	IBAN      string `json:"iban"`
	Reference string `json:"reference"`
	Scheme    string `json:"scheme"`
	Signed    string `json:"signed"`
	LastUsed  string `json:"lastUsed,omitempty"`
	Revoked   string `json:"revoked,omitempty"`
	Next      string `json:"next"` // FRST or RCUR
}

func mandateDTO(m *domain.Mandate) MandateDTO {
	s := m.State()
	return MandateDTO{ID: m.ID().String(), Creditor: s.Creditor.String(), Debtor: s.Debtor.String(), IBAN: s.IBAN.String(), Reference: s.Reference,
		Scheme: s.Scheme.String(), Signed: s.Signed.String(), LastUsed: dateText(s.LastUsed), Revoked: dateText(s.Revoked), Next: m.Sequence()}
}

func (s service) accountUseCases(svc *Service) {
	svc.OpenAccount = guard(PermAccountUpdate, func(ctx context.Context, c OpenAccount) (AccountDTO, error) {
		var v fw.Validation
		owner := domain.OrganizationID{UUID: parseID(&v, "owner", c.Owner)}
		iban := parseIBAN(&v, "iban", c.IBAN)
		if err := v.Err(); err != nil {
			return AccountDTO{}, err
		}
		if err := scopeOf(ctx).check("parties.party", owner, owner, true); err != nil {
			return AccountDTO{}, err
		}
		a, err := domain.ReconstituteAccount(domain.NewAccountID(), domain.AccountState{Owner: owner, IBAN: iban, BIC: c.BIC, Alias: c.Alias,
			Currency: vocab.MustCurrencyCode("EUR"), Collections: c.Collections, Payments: c.Payments, CreditorSuffix: c.CreditorSuffix, Opened: c.Opened})
		if err != nil {
			return AccountDTO{}, err
		}
		dup, err := s.Accounts.Exists(ctx, domain.AccFieldIBAN.Eq(iban.String()))
		if err != nil {
			return AccountDTO{}, err
		}
		if dup {
			return AccountDTO{}, fw.Violation("treasury.duplicate_iban", "the account is already registered")
		}
		if err := s.accounts.Create(ctx, a); err != nil {
			return AccountDTO{}, err
		}
		return accountDTO(a), nil
	}, pipeline.Transactional[OpenAccount, AccountDTO](s.UoW))

	svc.CloseAccount = guard(PermAccountUpdate, func(ctx context.Context, c CloseAccount) (AccountDTO, error) {
		sc := scopeOf(ctx)
		a, err := s.accounts.Update(ctx, c.ID, func(_ context.Context, a *domain.Account) error {
			if err := sc.check(domain.AccountKind, a.ID(), a.State().Owner, true); err != nil {
				return err
			}
			return a.Close(c.On)
		})
		if err != nil {
			return AccountDTO{}, err
		}
		return accountDTO(a), nil
	}, retry[CloseAccount, AccountDTO]())

	svc.SearchAccounts = guard(PermAccountRead, func(ctx context.Context, q SearchAccounts) ([]AccountDTO, error) {
		var v fw.Validation
		parts := []spec.Specification[*domain.Account]{within(scopeOf(ctx), domain.AccFieldOwner)}
		if q.Owner != "" {
			parts = append(parts, domain.AccFieldOwner.Eq(domain.OrganizationID{UUID: parseID(&v, "owner", q.Owner)}))
		}
		if err := v.Err(); err != nil {
			return nil, err
		}
		as, err := s.Accounts.Find(ctx, spec.And(parts...))
		if err != nil {
			return nil, err
		}
		out := []AccountDTO{}
		for _, a := range as {
			out = append(out, accountDTO(a))
		}
		return out, nil
	})

	svc.RegisterMandate = guard(PermMandateUpdate, func(ctx context.Context, c RegisterMandate) (MandateDTO, error) {
		var v fw.Validation
		creditor := domain.OrganizationID{UUID: parseID(&v, "creditor", c.Creditor)}
		debtor := domain.PartyID{UUID: parseID(&v, "debtor", c.Debtor)}
		iban := parseIBAN(&v, "iban", c.IBAN)
		scheme, ok := domain.ParseScheme(c.Scheme)
		v.Require(ok, "scheme", "enum", "CORE or B2B")
		if err := v.Err(); err != nil {
			return MandateDTO{}, err
		}
		if err := scopeOf(ctx).check("parties.party", creditor, creditor, true); err != nil {
			return MandateDTO{}, err
		}
		m, err := domain.ReconstituteMandate(domain.NewMandateID(), domain.MandateState{Creditor: creditor, Debtor: debtor, IBAN: iban,
			Reference: c.Reference, Scheme: scheme, Signed: c.Signed})
		if err != nil {
			return MandateDTO{}, err
		}
		dup, err := s.Mandates.Exists(ctx, domain.ManFieldCreditor.Eq(creditor).And(domain.ManFieldReference.Eq(m.State().Reference)))
		if err != nil {
			return MandateDTO{}, err
		}
		if dup {
			return MandateDTO{}, fw.Violation("treasury.duplicate_mandate", "the creditor already has a mandate with that reference")
		}
		if err := s.mandates.Create(ctx, m); err != nil {
			return MandateDTO{}, err
		}
		return mandateDTO(m), nil
	}, pipeline.Transactional[RegisterMandate, MandateDTO](s.UoW))

	svc.RevokeMandate = guard(PermMandateUpdate, func(ctx context.Context, c RevokeMandate) (MandateDTO, error) {
		sc := scopeOf(ctx)
		m, err := s.mandates.Update(ctx, c.ID, func(_ context.Context, m *domain.Mandate) error {
			if err := sc.check(domain.MandateKind, m.ID(), m.State().Creditor, true); err != nil {
				return err
			}
			return m.Revoke(c.On)
		})
		if err != nil {
			return MandateDTO{}, err
		}
		return mandateDTO(m), nil
	}, retry[RevokeMandate, MandateDTO]())

	svc.SearchMandates = guard(PermMandateRead, func(ctx context.Context, q SearchMandates) ([]MandateDTO, error) {
		var v fw.Validation
		parts := []spec.Specification[*domain.Mandate]{within(scopeOf(ctx), domain.ManFieldCreditor)}
		if q.Creditor != "" {
			parts = append(parts, domain.ManFieldCreditor.Eq(domain.OrganizationID{UUID: parseID(&v, "creditor", q.Creditor)}))
		}
		if q.Debtor != "" {
			parts = append(parts, domain.ManFieldDebtor.Eq(domain.PartyID{UUID: parseID(&v, "debtor", q.Debtor)}))
		}
		if err := v.Err(); err != nil {
			return nil, err
		}
		ms, err := s.Mandates.Find(ctx, spec.And(parts...), domain.ManFieldReference.Asc())
		if err != nil {
			return nil, err
		}
		out := []MandateDTO{}
		for _, m := range ms {
			out = append(out, mandateDTO(m))
		}
		return out, nil
	})
}

// Publications translates the domain events into the Published Language: one event per direct
// debit.
func Publications(r *messaging.Recorder) *messaging.Recorder {
	messaging.On(r, func(_ context.Context, e domain.RemittanceSettled) ([]app.IntegrationEvent, error) {
		out := make([]app.IntegrationEvent, 0, len(e.Items))
		for _, i := range e.Items {
			out = append(out, contracts.DirectDebitCollectedV1{RemittanceID: e.AggregateID, EndToEnd: i.EndToEnd, Creditor: e.Creditor,
				Debtor: i.Debtor.String(), InvoiceID: i.Invoice.String(), Installment: i.Installment, Amount: i.Amount.StringFixed(2), CollectedOn: e.Settled})
		}
		return out, nil
	})
	messaging.On(r, func(_ context.Context, e domain.DirectDebitReturned) ([]app.IntegrationEvent, error) {
		i := e.Item
		return []app.IntegrationEvent{contracts.DirectDebitReturnedV1{RemittanceID: e.AggregateID, EndToEnd: i.EndToEnd, Creditor: e.Creditor,
			Debtor: i.Debtor.String(), InvoiceID: i.Invoice.String(), Installment: i.Installment, Amount: i.Amount.StringFixed(2),
			ReturnedOn: i.Returned.String(), Reason: i.Reason}}, nil
	})
	transferPublications(r)
	return r
}
