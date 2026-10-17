package application

import (
	"context"

	"github.com/jhermoso/karpo-fw-go/contexts/payments/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// AllocationInput aplica parte de un pago a una obligación.
type AllocationInput struct {
	Payable string `json:"payable"`
	Amount  string `json:"amount"`
}

// RegisterPayment registra dinero pagado, aplicado en el acto si se quiere. Payee es un id de
// party, o Authority la administración tributaria (AEAT).
type RegisterPayment struct {
	Company     string            `json:"company"`
	Payee       string            `json:"payee,omitempty"`
	Authority   string            `json:"authority,omitempty"`
	Date        vocab.Date        `json:"date"`
	Amount      string            `json:"amount"`
	Method      string            `json:"method"`
	Reference   string            `json:"reference,omitempty"`
	Allocations []AllocationInput `json:"allocations,omitempty"`
}

// Allocate aplica parte de un pago a una obligación.
type Allocate struct {
	ID domain.PaymentID `json:"-"`
	AllocationInput
}

// Deallocate revierte una aplicación.
type Deallocate struct {
	ID         domain.PaymentID `json:"-"`
	Allocation string           `json:"allocation"`
}

// CancelPayment anula un pago y revierte sus aplicaciones.
type CancelPayment struct {
	ID domain.PaymentID `json:"-"`
}

// GetPayment carga un pago.
type GetPayment struct{ ID domain.PaymentID }

// SearchPayments busca pagos del ámbito de quien llama.
type SearchPayments struct {
	Company, Payee string
	Page, Size     int
}

// AllocationDTO es la forma de transporte de una aplicación.
type AllocationDTO struct {
	ID      string `json:"id"`
	Payable string `json:"payable"`
	Kind    string `json:"kind"`
	Amount  string `json:"amount"`
	On      string `json:"on"`
}

// PaymentDTO es la forma de transporte de un pago.
type PaymentDTO struct {
	ID          string          `json:"id"`
	Company     string          `json:"company"`
	Payee       string          `json:"payee"`
	Date        string          `json:"date"`
	Amount      string          `json:"amount"`
	Method      string          `json:"method"`
	Reference   string          `json:"reference,omitempty"`
	Allocated   string          `json:"allocated"`
	Unallocated string          `json:"unallocated"`
	Cancelled   bool            `json:"cancelled"`
	Allocations []AllocationDTO `json:"allocations"`
	Version     int64           `json:"version"`
}

func paymentDTO(p *domain.Payment) PaymentDTO {
	s := p.State()
	payee := s.Authority
	if !s.Payee.IsZero() {
		payee = s.Payee.String()
	}
	d := PaymentDTO{ID: p.ID().String(), Company: s.Company.String(), Payee: payee, Date: s.Date.String(), Amount: money(s.Amount), Method: s.Method.String(),
		Reference: s.Reference, Allocated: money(p.Allocated()), Unallocated: money(p.Unallocated()), Cancelled: s.Cancelled, Allocations: []AllocationDTO{},
		Version: p.Version()}
	for _, a := range s.Allocations {
		d.Allocations = append(d.Allocations, AllocationDTO{ID: a.ID.String(), Payable: a.Payable.String(), Kind: a.Kind.String(), Amount: money(a.Amount),
			On: a.On.String()})
	}
	return d
}

// apply registra una aplicación en los dos lados: primero la obligación (lo abierto), para que un
// importe rechazado no deje ninguna aplicación en el pago, y luego el pago. Los dos cambian en la
// unidad de trabajo de quien llama.
func (s service) apply(ctx context.Context, p *domain.Payment, in AllocationInput, on vocab.Date) error {
	var v fw.Validation
	id := domain.PayableID{UUID: parseID(&v, "payable", in.Payable)}
	amount := parseDecimal(&v, "amount", in.Amount)
	if err := v.Err(); err != nil {
		return err
	}
	ps := p.State()
	_, err := s.payables.Update(ctx, id, func(_ context.Context, b *domain.Payable) error {
		bs := b.State()
		if bs.Company != ps.Company {
			return fw.NotFound(domain.PayableKind, id)
		}
		if bs.Payee != ps.Payee || (ps.Payee.IsZero() && bs.Authority != ps.Authority) {
			return fw.Violation("payments.other_payee", "the payable is owed to another payee")
		}
		if err := b.Apply(amount); err != nil {
			return err
		}
		_, err := p.Allocate(id, bs.Kind, amount, on)
		return err
	})
	return err
}

func (s service) revert(ctx context.Context, allocations []domain.Allocation) error {
	for _, a := range allocations {
		if _, err := s.payables.Update(ctx, a.Payable, func(_ context.Context, b *domain.Payable) error { return b.Unapply(a.Amount) }); err != nil {
			return err
		}
	}
	return nil
}

func (s service) register(ctx context.Context, st domain.PaymentState, allocations []AllocationInput) (*domain.Payment, error) {
	p, err := domain.RegisterPayment(domain.NewPaymentID(), st)
	if err != nil {
		return nil, err
	}
	for _, a := range allocations {
		if err := s.apply(ctx, p, a, st.Date); err != nil {
			return nil, err
		}
	}
	if err := s.payments.Create(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

func (s service) paymentUseCases(svc *Service) {
	update := func(ctx context.Context, id domain.PaymentID, fn func(context.Context, *domain.Payment) error) (PaymentDTO, error) {
		sc := scopeOf(ctx)
		p, err := s.payments.Update(ctx, id, func(ctx context.Context, p *domain.Payment) error {
			if err := sc.check(domain.PaymentKind, p.ID(), p.State().Company, true); err != nil {
				return err
			}
			return fn(ctx, p)
		})
		if err != nil {
			return PaymentDTO{}, err
		}
		return paymentDTO(p), nil
	}

	svc.RegisterPayment = guard(PermPaymentWrite, func(ctx context.Context, c RegisterPayment) (PaymentDTO, error) {
		var v fw.Validation
		company := domain.OrganizationID{UUID: parseID(&v, "company", c.Company)}
		var payee domain.PartyID
		if c.Payee != "" {
			payee = domain.PartyID{UUID: parseID(&v, "payee", c.Payee)}
		}
		method, ok := domain.ParseMethod(c.Method)
		v.Require(ok, "method", "enum", "cash, transfer, card, check or direct-debit")
		amount := parseDecimal(&v, "amount", c.Amount)
		if err := v.Err(); err != nil {
			return PaymentDTO{}, err
		}
		if err := scopeOf(ctx).check("parties.party", company, company, true); err != nil {
			return PaymentDTO{}, err
		}
		p, err := s.register(ctx, domain.PaymentState{Company: company, Payee: payee, Authority: c.Authority, Date: c.Date, Amount: amount, Currency: euro,
			Method: method, Reference: c.Reference}, c.Allocations)
		if err != nil {
			return PaymentDTO{}, err
		}
		return paymentDTO(p), nil
	}, retry[RegisterPayment, PaymentDTO](), pipeline.Transactional[RegisterPayment, PaymentDTO](s.UoW))

	svc.Allocate = guard(PermPaymentWrite, func(ctx context.Context, c Allocate) (PaymentDTO, error) {
		return update(ctx, c.ID, func(ctx context.Context, p *domain.Payment) error {
			return s.apply(ctx, p, c.AllocationInput, vocab.DateOf(fw.Now()))
		})
	}, retry[Allocate, PaymentDTO](), pipeline.Transactional[Allocate, PaymentDTO](s.UoW))

	svc.Deallocate = guard(PermPaymentWrite, func(ctx context.Context, c Deallocate) (PaymentDTO, error) {
		var v fw.Validation
		id := domain.AllocationID{UUID: parseID(&v, "allocation", c.Allocation)}
		if err := v.Err(); err != nil {
			return PaymentDTO{}, err
		}
		return update(ctx, c.ID, func(ctx context.Context, p *domain.Payment) error {
			a, err := p.Deallocate(id)
			if err != nil {
				return err
			}
			return s.revert(ctx, []domain.Allocation{a})
		})
	}, retry[Deallocate, PaymentDTO](), pipeline.Transactional[Deallocate, PaymentDTO](s.UoW))

	svc.CancelPayment = guard(PermPaymentWrite, func(ctx context.Context, c CancelPayment) (PaymentDTO, error) {
		return update(ctx, c.ID, func(ctx context.Context, p *domain.Payment) error {
			back, err := p.Cancel()
			if err != nil {
				return err
			}
			return s.revert(ctx, back)
		})
	}, retry[CancelPayment, PaymentDTO](), pipeline.Transactional[CancelPayment, PaymentDTO](s.UoW))

	svc.GetPayment = guard(PermPaymentRead, func(ctx context.Context, q GetPayment) (PaymentDTO, error) {
		p, err := s.Payments.Get(ctx, q.ID)
		if err != nil {
			return PaymentDTO{}, err
		}
		if err := scopeOf(ctx).check(domain.PaymentKind, p.ID(), p.State().Company, false); err != nil {
			return PaymentDTO{}, err
		}
		return paymentDTO(p), nil
	})

	svc.SearchPayments = guard(PermPaymentRead, func(ctx context.Context, q SearchPayments) (fw.Page[PaymentDTO], error) {
		var v fw.Validation
		parts := []spec.Specification[*domain.Payment]{within(scopeOf(ctx), domain.PmtFieldCompany)}
		if q.Company != "" {
			parts = append(parts, domain.PmtFieldCompany.Eq(domain.OrganizationID{UUID: parseID(&v, "company", q.Company)}))
		}
		if q.Payee != "" {
			parts = append(parts, domain.PmtFieldPayee.Eq(domain.PartyID{UUID: parseID(&v, "payee", q.Payee)}))
		}
		if err := v.Err(); err != nil {
			return fw.Page[PaymentDTO]{}, err
		}
		page, err := s.Payments.FindPage(ctx, spec.And(parts...), fw.NewPageRequest(q.Page, q.Size, domain.PmtFieldDate.Asc()))
		if err != nil {
			return fw.Page[PaymentDTO]{}, err
		}
		return fw.MapPage(page, paymentDTO), nil
	})
}
