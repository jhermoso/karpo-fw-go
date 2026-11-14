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

// state valida la orden y la convierte en el estado de un perfil.
func (c SetSupplier) state() (domain.SupplierState, error) {
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
	return st, v.Err()
}

// ChangeSupplier cambia los valores por defecto de un perfil por su identidad (la empresa y el
// proveedor no cambian).
type ChangeSupplier struct {
	ID              domain.SupplierID `json:"-"`
	Category        string            `json:"category,omitempty"`
	WithholdingRate string            `json:"withholdingRate,omitempty"`
	PaymentDays     int               `json:"paymentDays,omitempty"`
	IBAN            string            `json:"iban,omitempty"`
	Blocked         bool              `json:"blocked,omitempty"`
	BlockReason     string            `json:"blockReason,omitempty"`
	// Version, si no es cero, es la que se leyó: si el perfil ya está en otra, el cambio es un
	// conflicto y no se reintenta.
	Version int64 `json:"-"`
}

// GetSupplier lee un perfil de proveedor.
type GetSupplier struct{ ID domain.SupplierID }

// PageSuppliers busca, paginado, entre los perfiles de proveedor del ámbito de quien llama.
type PageSuppliers struct {
	Company    string
	Supplier   string // la party proveedora
	Category   string
	ActiveOnly bool   // deja fuera los bloqueados
	Sort       string // paymentDays (por omisión); los empates, por la identidad
	Desc       bool
	// IncludeMergedParties incluye los de parties fusionadas en otra (D-100).
	IncludeMergedParties bool
	Page, Size           int
}

func (s service) supplierUseCases(svc *Service) {
	svc.SetSupplier = guard(PermSupplierUpdate, func(ctx context.Context, c SetSupplier) (SupplierDTO, error) {
		st, err := c.state()
		if err != nil {
			return SupplierDTO{}, err
		}
		company, supplier := st.Company, st.Supplier
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

	// RegisterSupplier da de alta un perfil: si la empresa ya tiene uno para ese proveedor, es una
	// infracción (SetSupplier, en cambio, lo sustituye).
	svc.RegisterSupplier = guard(PermSupplierUpdate, func(ctx context.Context, c SetSupplier) (SupplierDTO, error) {
		st, err := c.state()
		if err != nil {
			return SupplierDTO{}, err
		}
		if err := scopeOf(ctx).check("parties.party", st.Company, st.Company, true); err != nil {
			return SupplierDTO{}, err
		}
		dup, err := s.Suppliers.Exists(ctx, spec.And(domain.SupFieldCompany.Eq(st.Company), domain.SupFieldSupplier.Eq(st.Supplier)))
		if err != nil {
			return SupplierDTO{}, err
		}
		if dup {
			return SupplierDTO{}, fw.Violation("purchases.duplicate_supplier", "the company already has a profile for that supplier")
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

	svc.ChangeSupplier = versioned(s.UoW, PermSupplierUpdate, func(c ChangeSupplier) int64 { return c.Version }, func(ctx context.Context, c ChangeSupplier) (SupplierDTO, error) {
		sc := scopeOf(ctx)
		p, err := s.suppliers.Update(ctx, c.ID, func(_ context.Context, p *domain.SupplierProfile) error {
			cur := p.State()
			if err := sc.check(domain.SupplierKind, p.ID(), cur.Company, true); err != nil {
				return err
			}
			if c.Version != 0 && p.Version() != c.Version {
				return fw.Conflict(domain.SupplierKind, p.ID(), c.Version, "the supplier changed after it was read: read it again")
			}
			st, err := SetSupplier{Company: cur.Company.String(), Supplier: cur.Supplier.String(), Category: c.Category, WithholdingRate: c.WithholdingRate,
				PaymentDays: c.PaymentDays, IBAN: c.IBAN, Blocked: c.Blocked, BlockReason: c.BlockReason}.state()
			if err != nil {
				return err
			}
			return p.Change(st)
		})
		if err != nil {
			return SupplierDTO{}, err
		}
		return supplierDTO(p), nil
	})

	// read carga un perfil del ámbito de quien llama (404 fuera de él; 403 si se pide escribir y es
	// de sólo lectura).
	read := func(ctx context.Context, id domain.SupplierID, write bool) (*domain.SupplierProfile, error) {
		p, err := s.Suppliers.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		if err := scopeOf(ctx).check(domain.SupplierKind, p.ID(), p.State().Company, write); err != nil {
			return nil, err
		}
		return p, nil
	}
	svc.GetSupplier = guard(PermSupplierRead, func(ctx context.Context, q GetSupplier) (SupplierDTO, error) {
		p, err := read(ctx, q.ID, false)
		if err != nil {
			return SupplierDTO{}, err
		}
		return supplierDTO(p), nil
	})
	svc.SupplierChangeable = guard(PermSupplierUpdate, func(ctx context.Context, q GetSupplier) (bool, error) {
		_, err := read(ctx, q.ID, true)
		return err == nil, err
	})
	svc.PageSuppliers = guard(PermSupplierRead, func(ctx context.Context, q PageSuppliers) (fw.Page[SupplierDTO], error) {
		var v fw.Validation
		parts := []spec.Specification[*domain.SupplierProfile]{within(scopeOf(ctx), domain.SupFieldCompany)}
		if q.Company != "" {
			parts = append(parts, domain.SupFieldCompany.Eq(domain.OrganizationID{UUID: parseID(&v, "company", q.Company)}))
		}
		if q.Supplier != "" {
			parts = append(parts, domain.SupFieldSupplier.Eq(domain.PartyID{UUID: parseID(&v, "supplier", q.Supplier)}))
		}
		if c := strings.TrimSpace(q.Category); c != "" {
			parts = append(parts, domain.SupFieldCategory.Eq(domain.Category(c)))
		}
		if q.ActiveOnly {
			parts = append(parts, domain.SupFieldBlocked.Eq(false))
		}
		order := domain.SupFieldPaymentDays.Asc()
		switch {
		case q.Sort != "" && q.Sort != "paymentDays":
			v.Add("sortBy", "enum", "paymentDays")
		case q.Desc:
			order = domain.SupFieldPaymentDays.Desc()
		}
		if err := v.Err(); err != nil {
			return fw.Page[SupplierDTO]{}, err
		}
		merged, err := withoutMerged(ctx, s.Merged, q.IncludeMergedParties, domain.SupFieldSupplier)
		if err != nil {
			return fw.Page[SupplierDTO]{}, err
		}
		parts = append(parts, merged...)
		page, err := s.Suppliers.FindPage(ctx, spec.And(parts...), fw.NewPageRequest(q.Page, q.Size, order, domain.SupFieldIDOrder.Asc()))
		if err != nil {
			return fw.Page[SupplierDTO]{}, err
		}
		return fw.MapPage(page, supplierDTO), nil
	})
}
