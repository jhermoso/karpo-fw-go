package application

import (
	"context"

	"github.com/jhermoso/karpo-fw-go/contexts/products/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/products/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// CreatePriceList creates a price list of a company.
type CreatePriceList struct {
	Company string `json:"company"`
	Code    string `json:"code"`
	Name    string `json:"name"`
}

// SetPrice adds the price of a product to a list.
type SetPrice struct {
	ID          domain.PriceListID `json:"-"`
	Product     string             `json:"product"`
	MinQuantity string             `json:"minQuantity,omitempty"`
	UnitPrice   string             `json:"unitPrice"`
	Discount    string             `json:"discount,omitempty"`
	From        vocab.Date         `json:"from"`
	To          vocab.Date         `json:"to,omitzero"`
}

// RemovePrice removes a price from a list.
type RemovePrice struct {
	ID          domain.PriceListID `json:"-"`
	Product     string             `json:"product"`
	MinQuantity string             `json:"minQuantity,omitempty"`
	From        vocab.Date         `json:"from"`
}

// SetPriceListActive enables or retires a list.
type SetPriceListActive struct {
	ID     domain.PriceListID `json:"-"`
	Active bool               `json:"active"`
}

// GetPriceList loads a price list with its lines.
type GetPriceList struct{ ID domain.PriceListID }

// SearchPriceLists lists the price lists of a company (without lines).
type SearchPriceLists struct{ Company string }

// GetQuote prices a product for a quantity on a date, with an optional price list.
type GetQuote struct{ Company, Product, PriceList, Quantity, On string }

// PriceLineDTO is the transport form of a price.
type PriceLineDTO struct {
	Product     string `json:"product"`
	MinQuantity string `json:"minQuantity"`
	UnitPrice   string `json:"unitPrice"`
	Discount    string `json:"discount"`
	From        string `json:"from"`
	To          string `json:"to,omitempty"`
}

// PriceListDTO is the transport form of a price list.
type PriceListDTO struct {
	ID       string         `json:"id"`
	Company  string         `json:"company"`
	Code     string         `json:"code"`
	Name     string         `json:"name"`
	Currency string         `json:"currency"`
	Active   bool           `json:"active"`
	Lines    []PriceLineDTO `json:"lines"`
	Version  int64          `json:"version"`
}

func priceListDTO(p *domain.PriceList, lines bool) PriceListDTO {
	s := p.State()
	d := PriceListDTO{ID: p.ID().String(), Company: s.Company.String(), Code: s.Code, Name: s.Name, Currency: s.Currency.String(), Active: s.Active,
		Lines: []PriceLineDTO{}, Version: p.Version()}
	if lines {
		for _, l := range s.Lines {
			d.Lines = append(d.Lines, PriceLineDTO{Product: l.Product.String(), MinQuantity: l.MinQuantity.String(), UnitPrice: l.UnitPrice.StringFixed(4),
				Discount: l.Discount.StringFixed(2), From: l.From.String(), To: dateText(l.To)})
		}
	}
	return d
}

func (s service) priceUseCases(svc *Service) {
	svc.CreatePriceList = guard(PermPriceListWrite, func(ctx context.Context, c CreatePriceList) (PriceListDTO, error) {
		var v fw.Validation
		company := domain.OrganizationID{UUID: parseID(&v, "company", c.Company)}
		if err := v.Err(); err != nil {
			return PriceListDTO{}, err
		}
		if err := scopeOf(ctx).check("parties.party", company, company, true); err != nil {
			return PriceListDTO{}, err
		}
		pl, err := domain.ReconstitutePriceList(domain.NewPriceListID(), domain.PriceListState{Company: company, Code: c.Code, Name: c.Name,
			Currency: vocab.MustCurrencyCode("EUR"), Active: true})
		if err != nil {
			return PriceListDTO{}, err
		}
		dup, err := s.PriceLists.Exists(ctx, spec.And(domain.PlFieldCompany.Eq(company), domain.PlFieldCode.Eq(pl.State().Code)))
		if err != nil {
			return PriceListDTO{}, err
		}
		if dup {
			return PriceListDTO{}, fw.Violation("products.duplicate_price_list", "the company already has that price list code")
		}
		if err := s.priceLists.Create(ctx, pl); err != nil {
			return PriceListDTO{}, err
		}
		return priceListDTO(pl, true), nil
	}, pipeline.Transactional[CreatePriceList, PriceListDTO](s.UoW))

	update := func(ctx context.Context, id domain.PriceListID, fn func(context.Context, *domain.PriceList) error) (PriceListDTO, error) {
		sc := scopeOf(ctx)
		pl, err := s.priceLists.Update(ctx, id, func(ctx context.Context, pl *domain.PriceList) error {
			if err := sc.check(domain.PriceListKind, pl.ID(), pl.State().Company, true); err != nil {
				return err
			}
			return fn(ctx, pl)
		})
		if err != nil {
			return PriceListDTO{}, err
		}
		return priceListDTO(pl, true), nil
	}
	svc.SetPrice = guard(PermPriceListWrite, func(ctx context.Context, c SetPrice) (PriceListDTO, error) {
		var v fw.Validation
		line := domain.PriceLine{Product: domain.ProductID{UUID: parseID(&v, "product", c.Product)}, MinQuantity: parseDecimal(&v, "minQuantity", c.MinQuantity),
			UnitPrice: parseDecimal(&v, "unitPrice", c.UnitPrice), Discount: parseDecimal(&v, "discount", c.Discount), From: c.From, To: c.To}
		if err := v.Err(); err != nil {
			return PriceListDTO{}, err
		}
		return update(ctx, c.ID, func(ctx context.Context, pl *domain.PriceList) error {
			p, err := s.Products.Get(ctx, line.Product)
			if err != nil {
				return err
			}
			if p.State().Company != pl.State().Company {
				return fw.NotFound(domain.ProductKind, line.Product)
			}
			return pl.SetPrice(line)
		})
	}, retry[SetPrice, PriceListDTO](), pipeline.Transactional[SetPrice, PriceListDTO](s.UoW))
	svc.RemovePrice = guard(PermPriceListWrite, func(ctx context.Context, c RemovePrice) (PriceListDTO, error) {
		var v fw.Validation
		product := domain.ProductID{UUID: parseID(&v, "product", c.Product)}
		min := parseDecimal(&v, "minQuantity", c.MinQuantity)
		if err := v.Err(); err != nil {
			return PriceListDTO{}, err
		}
		return update(ctx, c.ID, func(_ context.Context, pl *domain.PriceList) error { return pl.RemovePrice(product, min, c.From) })
	}, retry[RemovePrice, PriceListDTO]())
	svc.SetPriceListActive = guard(PermPriceListWrite, func(ctx context.Context, c SetPriceListActive) (PriceListDTO, error) {
		return update(ctx, c.ID, func(_ context.Context, pl *domain.PriceList) error { pl.SetActive(c.Active); return nil })
	}, retry[SetPriceListActive, PriceListDTO]())

	svc.GetPriceList = guard(PermPriceListRead, func(ctx context.Context, q GetPriceList) (PriceListDTO, error) {
		pl, err := s.PriceLists.Get(ctx, q.ID)
		if err != nil {
			return PriceListDTO{}, err
		}
		if err := scopeOf(ctx).check(domain.PriceListKind, pl.ID(), pl.State().Company, false); err != nil {
			return PriceListDTO{}, err
		}
		return priceListDTO(pl, true), nil
	})
	svc.SearchPriceLists = guard(PermPriceListRead, func(ctx context.Context, q SearchPriceLists) ([]PriceListDTO, error) {
		var v fw.Validation
		company := domain.OrganizationID{UUID: parseID(&v, "company", q.Company)}
		if err := v.Err(); err != nil {
			return nil, err
		}
		if err := scopeOf(ctx).check("parties.party", company, company, false); err != nil {
			return nil, err
		}
		pls, err := s.PriceLists.Find(ctx, domain.PlFieldCompany.Eq(company), domain.PlFieldCode.Asc())
		if err != nil {
			return nil, err
		}
		out := []PriceListDTO{}
		for _, pl := range pls {
			out = append(out, priceListDTO(pl, false))
		}
		return out, nil
	})
	svc.Quote = guard(PermPriceListRead, func(ctx context.Context, q GetQuote) (contracts.Quote, error) {
		var v fw.Validation
		company := domain.OrganizationID{UUID: parseID(&v, "company", q.Company)}
		if err := v.Err(); err != nil {
			return contracts.Quote{}, err
		}
		if err := scopeOf(ctx).check("parties.party", company, company, false); err != nil {
			return contracts.Quote{}, err
		}
		return PricingPort{Products: s.Products, PriceLists: s.PriceLists}.Quote(ctx, q.Company, q.Product, q.PriceList, q.Quantity, q.On)
	})
}
