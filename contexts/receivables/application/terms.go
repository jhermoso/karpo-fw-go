package application

import (
	"context"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/receivables/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/receivables/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// CreateTerms crea condiciones de pago de un vendedor.
type CreateTerms struct {
	Seller          string `json:"seller"`
	Code            string `json:"code"`
	Description     string `json:"description"`
	Installments    int    `json:"installments"`
	DaysToFirst     int    `json:"daysToFirst"`
	DaysBetween     int    `json:"daysBetween,omitempty"`
	FixedDays       []int  `json:"fixedDays,omitempty"`
	NoPaymentFrom   int    `json:"noPaymentFrom,omitempty"`
	NoPaymentTo     int    `json:"noPaymentTo,omitempty"`
	CommercialMonth bool   `json:"commercialMonth,omitempty"`
	ControlHolidays bool   `json:"controlHolidays,omitempty"`
	BackwardDays    int    `json:"backwardDays,omitempty"`
}

// RetireTerms desactiva condiciones de pago.
type RetireTerms struct {
	ID domain.TermsID `json:"-"`
}

// SearchTerms lista las condiciones de un vendedor.
type SearchTerms struct {
	Seller     string
	ActiveOnly bool
}

// PreviewSchedule muestra los vencimientos que producen unas condiciones para un importe emitido
// en una fecha.
type PreviewSchedule struct {
	ID     domain.TermsID
	Issued string
	Amount string
}

// TermsDTO es la forma de transporte de unas condiciones de pago.
type TermsDTO struct {
	ID              string `json:"id"`
	Seller          string `json:"seller"`
	Code            string `json:"code"`
	Description     string `json:"description"`
	Installments    int    `json:"installments"`
	DaysToFirst     int    `json:"daysToFirst"`
	DaysBetween     int    `json:"daysBetween"`
	FixedDays       []int  `json:"fixedDays"`
	NoPaymentFrom   int    `json:"noPaymentFrom,omitempty"`
	NoPaymentTo     int    `json:"noPaymentTo,omitempty"`
	CommercialMonth bool   `json:"commercialMonth"`
	ControlHolidays bool   `json:"controlHolidays"`
	BackwardDays    int    `json:"backwardDays"`
	Active          bool   `json:"active"`
}

// DueDTO es una fecha de vencimiento de un calendario de vencimientos.
type DueDTO struct {
	Date   string `json:"date"`
	Amount string `json:"amount"`
}

func termsDTO(t *domain.Terms) TermsDTO {
	s := t.State()
	fixed := s.FixedDays
	if fixed == nil {
		fixed = []int{}
	}
	return TermsDTO{ID: t.ID().String(), Seller: s.Seller.String(), Code: s.Code, Description: s.Description, Installments: s.Installments,
		DaysToFirst: s.DaysToFirst, DaysBetween: s.DaysBetween, FixedDays: fixed, NoPaymentFrom: int(s.NoPayment.From), NoPaymentTo: int(s.NoPayment.To),
		CommercialMonth: s.CommercialMonth, ControlHolidays: s.ControlHolidays, BackwardDays: s.BackwardDays, Active: s.Active}
}

// SetCredit fija el perfil de crédito de un cliente (se crea la primera vez que se usa).
type SetCredit struct {
	Seller   string  `json:"seller"`
	Customer string  `json:"customer"`
	Terms    string  `json:"terms,omitempty"`
	Limit    *string `json:"limit,omitempty"` // ausente: sin límite
	Blocked  bool    `json:"blocked,omitempty"`
	// BlockReason dice por qué se bloquea (D-122); sin bloqueo no se guarda.
	BlockReason string `json:"blockReason,omitempty"`
}

// GetExposure devuelve la situación de crédito de un cliente.
type GetExposure struct {
	Seller, Customer, On string
}

// SearchCredit lista los perfiles de crédito de las empresas que ve quien llama; con Seller, sólo
// los de esa empresa. Es lo que la vista por pareja necesita para enseñar el límite y el bloqueo de
// cada cliente (docs/DECISIONES-COMUNES.md, D-73).
type SearchCredit struct {
	Seller               string
	IncludeMergedParties bool // también los de parties fusionadas en otra (D-100)
}

// CreditDTO es la forma de transporte de un perfil de crédito.
type CreditDTO struct {
	ID          string `json:"id"`
	Seller      string `json:"seller"`
	Customer    string `json:"customer"`
	Terms       string `json:"terms,omitempty"`
	Limited     bool   `json:"limited"`
	Limit       string `json:"limit,omitempty"`
	Blocked     bool   `json:"blocked"`
	BlockReason string `json:"blockReason,omitempty"` // D-122
}

func creditDTO(p *domain.CreditProfile) CreditDTO {
	s := p.State()
	d := CreditDTO{ID: p.ID().String(), Seller: s.Seller.String(), Customer: s.Customer.String(), Limited: s.Limited, Blocked: s.Blocked, BlockReason: s.BlockReason}
	if !s.Terms.IsZero() {
		d.Terms = s.Terms.String()
	}
	if s.Limited {
		d.Limit = money(s.Limit)
	}
	return d
}

func (s service) termsUseCases(svc *Service) {
	svc.CreateTerms = guard(PermTermsUpdate, func(ctx context.Context, c CreateTerms) (TermsDTO, error) {
		var v fw.Validation
		seller := domain.OrganizationID{UUID: parseID(&v, "seller", c.Seller)}
		if err := v.Err(); err != nil {
			return TermsDTO{}, err
		}
		if err := scopeOf(ctx).check("parties.party", seller, seller, true); err != nil {
			return TermsDTO{}, err
		}
		t, err := domain.ReconstituteTerms(domain.NewTermsID(), domain.TermsState{Seller: seller, Code: c.Code, Description: c.Description,
			Installments: c.Installments, DaysToFirst: c.DaysToFirst, DaysBetween: c.DaysBetween, FixedDays: c.FixedDays,
			NoPayment: domain.MonthRange{From: time.Month(c.NoPaymentFrom), To: time.Month(c.NoPaymentTo)}, CommercialMonth: c.CommercialMonth,
			ControlHolidays: c.ControlHolidays, BackwardDays: c.BackwardDays, Active: true})
		if err != nil {
			return TermsDTO{}, err
		}
		dup, err := s.Terms.Exists(ctx, domain.TermsFieldSeller.Eq(seller).And(domain.TermsFieldCode.Eq(t.State().Code)))
		if err != nil {
			return TermsDTO{}, err
		}
		if dup {
			return TermsDTO{}, fw.Violation("receivables.duplicate_terms", "the seller already has terms with that code")
		}
		if err := s.terms.Create(ctx, t); err != nil {
			return TermsDTO{}, err
		}
		return termsDTO(t), nil
	}, pipeline.Transactional[CreateTerms, TermsDTO](s.UoW))

	svc.RetireTerms = guard(PermTermsUpdate, func(ctx context.Context, c RetireTerms) (TermsDTO, error) {
		sc := scopeOf(ctx)
		t, err := s.terms.Update(ctx, c.ID, func(_ context.Context, t *domain.Terms) error {
			if err := sc.check(domain.TermsKind, t.ID(), t.State().Seller, true); err != nil {
				return err
			}
			t.Retire()
			return nil
		})
		if err != nil {
			return TermsDTO{}, err
		}
		return termsDTO(t), nil
	}, retry[RetireTerms, TermsDTO]())

	svc.SearchTerms = guard(PermTermsRead, func(ctx context.Context, q SearchTerms) ([]TermsDTO, error) {
		var v fw.Validation
		parts := []spec.Specification[*domain.Terms]{within(scopeOf(ctx), domain.TermsFieldSeller)}
		if q.Seller != "" {
			parts = append(parts, domain.TermsFieldSeller.Eq(domain.OrganizationID{UUID: parseID(&v, "seller", q.Seller)}))
		}
		if q.ActiveOnly {
			parts = append(parts, domain.TermsFieldActive.Eq(true))
		}
		if err := v.Err(); err != nil {
			return nil, err
		}
		ts, err := s.Terms.Find(ctx, spec.And(parts...), domain.TermsFieldCode.Asc())
		if err != nil {
			return nil, err
		}
		out := []TermsDTO{}
		for _, t := range ts {
			out = append(out, termsDTO(t))
		}
		return out, nil
	})

	svc.Preview = guard(PermTermsRead, func(ctx context.Context, q PreviewSchedule) ([]DueDTO, error) {
		var v fw.Validation
		issued, err := vocab.ParseDate(q.Issued)
		v.Require(err == nil, "issued", "format", "a date YYYY-MM-DD is required")
		amount := parseDecimal(&v, "amount", q.Amount)
		if err := v.Err(); err != nil {
			return nil, err
		}
		t, err := s.Terms.Get(ctx, q.ID)
		if err != nil {
			return nil, err
		}
		if err := scopeOf(ctx).check(domain.TermsKind, t.ID(), t.State().Seller, false); err != nil {
			return nil, err
		}
		ds, err := t.Schedule(ctx, issued, amount, s.Calendar)
		if err != nil {
			return nil, err
		}
		out := []DueDTO{}
		for _, d := range ds {
			out = append(out, DueDTO{Date: d.Date.String(), Amount: money(d.Amount)})
		}
		return out, nil
	})

	svc.SearchCredit = guard(PermCreditRead, func(ctx context.Context, q SearchCredit) ([]CreditDTO, error) {
		var v fw.Validation
		parts := []spec.Specification[*domain.CreditProfile]{within(scopeOf(ctx), domain.CredFieldSeller)}
		if q.Seller != "" {
			parts = append(parts, domain.CredFieldSeller.Eq(domain.OrganizationID{UUID: parseID(&v, "seller", q.Seller)}))
		}
		if err := v.Err(); err != nil {
			return nil, err
		}
		merged, err := withoutMerged(ctx, s.Merged, q.IncludeMergedParties, domain.CredFieldCustomer)
		if err != nil {
			return nil, err
		}
		parts = append(parts, merged...)
		ps, err := s.Credit.Find(ctx, spec.And(parts...))
		if err != nil {
			return nil, err
		}
		out := make([]CreditDTO, 0, len(ps))
		for _, p := range ps {
			out = append(out, creditDTO(p))
		}
		return out, nil
	})

	svc.SetCredit = guard(PermCreditUpdate, func(ctx context.Context, c SetCredit) (CreditDTO, error) {
		var v fw.Validation
		seller := domain.OrganizationID{UUID: parseID(&v, "seller", c.Seller)}
		customer := domain.PartyID{UUID: parseID(&v, "customer", c.Customer)}
		var terms domain.TermsID
		if c.Terms != "" {
			terms = domain.TermsID{UUID: parseID(&v, "terms", c.Terms)}
		}
		limited, limit := c.Limit != nil, vocab.DecimalFromInt(0)
		if limited {
			limit = parseDecimal(&v, "limit", *c.Limit)
		}
		if err := v.Err(); err != nil {
			return CreditDTO{}, err
		}
		if err := scopeOf(ctx).check("parties.party", seller, seller, true); err != nil {
			return CreditDTO{}, err
		}
		if !terms.IsZero() {
			t, err := s.Terms.Get(ctx, terms)
			if err != nil {
				return CreditDTO{}, err
			}
			if t.State().Seller != seller || !t.State().Active {
				return CreditDTO{}, fw.Violation("receivables.terms_unavailable", "the terms are not active terms of the seller")
			}
		}
		ps, err := s.Credit.Find(ctx, domain.CredFieldSeller.Eq(seller).And(domain.CredFieldCustomer.Eq(customer)))
		if err != nil {
			return CreditDTO{}, err
		}
		if len(ps) == 0 {
			p, err := domain.ReconstituteCreditProfile(domain.NewCreditProfileID(), domain.CreditProfileState{Seller: seller, Customer: customer,
				Terms: terms, Limited: limited, Limit: limit, Blocked: c.Blocked, BlockReason: c.BlockReason})
			if err != nil {
				return CreditDTO{}, err
			}
			if err := s.credit.Create(ctx, p); err != nil {
				return CreditDTO{}, err
			}
			return creditDTO(p), nil
		}
		p, err := s.credit.Update(ctx, ps[0].ID(), func(_ context.Context, p *domain.CreditProfile) error {
			return p.Set(terms, limited, limit, c.Blocked, c.BlockReason)
		})
		if err != nil {
			return CreditDTO{}, err
		}
		return creditDTO(p), nil
	}, pipeline.Transactional[SetCredit, CreditDTO](s.UoW))

	svc.GetExposure = guard(PermCreditRead, func(ctx context.Context, q GetExposure) (contracts.Exposure, error) {
		var v fw.Validation
		seller := domain.OrganizationID{UUID: parseID(&v, "seller", q.Seller)}
		if err := v.Err(); err != nil {
			return contracts.Exposure{}, err
		}
		if err := scopeOf(ctx).check("parties.party", seller, seller, false); err != nil {
			return contracts.Exposure{}, err
		}
		return CreditPort{Receivables: s.Receivables, Credit: s.Credit}.Exposure(ctx, q.Seller, q.Customer, q.On)
	})
}

// CreditPort implementa contracts.Credit (sirve a contextos, no a usuarios).
type CreditPort struct {
	Receivables domain.ReceivableRepository
	Credit      domain.CreditProfileRepository
}

var _ contracts.Credit = CreditPort{}

// Exposure implementa contracts.Credit: importes abiertos y vencidos del cliente con el vendedor
// frente a su límite (el C# sólo comparaba cada pedido con MaxRisk).
func (p CreditPort) Exposure(ctx context.Context, seller, customer, on string) (contracts.Exposure, error) {
	var v fw.Validation
	sid := domain.OrganizationID{UUID: parseID(&v, "seller", seller)}
	cid := domain.PartyID{UUID: parseID(&v, "customer", customer)}
	day := vocab.DateOf(fw.Now())
	if on != "" {
		d, err := vocab.ParseDate(on)
		v.Require(err == nil, "on", "format", "a date YYYY-MM-DD is required")
		day = d
	}
	if err := v.Err(); err != nil {
		return contracts.Exposure{}, err
	}
	rs, err := p.Receivables.Find(ctx, spec.And(domain.RecFieldSeller.Eq(sid), domain.RecFieldCustomer.Eq(cid), domain.RecFieldSettled.Eq(false)))
	if err != nil {
		return contracts.Exposure{}, err
	}
	open, overdue := vocab.DecimalFromInt(0), vocab.DecimalFromInt(0)
	for _, r := range rs {
		open, overdue = open.Add(r.Open()), overdue.Add(r.Overdue(day))
	}
	out := contracts.Exposure{Open: money(open), Overdue: money(overdue)}
	ps, err := p.Credit.Find(ctx, domain.CredFieldSeller.Eq(sid).And(domain.CredFieldCustomer.Eq(cid)))
	if err != nil {
		return contracts.Exposure{}, err
	}
	if len(ps) > 0 {
		out.Blocked = ps[0].State().Blocked
		if av, limited := ps[0].Available(open); limited {
			out.Limited, out.Limit, out.Available = true, money(ps[0].State().Limit), money(av)
		}
	}
	return out, nil
}
