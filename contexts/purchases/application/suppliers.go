package application

import (
	"context"
	"strings"

	"github.com/jhermoso/karpo-fw-go/contexts/purchases/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// SetSupplier creates or replaces the purchase profile of a supplier in a company.
type SetSupplier struct {
	Company         string `json:"company"`
	Supplier        string `json:"supplier"`
	Category        string `json:"category,omitempty"`
	WithholdingRate string `json:"withholdingRate,omitempty"`
	PaymentDays     int    `json:"paymentDays,omitempty"`
	IBAN            string `json:"iban,omitempty"`
	Blocked         bool   `json:"blocked,omitempty"`
}

// SearchSuppliers lists the supplier profiles of a company.
type SearchSuppliers struct{ Company string }

// SupplierDTO is the transport form of a supplier profile.
type SupplierDTO struct {
	ID              string `json:"id"`
	Company         string `json:"company"`
	Supplier        string `json:"supplier"`
	Category        string `json:"category,omitempty"`
	WithholdingRate string `json:"withholdingRate"`
	PaymentDays     int    `json:"paymentDays"`
	IBAN            string `json:"iban,omitempty"`
	Blocked         bool   `json:"blocked"`
	Version         int64  `json:"version"`
}

func supplierDTO(p *domain.SupplierProfile) SupplierDTO {
	s := p.State()
	return SupplierDTO{ID: p.ID().String(), Company: s.Company.String(), Supplier: s.Supplier.String(), Category: string(s.Category),
		WithholdingRate: money(s.WithholdingRate), PaymentDays: s.PaymentDays, IBAN: s.IBAN.String(), Blocked: s.Blocked, Version: p.Version()}
}

func (s service) supplierUseCases(svc *Service) {
	svc.SetSupplier = guard(PermSupplierUpdate, func(ctx context.Context, c SetSupplier) (SupplierDTO, error) {
		var v fw.Validation
		company := domain.OrganizationID{UUID: parseID(&v, "company", c.Company)}
		supplier := domain.PartyID{UUID: parseID(&v, "supplier", c.Supplier)}
		st := domain.SupplierState{Company: company, Supplier: supplier, Category: domain.Category(strings.TrimSpace(c.Category)),
			WithholdingRate: parseDecimal(&v, "withholdingRate", c.WithholdingRate), PaymentDays: c.PaymentDays, Blocked: c.Blocked}
		if c.IBAN != "" {
			iban, err := vocab.NewIBAN(c.IBAN)
			v.Merge("iban", err)
			st.IBAN = iban
		}
		if err := v.Err(); err != nil {
			return SupplierDTO{}, err
		}
		if err := scopeOf(ctx).check("parties.party", company, company, true); err != nil {
			return SupplierDTO{}, err
		}
		ps, err := s.Suppliers.Find(ctx, spec.And(domain.SupFieldCompany.Eq(company), domain.SupFieldSupplier.Eq(supplier)))
		if err != nil {
			return SupplierDTO{}, err
		}
		if len(ps) > 0 {
			p, err := s.suppliers.Update(ctx, ps[0].ID(), func(_ context.Context, p *domain.SupplierProfile) error { return p.Change(st) })
			if err != nil {
				return SupplierDTO{}, err
			}
			return supplierDTO(p), nil
		}
		p, err := domain.ReconstituteSupplier(domain.NewSupplierID(), st)
		if err != nil {
			return SupplierDTO{}, err
		}
		if err := s.suppliers.Create(ctx, p); err != nil {
			return SupplierDTO{}, err
		}
		return supplierDTO(p), nil
	}, retry[SetSupplier, SupplierDTO](), pipeline.Transactional[SetSupplier, SupplierDTO](s.UoW))

	svc.SearchSuppliers = guard(PermSupplierRead, func(ctx context.Context, q SearchSuppliers) ([]SupplierDTO, error) {
		var v fw.Validation
		company := domain.OrganizationID{UUID: parseID(&v, "company", q.Company)}
		if err := v.Err(); err != nil {
			return nil, err
		}
		if err := scopeOf(ctx).check("parties.party", company, company, false); err != nil {
			return nil, err
		}
		ps, err := s.Suppliers.Find(ctx, domain.SupFieldCompany.Eq(company))
		if err != nil {
			return nil, err
		}
		out := []SupplierDTO{}
		for _, p := range ps {
			out = append(out, supplierDTO(p))
		}
		return out, nil
	})
}
