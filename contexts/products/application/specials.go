package application

import (
	"context"

	"github.com/jhermoso/karpo-fw-go/contexts/products/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// SetSpecialPrice da a un cliente un precio especial de un producto (D-148). El cliente es una party
// de Parties; Products no comprueba su rol, como no comprueba el del proveedor de un producto.
type SetSpecialPrice struct {
	Company     string     `json:"company"`
	Customer    string     `json:"customer"`
	Product     string     `json:"product"`
	MinQuantity string     `json:"minQuantity,omitempty"`
	UnitPrice   string     `json:"unitPrice"`
	Discount    string     `json:"discount,omitempty"`
	From        vocab.Date `json:"from"`
	To          vocab.Date `json:"to,omitzero"`
}

// RemoveSpecialPrice quita un precio especial.
type RemoveSpecialPrice struct {
	Company     string     `json:"company"`
	Customer    string     `json:"customer"`
	Product     string     `json:"product"`
	MinQuantity string     `json:"minQuantity,omitempty"`
	From        vocab.Date `json:"from"`
}

// GetSpecialPrices carga los precios especiales de un cliente en una empresa (sin ninguno, la lista
// vacía).
type GetSpecialPrices struct{ Company, Customer string }

// SpecialPricesDTO es la forma de transporte de los precios especiales de un cliente.
type SpecialPricesDTO struct {
	ID       string         `json:"id,omitempty"`
	Company  string         `json:"company"`
	Customer string         `json:"customer"`
	Currency string         `json:"currency"`
	Lines    []PriceLineDTO `json:"lines"`
	Version  int64          `json:"version"`
}

func specialPricesDTO(p *domain.SpecialPrices) SpecialPricesDTO {
	s := p.State()
	return SpecialPricesDTO{ID: p.ID().String(), Company: s.Company.String(), Customer: s.Customer.String(), Currency: s.Currency.String(),
		Lines: priceLineDTOs(s.Lines), Version: p.Version()}
}

// specialsOf devuelve los precios especiales de un cliente en una empresa, o nil si no tiene.
func (s service) specialsOf(ctx context.Context, company domain.OrganizationID, customer domain.PartyID) (*domain.SpecialPrices, error) {
	sps, err := s.Specials.Find(ctx, spec.And(domain.SpFieldCompany.Eq(company), domain.SpFieldCustomer.Eq(customer)))
	if err != nil || len(sps) == 0 {
		return nil, err
	}
	return sps[0], nil
}

func parseOwner(v *fw.Validation, company, customer string) (domain.OrganizationID, domain.PartyID) {
	return domain.OrganizationID{UUID: parseID(v, "company", company)}, domain.PartyID{UUID: parseID(v, "customer", customer)}
}

func (s service) specialPriceUseCases(svc *Service) {
	// change aplica fn a los precios especiales del cliente, que se crean con el primero.
	change := func(ctx context.Context, company domain.OrganizationID, customer domain.PartyID, create bool,
		fn func(context.Context, *domain.SpecialPrices) error) (SpecialPricesDTO, error) {
		if err := scopeOf(ctx).check("parties.party", company, company, true); err != nil {
			return SpecialPricesDTO{}, err
		}
		sp, err := s.specialsOf(ctx, company, customer)
		if err != nil {
			return SpecialPricesDTO{}, err
		}
		if sp == nil {
			if !create {
				return SpecialPricesDTO{}, fw.NotFound(domain.SpecialPricesKind, customer)
			}
			if sp, err = domain.ReconstituteSpecialPrices(domain.NewSpecialPricesID(), domain.SpecialPricesState{Company: company, Customer: customer,
				Currency: vocab.MustCurrencyCode("EUR")}); err != nil {
				return SpecialPricesDTO{}, err
			}
			if err := fn(ctx, sp); err != nil {
				return SpecialPricesDTO{}, err
			}
			if err := s.specials.Create(ctx, sp); err != nil {
				return SpecialPricesDTO{}, err
			}
			return specialPricesDTO(sp), nil
		}
		if sp, err = s.specials.Update(ctx, sp.ID(), fn); err != nil {
			return SpecialPricesDTO{}, err
		}
		return specialPricesDTO(sp), nil
	}

	svc.SetSpecialPrice = guard(PermSpecialPriceUpdate, func(ctx context.Context, c SetSpecialPrice) (SpecialPricesDTO, error) {
		var v fw.Validation
		company, customer := parseOwner(&v, c.Company, c.Customer)
		line := domain.PriceLine{Product: domain.ProductID{UUID: parseID(&v, "product", c.Product)}, MinQuantity: parseDecimal(&v, "minQuantity", c.MinQuantity),
			UnitPrice: parseDecimal(&v, "unitPrice", c.UnitPrice), Discount: parseDecimal(&v, "discount", c.Discount), From: c.From, To: c.To}
		if err := v.Err(); err != nil {
			return SpecialPricesDTO{}, err
		}
		return change(ctx, company, customer, true, func(ctx context.Context, sp *domain.SpecialPrices) error {
			p, err := s.Products.Get(ctx, line.Product)
			if err != nil {
				return err
			}
			if p.State().Company != company {
				return fw.NotFound(domain.ProductKind, line.Product)
			}
			return sp.SetPrice(line)
		})
	}, retry[SetSpecialPrice, SpecialPricesDTO](), pipeline.Transactional[SetSpecialPrice, SpecialPricesDTO](s.UoW))

	svc.RemoveSpecialPrice = guard(PermSpecialPriceUpdate, func(ctx context.Context, c RemoveSpecialPrice) (SpecialPricesDTO, error) {
		var v fw.Validation
		company, customer := parseOwner(&v, c.Company, c.Customer)
		product := domain.ProductID{UUID: parseID(&v, "product", c.Product)}
		min := parseDecimal(&v, "minQuantity", c.MinQuantity)
		if err := v.Err(); err != nil {
			return SpecialPricesDTO{}, err
		}
		return change(ctx, company, customer, false, func(_ context.Context, sp *domain.SpecialPrices) error {
			return sp.RemovePrice(product, min, c.From)
		})
	}, retry[RemoveSpecialPrice, SpecialPricesDTO](), pipeline.Transactional[RemoveSpecialPrice, SpecialPricesDTO](s.UoW))

	svc.GetSpecialPrices = guard(PermSpecialPriceRead, func(ctx context.Context, q GetSpecialPrices) (SpecialPricesDTO, error) {
		var v fw.Validation
		company, customer := parseOwner(&v, q.Company, q.Customer)
		if err := v.Err(); err != nil {
			return SpecialPricesDTO{}, err
		}
		if err := scopeOf(ctx).check("parties.party", company, company, false); err != nil {
			return SpecialPricesDTO{}, err
		}
		sp, err := s.specialsOf(ctx, company, customer)
		if err != nil {
			return SpecialPricesDTO{}, err
		}
		if sp == nil {
			return SpecialPricesDTO{Company: company.String(), Customer: customer.String(), Currency: "EUR", Lines: []PriceLineDTO{}}, nil
		}
		return specialPricesDTO(sp), nil
	})
}
