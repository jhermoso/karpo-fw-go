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

// SetSupplier crea o sustituye el perfil de compra de un proveedor en una empresa.
type SetSupplier struct {
	Company         string `json:"company"`
	Supplier        string `json:"supplier"`
	Category        string `json:"category,omitempty"`
	WithholdingRate string `json:"withholdingRate,omitempty"`
	PaymentDays     int    `json:"paymentDays,omitempty"`
	IBAN            string `json:"iban,omitempty"`
	Blocked         bool   `json:"blocked,omitempty"`
	// BlockReason dice por qué se bloquea (D-122); sin bloqueo no se guarda.
	BlockReason string `json:"blockReason,omitempty"`
}

// SearchSuppliers lista los perfiles de proveedor de una empresa.
type SearchSuppliers struct {
	Company              string
	IncludeMergedParties bool // también los de parties fusionadas en otra (D-100)
}

// SupplierDTO es la forma de transporte de un perfil de proveedor.
type SupplierDTO struct {
	ID              string `json:"id"`
	Company         string `json:"company"`
	Supplier        string `json:"supplier"`
	Category        string `json:"category,omitempty"`
	WithholdingRate string `json:"withholdingRate"`
	PaymentDays     int    `json:"paymentDays"`
	IBAN            string `json:"iban,omitempty"`
	Blocked         bool   `json:"blocked"`
	BlockReason     string `json:"blockReason,omitempty"` // D-122
	Version         int64  `json:"version"`
}

func supplierDTO(p *domain.SupplierProfile) SupplierDTO {
	s := p.State()
	return SupplierDTO{ID: p.ID().String(), Company: s.Company.String(), Supplier: s.Supplier.String(), Category: string(s.Category),
		WithholdingRate: money(s.WithholdingRate), PaymentDays: s.PaymentDays, IBAN: s.IBAN.String(), Blocked: s.Blocked, BlockReason: s.BlockReason, Version: p.Version()}
}

func (s service) supplierUseCases(svc *Service) {
	svc.SetSupplier = guard(PermSupplierUpdate, func(ctx context.Context, c SetSupplier) (SupplierDTO, error) {
		var v fw.Validation
		company := domain.OrganizationID{UUID: parseID(&v, "company", c.Company)}
		supplier := domain.PartyID{UUID: parseID(&v, "supplier", c.Supplier)}
		st := domain.SupplierState{Company: company, Supplier: supplier, Category: domain.Category(strings.TrimSpace(c.Category)),
			WithholdingRate: parseDecimal(&v, "withholdingRate", c.WithholdingRate), PaymentDays: c.PaymentDays, Blocked: c.Blocked, BlockReason: c.BlockReason}
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
		merged, err := withoutMerged(ctx, s.Merged, q.IncludeMergedParties, domain.SupFieldSupplier)
		if err != nil {
			return nil, err
		}
		ps, err := s.Suppliers.Find(ctx, spec.And(append(merged, domain.SupFieldCompany.Eq(company))...))
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
