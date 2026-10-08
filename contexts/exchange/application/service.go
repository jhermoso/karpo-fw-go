// Package application holds the Exchange use cases (with permissions and company scope) and the
// translation to the Published Language.
package application

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/exchange/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/exchange/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/application/orchestration"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Deps are the ports the use cases need; Collaborators, Recorder and Audit are optional.
type Deps struct {
	Currencies    domain.CurrencyRepository
	Margins       domain.MarginRepository
	Settings      domain.SettingsRepository
	Reservations  domain.ReservationRepository
	Collaborators domain.Collaborators
	UoW           fw.UnitOfWork
	Recorder      app.EventRecorder
	Audit         app.AuditLog
}

// Service exposes the use cases.
type Service struct {
	SetCurrency    app.CommandHandler[SetCurrency, CurrencyDTO]
	SetRate        app.CommandHandler[SetRate, CurrencyDTO]
	ListCurrencies app.QueryHandler[ListCurrencies, []CurrencyDTO]
	SetMargin      app.CommandHandler[SetMargin, MarginDTO]
	ListMargins    app.QueryHandler[ListMargins, []MarginDTO]
	GetSettings    app.QueryHandler[GetSettings, SettingsDTO]
	SetSettings    app.CommandHandler[SetSettings, SettingsDTO]
	Quote          app.QueryHandler[GetQuote, QuoteDTO]

	Reserve   app.CommandHandler[Reserve, ReservationDTO]
	Advance   app.CommandHandler[AdvanceReservation, ReservationDTO]
	Cancel    app.CommandHandler[CancelReservation, ReservationDTO]
	ExpireDue app.CommandHandler[ExpireDue, ExpiredDTO]
	Get       app.QueryHandler[GetReservation, ReservationDTO]
	Search    app.QueryHandler[SearchReservations, fw.Page[ReservationDTO]]
	Dashboard app.QueryHandler[GetDashboard, DashboardDTO]
}

type service struct {
	Deps
	currencies   *orchestration.Orchestrator[domain.CurrencyID, *domain.Currency]
	margins      *orchestration.Orchestrator[domain.MarginID, *domain.Margin]
	settings     *orchestration.Orchestrator[domain.SettingsID, *domain.Settings]
	reservations *orchestration.Orchestrator[domain.ReservationID, *domain.Reservation]
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

func within[T any](s scope, field spec.Field[T, domain.OrganizationID]) spec.Spec[T] {
	switch {
	case s.global:
		return spec.All[T]()
	case len(s.orgs) == 0:
		return spec.None[T]()
	}
	return field.In(s.orgs...)
}

func parseID(v *fw.Validation, field, s string) fw.UUID {
	u, err := fw.ParseUUID(s)
	v.Require(err == nil && !u.IsZero(), field, "format", field+" must be an id")
	return u
}

func optionalID(v *fw.Validation, field, s string) fw.UUID {
	if s == "" {
		return fw.UUID{}
	}
	return parseID(v, field, s)
}

func parseDecimal(v *fw.Validation, field, s string) vocab.Decimal {
	if s == "" {
		return vocab.DecimalFromInt(0)
	}
	d, err := vocab.ParseDecimal(s)
	v.Require(err == nil, field, "format", field+" must be a decimal number")
	return d
}

func optID(u fw.UUID) string {
	if u.IsZero() {
		return ""
	}
	return u.String()
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func guard[In, Out any](p authz.Permission, fn func(context.Context, In) (Out, error), mw ...app.Middleware[In, Out]) app.Handler[In, Out] {
	return app.Chain[In, Out](app.HandlerFunc[In, Out](fn), append([]app.Middleware[In, Out]{pipeline.RequirePermission[In, Out](p)}, mw...)...)
}

func changing[In, Out any](uow fw.UnitOfWork, p authz.Permission, fn func(context.Context, In) (Out, error)) app.Handler[In, Out] {
	return guard(p, fn, pipeline.RetryOnConflict[In, Out](5, 10*time.Millisecond), pipeline.Transactional[In, Out](uow))
}

// company parses the company of a command and checks the caller's scope over it.
func company(ctx context.Context, v *fw.Validation, s string, write bool) (domain.OrganizationID, error) {
	org := domain.OrganizationID{UUID: parseID(v, "company", s)}
	if err := v.Err(); err != nil {
		return org, err
	}
	return org, scopeOf(ctx).check("parties.party", org, org, write)
}

// settingsOf returns the settings of a company: its own, or the defaults until it changes them.
func (s service) settingsOf(ctx context.Context, org domain.OrganizationID) (*domain.Settings, domain.SettingsState, error) {
	found, err := s.Settings.Find(ctx, domain.SetFieldCompany.Eq(org))
	if err != nil {
		return nil, domain.SettingsState{}, err
	}
	if len(found) == 0 {
		return nil, domain.DefaultSettings(org), nil
	}
	return found[0], found[0].State(), nil
}

func (s service) currency(ctx context.Context, org domain.OrganizationID, code string) (*domain.Currency, error) {
	found, err := s.Currencies.Find(ctx, spec.And(domain.CurFieldCompany.Eq(org), domain.CurFieldCode.Eq(domain.NormalizeCode(code))))
	if err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, fw.Violation("exchange.unknown_currency", "the company does not exchange "+domain.NormalizeCode(code))
	}
	return found[0], nil
}

func (s service) margin(ctx context.Context, org domain.OrganizationID, currency, segment string) (*domain.Margin, error) {
	found, err := s.Margins.Find(ctx, spec.And(domain.MrgFieldCompany.Eq(org), domain.MrgFieldCur.Eq(domain.NormalizeCode(currency)),
		domain.MrgFieldSegment.Eq(domain.NormalizeCode(segment))))
	if err != nil || len(found) == 0 {
		return nil, err
	}
	return found[0], nil
}

// quote prices an amount of a currency of a company for a segment.
func (s service) quote(ctx context.Context, org domain.OrganizationID, set domain.SettingsState, currency, segment string, amount vocab.Decimal) (domain.Quotation, error) {
	if segment == "" {
		segment = domain.DefaultSegment
	}
	c, err := s.currency(ctx, org, currency)
	if err != nil {
		return domain.Quotation{}, err
	}
	m, err := s.margin(ctx, org, currency, segment)
	if err != nil {
		return domain.Quotation{}, err
	}
	return domain.Quote(c, m, set, segment, amount)
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
		currencies:   orchestration.New[domain.CurrencyID, *domain.Currency](d.Currencies, d.UoW, opts...),
		margins:      orchestration.New[domain.MarginID, *domain.Margin](d.Margins, d.UoW, opts...),
		settings:     orchestration.New[domain.SettingsID, *domain.Settings](d.Settings, d.UoW, opts...),
		reservations: orchestration.New[domain.ReservationID, *domain.Reservation](d.Reservations, d.UoW, opts...)}
	svc := &Service{}
	s.wirePricing(svc)
	s.wireReservations(svc)
	return svc
}

// Publications translates the domain events into the Published Language.
func Publications(r *messaging.Recorder) *messaging.Recorder {
	messaging.On(r, func(_ context.Context, e domain.ReservationRegistered) ([]app.IntegrationEvent, error) {
		return []app.IntegrationEvent{contracts.ReservationRegisteredV1{ReservationID: e.AggregateID, Reference: e.Reference, Company: e.Company,
			Customer: e.Customer, Channel: e.Channel, Pickup: stamp(e.Pickup), ExpiresAt: stamp(e.ExpiresAt), Lines: e.Lines, Total: e.Total}}, nil
	})
	messaging.On(r, func(_ context.Context, e domain.ReservationStatusChanged) ([]app.IntegrationEvent, error) {
		return []app.IntegrationEvent{contracts.ReservationStatusChangedV1{ReservationID: e.AggregateID, Reference: e.Reference, Company: e.Company,
			Customer: e.Customer, From: e.From, To: e.To, At: stamp(e.At), Total: e.Total}}, nil
	})
	return r
}
