// Package application holds the Products use cases (with permissions and company scope), the
// Catalog and Pricing ports other contexts read, and the translation to the Published Language.
package application

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/products/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/products/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/application/orchestration"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Permissions (the C# product, price and stock endpoints required authentication only). The
// catalog and the prices are maintained by different people.
var (
	PermProductRead    = authz.MustPermission("Products.Product.Read")
	PermProductUpdate  = authz.MustPermission("Products.Product.Update")
	PermPriceListRead  = authz.MustPermission("Products.PriceList.Read")
	PermPriceListWrite = authz.MustPermission("Products.PriceList.Update")
)

// Deps are the ports the use cases need; Recorder and Audit are optional.
type Deps struct {
	Products   domain.ProductRepository
	Categories domain.CategoryRepository
	PriceLists domain.PriceListRepository
	UoW        fw.UnitOfWork
	Recorder   app.EventRecorder
	Audit      app.AuditLog
}

// Service exposes the use cases.
type Service struct {
	RegisterProduct    app.CommandHandler[RegisterProduct, ProductDTO]
	ChangeProduct      app.CommandHandler[ChangeProduct, ProductDTO]
	DiscontinueProduct app.CommandHandler[DiscontinueProduct, ProductDTO]
	GetProduct         app.QueryHandler[GetProduct, ProductDTO]
	SearchProducts     app.QueryHandler[SearchProducts, fw.Page[ProductDTO]]

	CreateCategory   app.CommandHandler[CreateCategory, CategoryDTO]
	RenameCategory   app.CommandHandler[RenameCategory, CategoryDTO]
	SearchCategories app.QueryHandler[SearchCategories, []CategoryDTO]

	CreatePriceList    app.CommandHandler[CreatePriceList, PriceListDTO]
	SetPrice           app.CommandHandler[SetPrice, PriceListDTO]
	RemovePrice        app.CommandHandler[RemovePrice, PriceListDTO]
	SetPriceListActive app.CommandHandler[SetPriceListActive, PriceListDTO]
	GetPriceList       app.QueryHandler[GetPriceList, PriceListDTO]
	SearchPriceLists   app.QueryHandler[SearchPriceLists, []PriceListDTO]
	Quote              app.QueryHandler[GetQuote, contracts.Quote]
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

type service struct {
	Deps
	products   *orchestration.Orchestrator[domain.ProductID, *domain.Product]
	categories *orchestration.Orchestrator[domain.CategoryID, *domain.Category]
	priceLists *orchestration.Orchestrator[domain.PriceListID, *domain.PriceList]
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
		products:   orchestration.New[domain.ProductID, *domain.Product](d.Products, d.UoW, opts...),
		categories: orchestration.New[domain.CategoryID, *domain.Category](d.Categories, d.UoW, opts...),
		priceLists: orchestration.New[domain.PriceListID, *domain.PriceList](d.PriceLists, d.UoW, opts...),
	}
	svc := &Service{}
	s.productUseCases(svc)
	s.priceUseCases(svc)
	return svc
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

func dateText(d vocab.Date) string {
	if d.IsZero() {
		return ""
	}
	return d.String()
}

func guard[In, Out any](p authz.Permission, fn func(context.Context, In) (Out, error), mw ...app.Middleware[In, Out]) app.Handler[In, Out] {
	return app.Chain[In, Out](app.HandlerFunc[In, Out](fn), append([]app.Middleware[In, Out]{pipeline.RequirePermission[In, Out](p)}, mw...)...)
}

func retry[In, Out any]() app.Middleware[In, Out] {
	return pipeline.RetryOnConflict[In, Out](5, 10*time.Millisecond)
}

// Publications translates the domain events into the Published Language.
func Publications(r *messaging.Recorder) *messaging.Recorder {
	messaging.On(r, func(_ context.Context, e domain.ProductRegistered) ([]app.IntegrationEvent, error) {
		return []app.IntegrationEvent{contracts.ProductRegisteredV1{ProductID: e.AggregateID, Company: e.Company, SKU: e.SKU, Name: e.Name, Kind: e.Kind}}, nil
	})
	messaging.On(r, func(_ context.Context, e domain.ProductDiscontinued) ([]app.IntegrationEvent, error) {
		return []app.IntegrationEvent{contracts.ProductDiscontinuedV1{ProductID: e.AggregateID, Company: e.Company, SKU: e.SKU, On: e.On}}, nil
	})
	return r
}

func productRef(p *domain.Product) contracts.ProductRef {
	s := p.State()
	return contracts.ProductRef{ID: p.ID().String(), Company: s.Company.String(), SKU: s.SKU, Name: s.Name, Kind: s.Kind.String(), UoM: s.UoM,
		TaxCode: s.TaxCode, ExpenseCategory: s.ExpenseCategory, ForSale: s.ForSale, ForPurchase: s.ForPurchase, Stocked: s.Stocked,
		Tracking: s.Tracking.String(), BlockedSales: s.BlockedSales, BlockedPurchase: s.BlockedPurchase, Discontinued: dateText(s.Discontinued),
		StandardCost: s.StandardCost.StringFixed(4)}
}

// CatalogPort implements contracts.Catalog (it serves other contexts, not users).
type CatalogPort struct{ Repo domain.ProductRepository }

var _ contracts.Catalog = CatalogPort{}

// Products implements contracts.Catalog.
func (c CatalogPort) Products(ctx context.Context, ids []string) (map[string]contracts.ProductRef, error) {
	if len(ids) > contracts.MaxBatch {
		return nil, fmt.Errorf("%w: at most %d ids per call", fw.ErrValidation, contracts.MaxBatch)
	}
	var keys []domain.ProductID
	for _, id := range ids {
		if u, err := fw.ParseUUID(id); err == nil && !slices.Contains(keys, domain.ProductID{UUID: u}) {
			keys = append(keys, domain.ProductID{UUID: u})
		}
	}
	out := map[string]contracts.ProductRef{}
	if len(keys) == 0 {
		return out, nil
	}
	ps, err := c.Repo.Find(ctx, domain.ProdFieldID.In(keys...))
	if err != nil {
		return nil, err
	}
	for _, p := range ps {
		out[p.ID().String()] = productRef(p)
	}
	return out, nil
}

// PricingPort implements contracts.Pricing (it serves other contexts, not users).
type PricingPort struct {
	Products   domain.ProductRepository
	PriceLists domain.PriceListRepository
}

var _ contracts.Pricing = PricingPort{}

// Quote implements contracts.Pricing.
func (p PricingPort) Quote(ctx context.Context, company, product, priceList, quantity, on string) (contracts.Quote, error) {
	var v fw.Validation
	cid := domain.OrganizationID{UUID: parseID(&v, "company", company)}
	pid := domain.ProductID{UUID: parseID(&v, "product", product)}
	qty := parseDecimal(&v, "quantity", quantity)
	date, err := vocab.ParseDate(on)
	v.Require(err == nil, "on", "format", "a date YYYY-MM-DD is required")
	v.Require(qty.IsPositive(), "quantity", "range", "a positive quantity")
	var lid domain.PriceListID
	if priceList != "" {
		lid = domain.PriceListID{UUID: parseID(&v, "priceList", priceList)}
	}
	if err := v.Err(); err != nil {
		return contracts.Quote{}, err
	}
	prod, err := p.Products.Get(ctx, pid)
	if err != nil {
		return contracts.Quote{}, err
	}
	if prod.State().Company != cid {
		return contracts.Quote{}, fw.NotFound(domain.ProductKind, pid)
	}
	var list *domain.PriceList
	if !lid.IsZero() {
		if list, err = p.PriceLists.Get(ctx, lid); err != nil {
			return contracts.Quote{}, err
		}
		if list.State().Company != cid {
			return contracts.Quote{}, fw.NotFound(domain.PriceListKind, lid)
		}
	}
	q := domain.QuoteOf(prod, list, qty, date)
	return contracts.Quote{UnitPrice: q.UnitPrice.StringFixed(4), Discount: q.Discount.StringFixed(2), Net: q.Net.StringFixed(4), Source: q.Source}, nil
}
