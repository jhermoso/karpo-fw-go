// Package application holds the Inventory use cases (with permissions and company scope), the
// availability port and the translation to the Published Language.
package application

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/inventory/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/inventory/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/application/orchestration"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Permissions (the C# stock endpoints required authentication only, and anyone could adjust).
// Moving stock and correcting it with a count are separate permissions.
var (
	PermWarehouseRead   = authz.MustPermission("Inventory.Warehouse.Read")
	PermWarehouseUpdate = authz.MustPermission("Inventory.Warehouse.Update")
	PermStockRead       = authz.MustPermission("Inventory.Stock.Read")
	PermStockMove       = authz.MustPermission("Inventory.Stock.Move")
	PermStockAdjust     = authz.MustPermission("Inventory.Stock.Adjust")
)

// Deps are the ports the use cases need; Recorder and Audit are optional.
type Deps struct {
	Warehouses   domain.WarehouseRepository
	Levels       domain.LevelRepository
	Movements    domain.MovementRepository
	Reservations domain.ReservationRepository
	Catalog      domain.Catalog
	UoW          fw.UnitOfWork
	Recorder     app.EventRecorder
	Audit        app.AuditLog
}

// Service exposes the use cases.
type Service struct {
	CreateWarehouse  app.CommandHandler[CreateWarehouse, WarehouseDTO]
	CloseWarehouse   app.CommandHandler[CloseWarehouse, WarehouseDTO]
	SearchWarehouses app.QueryHandler[SearchWarehouses, []WarehouseDTO]

	Receive         app.CommandHandler[Receive, MovementDTO]
	Issue           app.CommandHandler[Issue, MovementDTO]
	Adjust          app.CommandHandler[Adjust, LevelDTO]
	Transfer        app.CommandHandler[Transfer, []MovementDTO]
	Reserve         app.CommandHandler[Reserve, ReservationDTO]
	Release         app.CommandHandler[Release, ReservationDTO]
	SetReorderPoint app.CommandHandler[SetReorderPoint, LevelDTO]

	Stock        app.QueryHandler[GetStock, []LevelDTO]
	Ledger       app.QueryHandler[GetLedger, fw.Page[MovementDTO]]
	Valuation    app.QueryHandler[GetValuation, ValuationDTO]
	Reservations app.QueryHandler[SearchReservations, []ReservationDTO]
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
	warehouses   *orchestration.Orchestrator[domain.WarehouseID, *domain.Warehouse]
	movements    *orchestration.Orchestrator[domain.MovementID, *domain.Movement]
	reservations *orchestration.Orchestrator[domain.ReservationID, *domain.Reservation]
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
		warehouses:   orchestration.New[domain.WarehouseID, *domain.Warehouse](d.Warehouses, d.UoW, opts...),
		movements:    orchestration.New[domain.MovementID, *domain.Movement](d.Movements, d.UoW, opts...),
		reservations: orchestration.New[domain.ReservationID, *domain.Reservation](d.Reservations, d.UoW, opts...),
	}
}

// NewService wires the use cases.
func NewService(d Deps) *Service {
	s := newService(d)
	svc := &Service{}
	s.warehouseUseCases(svc)
	s.stockUseCases(svc)
	s.queries(svc)
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

func today(d vocab.Date) vocab.Date {
	if d.IsZero() {
		return vocab.DateOf(fw.Now())
	}
	return d
}

func guard[In, Out any](p authz.Permission, fn func(context.Context, In) (Out, error), mw ...app.Middleware[In, Out]) app.Handler[In, Out] {
	return app.Chain[In, Out](app.HandlerFunc[In, Out](fn), append([]app.Middleware[In, Out]{pipeline.RequirePermission[In, Out](p)}, mw...)...)
}

// moving wraps a stock-changing use case: retried on a version conflict of its level, in one unit
// of work with its movement.
func moving[In, Out any](s service, p authz.Permission, fn func(context.Context, In) (Out, error)) app.Handler[In, Out] {
	return guard(p, fn, pipeline.RetryOnConflict[In, Out](5, 10*time.Millisecond), pipeline.Transactional[In, Out](s.UoW))
}

// Publications translates the domain events into the Published Language.
func Publications(r *messaging.Recorder) *messaging.Recorder {
	messaging.On(r, func(_ context.Context, e domain.StockMoved) ([]app.IntegrationEvent, error) {
		m := e.Snapshot
		return []app.IntegrationEvent{contracts.StockMovedV1{MovementID: e.AggregateID, Company: m.Company.String(), Warehouse: m.Warehouse.String(),
			Product: m.Product.String(), Kind: m.Kind.String(), Date: m.Date.String(), Quantity: m.Quantity.String(), UnitCost: m.UnitCost.StringFixed(4),
			Value: m.Value.StringFixed(2), Balance: m.Balance.String(), Lot: m.Lot, SourceType: m.Source.Type, SourceID: m.Source.ID}}, nil
	})
	messaging.On(r, func(_ context.Context, e domain.StockReserved) ([]app.IntegrationEvent, error) {
		return []app.IntegrationEvent{contracts.StockReservedV1{ReservationID: e.AggregateID, Company: e.Company, Warehouse: e.Warehouse, Product: e.Product,
			SourceType: e.SourceType, SourceID: e.SourceID, Added: e.Added, Held: e.Held}}, nil
	})
	return r
}

// AvailabilityPort implements contracts.Availability (it serves other contexts, not users).
type AvailabilityPort struct{ Levels domain.LevelRepository }

var _ contracts.Availability = AvailabilityPort{}

// Stock implements contracts.Availability.
func (a AvailabilityPort) Stock(ctx context.Context, company, product, warehouse string) (contracts.Stock, error) {
	var v fw.Validation
	parts := []spec.Specification[*domain.Level]{domain.LvlFieldCompany.Eq(domain.OrganizationID{UUID: parseID(&v, "company", company)}),
		domain.LvlFieldProduct.Eq(domain.ProductID{UUID: parseID(&v, "product", product)})}
	if warehouse != "" {
		parts = append(parts, domain.LvlFieldWarehouse.Eq(domain.WarehouseID{UUID: parseID(&v, "warehouse", warehouse)}))
	}
	if err := v.Err(); err != nil {
		return contracts.Stock{}, err
	}
	ls, err := a.Levels.Find(ctx, spec.And(parts...))
	if err != nil {
		return contracts.Stock{}, err
	}
	on, res := vocab.DecimalFromInt(0), vocab.DecimalFromInt(0)
	for _, l := range ls {
		on, res = on.Add(l.State().OnHand), res.Add(l.State().Reserved)
	}
	return contracts.Stock{OnHand: on.String(), Reserved: res.String(), Available: on.Sub(res).String()}, nil
}

// warehouse loads a warehouse of the caller's scope; for a movement it must be active.
func (s service) warehouse(ctx context.Context, id domain.WarehouseID, write bool) (*domain.Warehouse, error) {
	w, err := s.Warehouses.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := scopeOf(ctx).check(domain.WarehouseKind, id, w.State().Company, write); err != nil {
		return nil, err
	}
	if write && !w.State().Active {
		return nil, fw.Violation("inventory.warehouse_closed", "the warehouse is closed")
	}
	return w, nil
}

// item checks that a product is a stocked good of the company and that a tracked one comes with
// its lot or serial.
func (s service) item(ctx context.Context, company domain.OrganizationID, product domain.ProductID, lot string, needLot bool) (domain.Item, error) {
	items, err := s.Catalog.Items(ctx, []domain.ProductID{product})
	if err != nil {
		return domain.Item{}, err
	}
	it, ok := items[product]
	if !ok || it.Company != company {
		return domain.Item{}, fw.Violation("inventory.unknown_product", "the product is not in the catalog of the company")
	}
	if !it.Stocked {
		return domain.Item{}, fw.Violation("inventory.not_stocked", "the product is not stocked")
	}
	if needLot && it.Tracked && lot == "" {
		return domain.Item{}, fw.Violation("inventory.lot_required", "the product is tracked: its lot or serial is required")
	}
	return it, nil
}

// level returns the stock level of a product in a warehouse, new and empty when it has none.
func (s service) level(ctx context.Context, w *domain.Warehouse, product domain.ProductID) (*domain.Level, error) {
	ls, err := s.Levels.Find(ctx, spec.And(domain.LvlFieldWarehouse.Eq(w.ID()), domain.LvlFieldProduct.Eq(product)))
	if err != nil {
		return nil, err
	}
	if len(ls) > 0 {
		return ls[0], nil
	}
	return domain.OpenLevel(domain.NewLevelID(), w.State().Company, w.ID(), product)
}

// post saves a level and the movement that explains its change.
func (s service) post(ctx context.Context, l *domain.Level, seq int64, kind domain.MoveKind, date vocab.Date, q, cost vocab.Decimal, lot, note string,
	src domain.Source) (*domain.Movement, error) {
	if err := s.Levels.Save(ctx, l); err != nil {
		return nil, err
	}
	m, err := domain.Post(domain.NewMovementID(), l, seq, kind, date, q, cost, lot, note, src)
	if err != nil {
		return nil, err
	}
	if err := s.movements.Create(ctx, m); err != nil {
		return nil, err
	}
	return m, nil
}

// posted returns the movement a source already produced for a product in a warehouse, so a
// redelivered fact moves the stock once.
func (s service) posted(ctx context.Context, w domain.WarehouseID, product domain.ProductID, kind domain.MoveKind, src domain.Source) (*domain.Movement, error) {
	if src.Type == "" || src.ID == "" {
		return nil, nil
	}
	ms, err := s.Movements.Find(ctx, spec.And(domain.MovFieldWarehouse.Eq(w), domain.MovFieldProduct.Eq(product), domain.MovFieldKind.Eq(int(kind)),
		domain.MovFieldSrcType.Eq(src.Type), domain.MovFieldSrcID.Eq(src.ID)))
	if err != nil || len(ms) == 0 {
		return nil, err
	}
	return ms[0], nil
}
