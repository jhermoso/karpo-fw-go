package application

import (
	"context"
	"strings"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/purchases/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
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
	// Blocked no cambia el bloqueo (D-156 ampliada): si pide un estado distinto del actual (o
	// bloqueado en un alta) es 422 purchases.block_change_requires_command; si coincide o no viene,
	// se ignora. Se bloquea y desbloquea con BlockSupplier.
	Blocked *bool `json:"blocked,omitempty"`
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
	UnblockReason   string `json:"unblockReason,omitempty"`
	Version         int64  `json:"version"`
	// PendingApproval es el id de la solicitud de aprobación que ha dejado la orden de bloquear o
	// desbloquear, si la exige (D-156): el perfil no ha cambiado.
	PendingApproval string `json:"pendingApproval,omitempty"`
}

func supplierDTO(p *domain.SupplierProfile) SupplierDTO {
	s := p.State()
	return SupplierDTO{ID: p.ID().String(), Company: s.Company.String(), Supplier: s.Supplier.String(), Category: string(s.Category),
		WithholdingRate: money(s.WithholdingRate), PaymentDays: s.PaymentDays, IBAN: s.IBAN.String(), Blocked: s.Blocked, BlockReason: s.BlockReason,
		UnblockReason: s.UnblockReason, Version: p.Version()}
}

// state valida la orden y la convierte en el estado de un perfil.
func (c SetSupplier) state() (domain.SupplierState, error) {
	var v fw.Validation
	company := domain.OrganizationID{UUID: parseID(&v, "company", c.Company)}
	supplier := domain.PartyID{UUID: parseID(&v, "supplier", c.Supplier)}
	st := domain.SupplierState{Company: company, Supplier: supplier, Category: domain.Category(strings.TrimSpace(c.Category)),
		WithholdingRate: parseDecimal(&v, "withholdingRate", c.WithholdingRate), PaymentDays: c.PaymentDays}
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
	// Blocked, como en SetSupplier: no cambia el bloqueo.
	Blocked *bool `json:"blocked,omitempty"`
	// Version, si no es cero, es la que se leyó: si el perfil ya está en otra, el cambio es un
	// conflicto y no se reintenta.
	Version int64 `json:"-"`
}

// BlockSupplier bloquea un perfil (Blocked) o lo desbloquea, con su motivo obligatorio. Si la
// empresa exige aprobación para esa acción, no cambia el perfil: deja la solicitud y el DTO trae
// PendingApproval.
type BlockSupplier struct {
	ID      domain.SupplierID `json:"-"`
	Blocked bool              `json:"-"`
	Reason  string            `json:"reason"`
	// Version, si no es cero, es la que se leyó (como en ChangeSupplier).
	Version int64 `json:"version,omitempty"`
}

// SupplierBlockHistory lee el historial de bloqueos de un perfil.
type SupplierBlockHistory struct{ ID domain.SupplierID }

// BlockEventDTO es una entrada del historial de bloqueos.
type BlockEventDTO struct {
	ID       string    `json:"id"`
	Profile  string    `json:"profile"`
	Company  string    `json:"company"`
	Supplier string    `json:"supplier"`
	Blocked  bool      `json:"blocked"`
	Reason   string    `json:"reason"`
	UserID   string    `json:"userId,omitempty"`
	UserName string    `json:"userName,omitempty"`
	At       time.Time `json:"at"`
	Approval string    `json:"approval,omitempty"`
}

func blockEventDTO(e *domain.BlockEvent) BlockEventDTO {
	s := e.State()
	out := BlockEventDTO{ID: e.ID().String(), Profile: s.Profile.String(), Company: s.Company.String(), Supplier: s.Supplier.String(),
		Blocked: s.Blocked, Reason: s.Reason, UserName: s.UserName, At: s.At}
	if !s.UserID.IsZero() {
		out.UserID = s.UserID.String()
	}
	if !s.Approval.IsZero() {
		out.Approval = s.Approval.String()
	}
	return out
}

// sameBlock comprueba que el alta o el cambio general no pide cambiar el bloqueo.
func sameBlock(asked *bool, current bool) error {
	if asked != nil && *asked != current {
		return fw.Violation(domain.CodeBlockChangeRequiresCommand, "block and unblock a supplier with their own commands")
	}
	return nil
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
	// IDs son las identidades pedidas, tal como llegan (docs/CONTRATO-MANTENIMIENTO.md); ByIDs dice
	// que el filtro llegó, aunque esté vacío.
	IDs   []string
	ByIDs bool
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
			p, err := s.suppliers.Update(ctx, ps[0].ID(), func(ctx context.Context, p *domain.SupplierProfile) error {
				if err := sameBlock(c.Blocked, p.State().Blocked); err != nil {
					return err
				}
				return p.Change(st)
			})
			if err != nil {
				return SupplierDTO{}, err
			}
			return supplierDTO(p), nil
		}
		return s.register(ctx, st, c.Blocked)
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
		return s.register(ctx, st, c.Blocked)
	}, retry[SetSupplier, SupplierDTO](), pipeline.Transactional[SetSupplier, SupplierDTO](s.UoW))

	svc.ChangeSupplier = versioned(s.UoW, PermSupplierUpdate, func(c ChangeSupplier) int64 { return c.Version }, func(ctx context.Context, c ChangeSupplier) (SupplierDTO, error) {
		sc := scopeOf(ctx)
		p, err := s.suppliers.Update(ctx, c.ID, func(ctx context.Context, p *domain.SupplierProfile) error {
			cur := p.State()
			if err := sc.check(domain.SupplierKind, p.ID(), cur.Company, true); err != nil {
				return err
			}
			if c.Version != 0 && p.Version() != c.Version {
				return fw.Conflict(domain.SupplierKind, p.ID(), c.Version, "the supplier changed after it was read: read it again")
			}
			st, err := SetSupplier{Company: cur.Company.String(), Supplier: cur.Supplier.String(), Category: c.Category, WithholdingRate: c.WithholdingRate,
				PaymentDays: c.PaymentDays, IBAN: c.IBAN}.state()
			if err != nil {
				return err
			}
			if err := sameBlock(c.Blocked, cur.Blocked); err != nil {
				return err
			}
			return p.Change(st)
		})
		if err != nil {
			return SupplierDTO{}, err
		}
		return supplierDTO(p), nil
	})

	// BlockSupplier bloquea o desbloquea (D-156 ampliada). Con aprobación, deja la solicitud y no
	// toca el perfil (ni su versión); sin ella, cambia el perfil y deja la entrada del historial en
	// la misma transacción.
	svc.BlockSupplier = versioned(s.UoW, PermSupplierUpdate, func(c BlockSupplier) int64 { return c.Version }, func(ctx context.Context, c BlockSupplier) (SupplierDTO, error) {
		p, err := s.Suppliers.Get(ctx, c.ID)
		if err != nil {
			return SupplierDTO{}, err
		}
		cur := p.State()
		if err := scopeOf(ctx).check(domain.SupplierKind, p.ID(), cur.Company, true); err != nil {
			return SupplierDTO{}, err
		}
		if c.Version != 0 && p.Version() != c.Version {
			return SupplierDTO{}, fw.Conflict(domain.SupplierKind, p.ID(), c.Version, "the supplier changed after it was read: read it again")
		}
		// Las infracciones (el motivo, ya bloqueado) se comprueban sobre una copia antes de pedir
		// aprobación.
		probe, err := domain.ReconstituteSupplier(p.ID(), cur)
		if err != nil {
			return SupplierDTO{}, err
		}
		if err := probe.SetBlocked(c.Blocked, c.Reason); err != nil {
			return SupplierDTO{}, err
		}
		pending, err := s.gate(ctx, p.ID(), cur.Company, c.Blocked, c.Reason)
		if err != nil || pending != "" {
			return withPending(supplierDTO(p), pending), err
		}
		if p, err = s.setBlocked(ctx, c.ID, c.Blocked, c.Reason, c.Version, fw.UUID{}); err != nil {
			return SupplierDTO{}, err
		}
		return supplierDTO(p), nil
	})

	svc.SupplierBlockHistory = guard(PermSupplierRead, func(ctx context.Context, q SupplierBlockHistory) ([]BlockEventDTO, error) {
		p, err := s.Suppliers.Get(ctx, q.ID)
		if err != nil {
			return nil, err
		}
		if err := scopeOf(ctx).check(domain.SupplierKind, p.ID(), p.State().Company, false); err != nil {
			return nil, err
		}
		es, err := s.BlockEvents.Find(ctx, domain.BlkFieldProfile.Eq(q.ID), domain.BlkFieldAt.Asc(), domain.BlkFieldIDOrder.Asc())
		if err != nil {
			return nil, err
		}
		out := make([]BlockEventDTO, 0, len(es))
		for _, e := range es {
			out = append(out, blockEventDTO(e))
		}
		return out, nil
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
		if q.ByIDs {
			parts = append(parts, domain.SuppliersWithIDs(app.ParseIDs(&v, q.IDs, domain.ParseSupplierID)...))
		}
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
		// Pedir por identidad o por la party trae también los de parties fusionadas: ids equivale a
		// pedirlos uno a uno, y una party se pide por sí misma (D-100, D-108).
		includeMerged := q.IncludeMergedParties || q.ByIDs || q.Supplier != ""
		merged, err := withoutMerged(ctx, s.Merged, includeMerged, domain.SupFieldSupplier)
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

// register da de alta un perfil, que nace sin bloquear.
func (s service) register(ctx context.Context, st domain.SupplierState, blocked *bool) (SupplierDTO, error) {
	if err := sameBlock(blocked, false); err != nil {
		return SupplierDTO{}, err
	}
	p, err := domain.NewSupplier(domain.NewSupplierID(), st)
	if err != nil {
		return SupplierDTO{}, err
	}
	if err := s.suppliers.Create(ctx, p); err != nil {
		return SupplierDTO{}, err
	}
	return supplierDTO(p), nil
}

// setBlocked bloquea o desbloquea el perfil y deja la entrada del historial, en la transacción de
// ctx. version, si no es cero, es la que se leyó: se comprueba dentro del mutador, así un
// reintento no bloquea sobre una versión que el usuario no leyó. approval es la solicitud
// aprobada, si la hubo. El usuario es el del contexto de autorización (el mismo que usa el motor de
// aprobaciones); sin él, el actor de la petición.
func (s service) setBlocked(ctx context.Context, id domain.SupplierID, blocked bool, reason string, version int64, approval fw.UUID) (*domain.SupplierProfile, error) {
	p, err := s.suppliers.Update(ctx, id, func(_ context.Context, p *domain.SupplierProfile) error {
		if version != 0 && p.Version() != version {
			return fw.Conflict(domain.SupplierKind, p.ID(), version, "the supplier changed after it was read: read it again")
		}
		return p.SetBlocked(blocked, reason)
	})
	if err != nil {
		return nil, err
	}
	st := p.State()
	actor := app.ActorFrom(ctx)
	if ac, ok := authz.FromContext(ctx); ok {
		actor = ac.Actor()
	}
	e, err := domain.NewBlockEvent(domain.NewBlockEventID(), domain.BlockEventState{Profile: id, Company: st.Company, Supplier: st.Supplier,
		Blocked: blocked, Reason: st.BlockReason + st.UnblockReason, UserID: actor.PartyID, UserName: actor.Name, At: fw.Now(), Approval: approval})
	if err != nil {
		return nil, err
	}
	if err := s.BlockEvents.Save(ctx, e); err != nil {
		return nil, err
	}
	return p, nil
}

func withPending(d SupplierDTO, pending string) SupplierDTO {
	d.PendingApproval = pending
	return d
}
