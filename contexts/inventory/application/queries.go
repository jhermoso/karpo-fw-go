package application

import (
	"context"
	"slices"
	"strings"

	"github.com/jhermoso/karpo-fw-go/contexts/inventory/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// GetStock lists the stock levels of a company, optionally of a warehouse or a product, or only
// those at or under their reorder point.
type GetStock struct {
	Company, Warehouse, Product string
	BelowReorder                bool
}

// GetLedger pages the movements of a company (the stock card of a product in a warehouse when
// both are given), in posting order.
type GetLedger struct {
	Company, Warehouse, Product, From, To string
	Page, Size                            int
}

// GetValuation values the stock of a company (optionally of a warehouse) at its average cost.
type GetValuation struct{ Company, Warehouse string }

// SearchReservations lists the reservations of a company.
type SearchReservations struct {
	Company  string
	OpenOnly bool
}

// ValuationLine is the value of a product across the warehouses asked for.
type ValuationLine struct {
	Product string `json:"product"`
	SKU     string `json:"sku,omitempty"`
	Name    string `json:"name,omitempty"`
	OnHand  string `json:"onHand"`
	Value   string `json:"value"`
}

// ValuationDTO is the value of the stock.
type ValuationDTO struct {
	Total string          `json:"total"`
	Lines []ValuationLine `json:"lines"`
}

// levels loads the levels of a company the caller may read, with the names of their products.
func (s service) levels(ctx context.Context, company, warehouse, product string) ([]*domain.Level, map[domain.ProductID]domain.Item, error) {
	var v fw.Validation
	cid := domain.OrganizationID{UUID: parseID(&v, "company", company)}
	parts := []spec.Specification[*domain.Level]{domain.LvlFieldCompany.Eq(cid)}
	if warehouse != "" {
		parts = append(parts, domain.LvlFieldWarehouse.Eq(domain.WarehouseID{UUID: parseID(&v, "warehouse", warehouse)}))
	}
	if product != "" {
		parts = append(parts, domain.LvlFieldProduct.Eq(domain.ProductID{UUID: parseID(&v, "product", product)}))
	}
	if err := v.Err(); err != nil {
		return nil, nil, err
	}
	if err := scopeOf(ctx).check("parties.party", cid, cid, false); err != nil {
		return nil, nil, err
	}
	ls, err := s.Levels.Find(ctx, spec.And(parts...))
	if err != nil {
		return nil, nil, err
	}
	var ids []domain.ProductID
	for _, l := range ls {
		if !slices.Contains(ids, l.State().Product) {
			ids = append(ids, l.State().Product)
		}
	}
	items := map[domain.ProductID]domain.Item{}
	if len(ids) > 0 {
		if items, err = s.Catalog.Items(ctx, ids); err != nil {
			return nil, nil, err
		}
	}
	return ls, items, nil
}

func (s service) queries(svc *Service) {
	svc.Stock = guard(PermStockRead, func(ctx context.Context, q GetStock) ([]LevelDTO, error) {
		ls, items, err := s.levels(ctx, q.Company, q.Warehouse, q.Product)
		if err != nil {
			return nil, err
		}
		out := []LevelDTO{}
		for _, l := range ls {
			if q.BelowReorder && !l.BelowReorder() {
				continue
			}
			out = append(out, levelDTO(l, items[l.State().Product]))
		}
		slices.SortFunc(out, func(a, b LevelDTO) int {
			if c := strings.Compare(a.SKU, b.SKU); c != 0 {
				return c
			}
			return strings.Compare(a.Warehouse, b.Warehouse)
		})
		return out, nil
	})

	svc.Ledger = guard(PermStockRead, func(ctx context.Context, q GetLedger) (fw.Page[MovementDTO], error) {
		var v fw.Validation
		cid := domain.OrganizationID{UUID: parseID(&v, "company", q.Company)}
		parts := []spec.Specification[*domain.Movement]{domain.MovFieldCompany.Eq(cid)}
		if q.Warehouse != "" {
			parts = append(parts, domain.MovFieldWarehouse.Eq(domain.WarehouseID{UUID: parseID(&v, "warehouse", q.Warehouse)}))
		}
		if q.Product != "" {
			parts = append(parts, domain.MovFieldProduct.Eq(domain.ProductID{UUID: parseID(&v, "product", q.Product)}))
		}
		if q.From != "" {
			d, err := vocab.ParseDate(q.From)
			v.Require(err == nil, "from", "format", "a date YYYY-MM-DD is required")
			parts = append(parts, domain.MovFieldDate.Ge(d))
		}
		if q.To != "" {
			d, err := vocab.ParseDate(q.To)
			v.Require(err == nil, "to", "format", "a date YYYY-MM-DD is required")
			parts = append(parts, domain.MovFieldDate.Le(d))
		}
		if err := v.Err(); err != nil {
			return fw.Page[MovementDTO]{}, err
		}
		if err := scopeOf(ctx).check("parties.party", cid, cid, false); err != nil {
			return fw.Page[MovementDTO]{}, err
		}
		// Within a level the sequence is the posting order; across levels, the date comes first.
		order := []spec.Order[*domain.Movement]{domain.MovFieldDate.Asc(), domain.MovFieldSeq.Asc()}
		if q.Warehouse != "" && q.Product != "" {
			order = order[1:]
		}
		page, err := s.Movements.FindPage(ctx, spec.And(parts...), fw.NewPageRequest(q.Page, q.Size, order...))
		if err != nil {
			return fw.Page[MovementDTO]{}, err
		}
		return fw.MapPage(page, movementDTO), nil
	})

	svc.Valuation = guard(PermStockRead, func(ctx context.Context, q GetValuation) (ValuationDTO, error) {
		ls, items, err := s.levels(ctx, q.Company, q.Warehouse, "")
		if err != nil {
			return ValuationDTO{}, err
		}
		type sum struct{ qty, value vocab.Decimal }
		by := map[domain.ProductID]*sum{}
		total := vocab.DecimalFromInt(0)
		for _, l := range ls {
			st := l.State()
			if st.OnHand.IsZero() {
				continue
			}
			x, ok := by[st.Product]
			if !ok {
				x = &sum{vocab.DecimalFromInt(0), vocab.DecimalFromInt(0)}
				by[st.Product] = x
			}
			x.qty, x.value = x.qty.Add(st.OnHand), x.value.Add(l.Value())
			total = total.Add(l.Value())
		}
		out := ValuationDTO{Total: total.StringFixed(2), Lines: []ValuationLine{}}
		for p, x := range by {
			out.Lines = append(out.Lines, ValuationLine{Product: p.String(), SKU: items[p].SKU, Name: items[p].Name, OnHand: x.qty.String(),
				Value: x.value.StringFixed(2)})
		}
		slices.SortFunc(out.Lines, func(a, b ValuationLine) int { return strings.Compare(a.SKU+a.Product, b.SKU+b.Product) })
		return out, nil
	})

	svc.Reservations = guard(PermStockRead, func(ctx context.Context, q SearchReservations) ([]ReservationDTO, error) {
		var v fw.Validation
		cid := domain.OrganizationID{UUID: parseID(&v, "company", q.Company)}
		if err := v.Err(); err != nil {
			return nil, err
		}
		if err := scopeOf(ctx).check("parties.party", cid, cid, false); err != nil {
			return nil, err
		}
		parts := []spec.Specification[*domain.Reservation]{domain.ResFieldCompany.Eq(cid)}
		if q.OpenOnly {
			parts = append(parts, domain.ResFieldClosed.Eq(false))
		}
		rs, err := s.Reservations.Find(ctx, spec.And(parts...))
		if err != nil {
			return nil, err
		}
		out := []ReservationDTO{}
		for _, r := range rs {
			out = append(out, reservationDTO(r))
		}
		slices.SortFunc(out, func(a, b ReservationDTO) int {
			return strings.Compare(a.SourceType+a.SourceID+a.ID, b.SourceType+b.SourceID+b.ID)
		})
		return out, nil
	})
}
