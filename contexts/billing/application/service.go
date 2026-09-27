// Package application holds the Billing use cases (with permissions and seller scope), the ports
// Billing consumes (tax engine, fiscal identities) and the translation to its Published Language.
package application

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/billing/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/billing/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/application/orchestration"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Permissions (the C# defined Invoicing.* codes but enforced none). Issuing is a permission of its
// own, apart from preparing.
var (
	PermInvoiceRead   = authz.MustPermission("Billing.Invoice.Read")
	PermInvoiceCreate = authz.MustPermission("Billing.Invoice.Create")
	PermInvoiceUpdate = authz.MustPermission("Billing.Invoice.Update")
	PermInvoiceIssue  = authz.MustPermission("Billing.Invoice.Issue")
	PermSeriesRead    = authz.MustPermission("Billing.Series.Read")
	PermSeriesUpdate  = authz.MustPermission("Billing.Series.Update")
)

// Deps are the ports the use cases need; Recorder and Audit are optional.
type Deps struct {
	Series     domain.SeriesRepository
	Invoices   domain.InvoiceRepository
	Taxes      domain.Taxes
	Identities domain.Identities
	UoW        fw.UnitOfWork
	Recorder   app.EventRecorder
	Audit      app.AuditLog
}

// Service exposes the use cases.
type Service struct {
	OpenSeries   app.CommandHandler[OpenSeries, SeriesDTO]
	CloseSeries  app.CommandHandler[CloseSeries, SeriesDTO]
	SearchSeries app.QueryHandler[SearchSeries, []SeriesDTO]

	DraftInvoice   app.CommandHandler[DraftInvoice, InvoiceDTO]
	AddLine        app.CommandHandler[AddLine, InvoiceDTO]
	RemoveLine     app.CommandHandler[RemoveLine, InvoiceDTO]
	SetDetails     app.CommandHandler[SetDetails, InvoiceDTO]
	PreviewTaxes   app.QueryHandler[PreviewTaxes, BreakdownDTO]
	Issue          app.CommandHandler[IssueInvoice, InvoiceDTO]
	Discard        app.CommandHandler[DiscardInvoice, struct{}]
	GetInvoice     app.QueryHandler[GetInvoice, InvoiceDTO]
	SearchInvoices app.QueryHandler[SearchInvoices, fw.Page[InvoiceDTO]]
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

// check returns a uniform 404 outside the seller's scope and 403 when it is read-only.
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
	series   *orchestration.Orchestrator[domain.SeriesID, *domain.Series]
	invoices *orchestration.Orchestrator[domain.InvoiceID, *domain.Invoice]
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

func optMoney(d vocab.Decimal) string {
	if d.IsZero() {
		return ""
	}
	return d.StringFixed(2)
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
		series:   orchestration.New[domain.SeriesID, *domain.Series](d.Series, d.UoW, opts...),
		invoices: orchestration.New[domain.InvoiceID, *domain.Invoice](d.Invoices, d.UoW, opts...),
	}
	svc := &Service{}
	s.seriesUseCases(svc)
	s.invoiceUseCases(svc)
	return svc
}

func guard[In, Out any](p authz.Permission, fn func(context.Context, In) (Out, error), mw ...app.Middleware[In, Out]) app.Handler[In, Out] {
	return app.Chain[In, Out](app.HandlerFunc[In, Out](fn), append([]app.Middleware[In, Out]{pipeline.RequirePermission[In, Out](p)}, mw...)...)
}

func retry[In, Out any]() app.Middleware[In, Out] {
	return pipeline.RetryOnConflict[In, Out](5, 10*time.Millisecond)
}

// OpenSeries opens a numbering series of a seller for a year.
type OpenSeries struct {
	Seller     string `json:"seller"`
	Code       string `json:"code"`
	Year       int    `json:"year"`
	Corrective bool   `json:"corrective,omitempty"`
}

// CloseSeries closes a series.
type CloseSeries struct {
	ID domain.SeriesID `json:"-"`
}

// SearchSeries lists the series of a seller.
type SearchSeries struct {
	Seller string
	Year   int
}

// SeriesDTO is the transport form of a series.
type SeriesDTO struct {
	ID         string `json:"id"`
	Seller     string `json:"seller"`
	Code       string `json:"code"`
	Year       int    `json:"year"`
	Last       int64  `json:"last"`
	Corrective bool   `json:"corrective"`
	Active     bool   `json:"active"`
}

func seriesDTO(s *domain.Series) SeriesDTO {
	st := s.State()
	return SeriesDTO{ID: s.ID().String(), Seller: st.Seller.String(), Code: st.Code, Year: st.Year, Last: st.Last, Corrective: st.Corrective, Active: st.Active}
}

func (s service) seriesUseCases(svc *Service) {
	svc.OpenSeries = guard(PermSeriesUpdate, func(ctx context.Context, c OpenSeries) (SeriesDTO, error) {
		var v fw.Validation
		seller := domain.OrganizationID{UUID: parseID(&v, "seller", c.Seller)}
		if err := v.Err(); err != nil {
			return SeriesDTO{}, err
		}
		if err := scopeOf(ctx).check("parties.party", seller, seller, true); err != nil {
			return SeriesDTO{}, err
		}
		sr, err := domain.OpenSeries(domain.NewSeriesID(), domain.SeriesState{Seller: seller, Code: c.Code, Year: c.Year, Corrective: c.Corrective})
		if err != nil {
			return SeriesDTO{}, err
		}
		dup, err := s.Series.Exists(ctx, spec.And(domain.SerFieldSeller.Eq(seller), domain.SerFieldCode.Eq(sr.State().Code), domain.SerFieldYear.Eq(c.Year)))
		if err != nil {
			return SeriesDTO{}, err
		}
		if dup {
			return SeriesDTO{}, fw.Violation("billing.duplicate_series", "the seller already has that series for the year")
		}
		if err := s.series.Create(ctx, sr); err != nil {
			return SeriesDTO{}, err
		}
		return seriesDTO(sr), nil
	}, pipeline.Transactional[OpenSeries, SeriesDTO](s.UoW))

	svc.CloseSeries = guard(PermSeriesUpdate, func(ctx context.Context, c CloseSeries) (SeriesDTO, error) {
		sc := scopeOf(ctx)
		sr, err := s.series.Update(ctx, c.ID, func(_ context.Context, sr *domain.Series) error {
			if err := sc.check(domain.SeriesKind, sr.ID(), sr.State().Seller, true); err != nil {
				return err
			}
			sr.Close()
			return nil
		})
		if err != nil {
			return SeriesDTO{}, err
		}
		return seriesDTO(sr), nil
	}, retry[CloseSeries, SeriesDTO]())

	svc.SearchSeries = guard(PermSeriesRead, func(ctx context.Context, q SearchSeries) ([]SeriesDTO, error) {
		var v fw.Validation
		parts := []spec.Specification[*domain.Series]{within(scopeOf(ctx), domain.SerFieldSeller)}
		if q.Seller != "" {
			parts = append(parts, domain.SerFieldSeller.Eq(domain.OrganizationID{UUID: parseID(&v, "seller", q.Seller)}))
		}
		if q.Year != 0 {
			parts = append(parts, domain.SerFieldYear.Eq(q.Year))
		}
		if err := v.Err(); err != nil {
			return nil, err
		}
		ss, err := s.Series.Find(ctx, spec.And(parts...), domain.SerFieldCode.Asc())
		if err != nil {
			return nil, err
		}
		out := []SeriesDTO{}
		for _, sr := range ss {
			out = append(out, seriesDTO(sr))
		}
		return out, nil
	})
}

// Publications translates the domain events into the Published Language.
func Publications(r *messaging.Recorder) *messaging.Recorder {
	messaging.On(r, func(_ context.Context, e domain.InvoiceIssued) ([]app.IntegrationEvent, error) {
		s := e.Invoice
		out := contracts.InvoiceIssuedV1{InvoiceID: e.AggregateID, Number: s.Number, Kind: s.Kind.String(), Reason: s.Reason,
			Seller: s.Seller.String(), SellerNIF: s.SellerIdentity.NIF, Customer: s.Customer.String(), CustomerNIF: s.CustomerIdentity.NIF,
			CustomerName: s.CustomerIdentity.Name, IssueDate: s.IssueDate.String(), OperationDate: dateText(s.OperationDate),
			DueDate: dateText(s.DueDate), Currency: s.Currency.String(), Country: s.Taxes.Country, Net: money(s.Taxes.Net),
			Tax: money(s.Taxes.Tax), Surcharge: money(s.Taxes.Surcharge), Total: money(s.Taxes.Total()), Taxes: []contracts.TaxLineV1{}}
		if !s.Corrects.IsZero() {
			out.Corrects = s.Corrects.String()
		}
		for _, l := range s.Taxes.Lines {
			out.Taxes = append(out.Taxes, contracts.TaxLineV1{TaxType: l.TaxType, TaxCode: l.TaxCode, Treatment: l.Treatment,
				TreatmentKind: l.TreatmentKind, Rate: money(l.Rate), Base: money(l.Base), Amount: money(l.Amount),
				SurchargeRate: optMoney(l.SurchargeRate), SurchargeAmount: optMoney(l.SurchargeAmount)})
		}
		return []app.IntegrationEvent{out}, nil
	})
	return r
}

func upper(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }
