// Package application contiene los casos de uso de Products (con permisos y ámbito de empresa), los
// puertos Catalog y Pricing que leen otros contextos y la traducción al Published Language.
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

// Permisos (los endpoints de producto, precio y stock del C# sólo pedían autenticación). El
// catálogo y los precios los mantienen personas distintas; los precios especiales de cada cliente,
// también aparte de las tarifas.
var (
	PermProductRead        = authz.MustPermission("Products.Product.Read")
	PermProductUpdate      = authz.MustPermission("Products.Product.Update")
	PermPriceListRead      = authz.MustPermission("Products.PriceList.Read")
	PermPriceListWrite     = authz.MustPermission("Products.PriceList.Update")
	PermSpecialPriceRead   = authz.MustPermission("Products.SpecialPrice.Read")
	PermSpecialPriceUpdate = authz.MustPermission("Products.SpecialPrice.Update")
)

// Deps son los puertos que necesitan los casos de uso; Recorder y Audit son opcionales.
type Deps struct {
	Products   domain.ProductRepository
	Categories domain.CategoryRepository
	PriceLists domain.PriceListRepository
	Specials   domain.SpecialPricesRepository
	UoW        fw.UnitOfWork
	Today      app.CompanyToday // opcional: sin él, hoy es el de UTC (docs/ARQUITECTURA.md, «Hoy es el de la empresa»)
	Recorder   app.EventRecorder
	Audit      app.AuditLog
}

// Service expone los casos de uso.
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
	SetDefaultList     app.CommandHandler[SetDefaultPriceList, PriceListDTO]
	GetPriceList       app.QueryHandler[GetPriceList, PriceListDTO]
	SearchPriceLists   app.QueryHandler[SearchPriceLists, []PriceListDTO]
	Quote              app.QueryHandler[GetQuote, contracts.Quote]

	SetSpecialPrice    app.CommandHandler[SetSpecialPrice, SpecialPricesDTO]
	RemoveSpecialPrice app.CommandHandler[RemoveSpecialPrice, SpecialPricesDTO]
	GetSpecialPrices   app.QueryHandler[GetSpecialPrices, SpecialPricesDTO]
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

// check devuelve un 404 uniforme fuera del ámbito de la empresa y 403 cuando es de sólo lectura.
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
	specials   *orchestration.Orchestrator[domain.SpecialPricesID, *domain.SpecialPrices]
}

// NewService conecta los casos de uso.
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
		specials:   orchestration.New[domain.SpecialPricesID, *domain.SpecialPrices](d.Specials, d.UoW, opts...),
	}
	svc := &Service{}
	s.productUseCases(svc)
	s.priceUseCases(svc)
	s.specialPriceUseCases(svc)
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

// Publications traduce los eventos de dominio al Published Language.
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

// CatalogPort implementa contracts.Catalog (sirve a otros contextos, no a usuarios).
type CatalogPort struct{ Repo domain.ProductRepository }

var _ contracts.Catalog = CatalogPort{}

// Products implementa contracts.Catalog.
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

// PricingPort implementa contracts.Pricing (sirve a otros contextos, no a usuarios).
type PricingPort struct {
	Products   domain.ProductRepository
	PriceLists domain.PriceListRepository
	Specials   domain.SpecialPricesRepository
}

var _ contracts.Pricing = PricingPort{}

// Quote implementa contracts.Pricing: arma la cadena de D-148 (precio especial del cliente, tarifa
// del cliente, tarifa general de la empresa) y domain.QuoteOf toma el primer nivel con precio o, si
// ninguno lo tiene, el precio base.
func (p PricingPort) Quote(ctx context.Context, r contracts.QuoteRequest) (contracts.Quote, error) {
	var v fw.Validation
	cid := domain.OrganizationID{UUID: parseID(&v, "company", r.Company)}
	pid := domain.ProductID{UUID: parseID(&v, "product", r.Product)}
	qty := parseDecimal(&v, "quantity", r.Quantity)
	date, err := vocab.ParseDate(r.On)
	v.Require(err == nil, "on", "format", "a date YYYY-MM-DD is required")
	v.Require(qty.IsPositive(), "quantity", "range", "a positive quantity")
	var lid domain.PriceListID
	if r.PriceList != "" {
		lid = domain.PriceListID{UUID: parseID(&v, "priceList", r.PriceList)}
	}
	var customer domain.PartyID
	if r.Customer != "" {
		customer = domain.PartyID{UUID: parseID(&v, "customer", r.Customer)}
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
	var levels []domain.Level
	if !customer.IsZero() && p.Specials != nil {
		sps, err := p.Specials.Find(ctx, spec.And(domain.SpFieldCompany.Eq(cid), domain.SpFieldCustomer.Eq(customer)))
		if err != nil {
			return contracts.Quote{}, err
		}
		if len(sps) > 0 {
			levels = append(levels, domain.Level{Source: domain.FromSpecial, Prices: sps[0]})
		}
	}
	if !lid.IsZero() {
		list, err := p.PriceLists.Get(ctx, lid)
		if err != nil {
			return contracts.Quote{}, err
		}
		if list.State().Company != cid {
			return contracts.Quote{}, fw.NotFound(domain.PriceListKind, lid)
		}
		levels = append(levels, domain.Level{Source: domain.FromCustomerList, Prices: list})
	}
	defaults, err := p.PriceLists.Find(ctx, domain.PlFieldDefaultFor.Eq(cid))
	if err != nil {
		return contracts.Quote{}, err
	}
	if len(defaults) > 0 {
		levels = append(levels, domain.Level{Source: domain.FromDefaultList, Prices: defaults[0]})
	}
	q := domain.QuoteOf(prod, qty, date, levels...)
	return contracts.Quote{UnitPrice: q.UnitPrice.StringFixed(4), Discount: q.Discount.StringFixed(2), Net: q.Net.StringFixed(4),
		Source: sources[q.Source]}, nil
}

// sources traduce el origen del dominio al del lenguaje publicado.
var sources = map[domain.PriceSource]string{
	domain.FromSpecial:      contracts.SourceSpecial,
	domain.FromCustomerList: contracts.SourceCustomerList,
	domain.FromDefaultList:  contracts.SourceDefaultList,
	domain.FromBase:         contracts.SourceBase,
}
