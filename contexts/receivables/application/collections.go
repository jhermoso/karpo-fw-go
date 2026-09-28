package application

import (
	"context"
	"errors"

	"github.com/jhermoso/karpo-fw-go/contexts/receivables/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// GetReceivable loads the receivable of an invoice.
type GetReceivable struct{ ID domain.ReceivableID }

// SearchReceivables searches receivables of the caller's scope.
type SearchReceivables struct {
	Seller, Customer string
	OpenOnly         bool
	Page, Size       int
}

// InstallmentDTO is the transport form of an installment.
type InstallmentDTO struct {
	No        int    `json:"no"`
	Due       string `json:"due"`
	Amount    string `json:"amount"`
	Collected string `json:"collected"`
	Open      string `json:"open"`
}

// ReceivableDTO is the transport form of a receivable.
type ReceivableDTO struct {
	InvoiceID    string           `json:"invoiceId"`
	Number       string           `json:"number"`
	Seller       string           `json:"seller"`
	Customer     string           `json:"customer"`
	Issued       string           `json:"issued"`
	Total        string           `json:"total"`
	Open         string           `json:"open"`
	Settled      bool             `json:"settled"`
	Installments []InstallmentDTO `json:"installments"`
	Version      int64            `json:"version"`
}

func receivableDTO(r *domain.Receivable) ReceivableDTO {
	s := r.State()
	d := ReceivableDTO{InvoiceID: r.ID().String(), Number: s.Number, Seller: s.Seller.String(), Customer: s.Customer.String(), Issued: s.Issued.String(),
		Total: money(s.Total), Open: money(r.Open()), Settled: r.Settled(), Installments: []InstallmentDTO{}, Version: r.Version()}
	for _, i := range s.Installments {
		d.Installments = append(d.Installments, InstallmentDTO{No: i.No, Due: i.Due.String(), Amount: money(i.Amount), Collected: money(i.Collected), Open: money(i.Open())})
	}
	return d
}

// AllocationInput applies an amount to an installment of an invoice.
type AllocationInput struct {
	Invoice     string `json:"invoice"`
	Installment int    `json:"installment"`
	Amount      string `json:"amount"`
}

// RegisterCollection records money received from a customer, optionally allocated at once.
type RegisterCollection struct {
	Seller      string            `json:"seller"`
	Payer       string            `json:"payer"`
	Date        vocab.Date        `json:"date"`
	Amount      string            `json:"amount"`
	Method      string            `json:"method"`
	Reference   string            `json:"reference,omitempty"`
	Allocations []AllocationInput `json:"allocations,omitempty"`
}

// Allocate applies part of a collection to an installment.
type Allocate struct {
	ID domain.CollectionID `json:"-"`
	AllocationInput
}

// Deallocate reverses an allocation.
type Deallocate struct {
	ID         domain.CollectionID `json:"-"`
	Allocation string              `json:"allocation"`
}

// CancelCollection cancels a collection and reverses its allocations.
type CancelCollection struct {
	ID domain.CollectionID `json:"-"`
}

// OffsetCredit nets a credit (a corrective invoice) against an installment of an invoice of the
// same customer.
type OffsetCredit struct {
	Seller      string     `json:"seller"`
	Credit      string     `json:"credit"`
	Invoice     string     `json:"invoice"`
	Installment int        `json:"installment"`
	Amount      string     `json:"amount"`
	Date        vocab.Date `json:"date,omitzero"`
}

// GetCollection loads a collection.
type GetCollection struct{ ID domain.CollectionID }

// SearchCollections searches collections of the caller's scope.
type SearchCollections struct {
	Seller, Payer string
	Page, Size    int
}

// AllocationDTO is the transport form of an allocation.
type AllocationDTO struct {
	ID          string `json:"id"`
	Invoice     string `json:"invoice"`
	Installment int    `json:"installment"`
	Amount      string `json:"amount"`
	On          string `json:"on"`
}

// CollectionDTO is the transport form of a collection.
type CollectionDTO struct {
	ID          string          `json:"id"`
	Seller      string          `json:"seller"`
	Payer       string          `json:"payer"`
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

func collectionDTO(c *domain.Collection) CollectionDTO {
	s := c.State()
	d := CollectionDTO{ID: c.ID().String(), Seller: s.Seller.String(), Payer: s.Payer.String(), Date: s.Date.String(), Amount: money(s.Amount),
		Method: s.Method.String(), Reference: s.Reference, Allocated: money(c.Allocated()), Unallocated: money(c.Unallocated()), Cancelled: s.Cancelled,
		Allocations: []AllocationDTO{}, Version: c.Version()}
	for _, a := range s.Allocations {
		d.Allocations = append(d.Allocations, AllocationDTO{ID: a.ID.String(), Invoice: a.Receivable.String(), Installment: a.Installment,
			Amount: money(a.Amount), On: a.On.String()})
	}
	return d
}

// apply records an allocation on both sides: the collection (its amount) and the receivable (what
// is open). Both aggregates change in the caller's unit of work.
func (s service) apply(ctx context.Context, c *domain.Collection, in AllocationInput, on vocab.Date) error {
	var v fw.Validation
	rid := domain.ReceivableID{UUID: parseID(&v, "invoice", in.Invoice)}
	amount := parseDecimal(&v, "amount", in.Amount)
	if err := v.Err(); err != nil {
		return err
	}
	cs := c.State()
	_, err := s.receivables.Update(ctx, rid, func(_ context.Context, r *domain.Receivable) error {
		rs := r.State()
		if rs.Seller != cs.Seller {
			return fw.NotFound(domain.ReceivableKind, rid)
		}
		if rs.Customer != cs.Payer {
			return fw.Violation("receivables.other_customer", "the invoice is of another customer")
		}
		if _, err := c.Allocate(rid, in.Installment, amount, on); err != nil {
			return err
		}
		return r.Apply(in.Installment, amount)
	})
	return err
}

func (s service) revert(ctx context.Context, allocations []domain.Allocation) error {
	for _, a := range allocations {
		if _, err := s.receivables.Update(ctx, a.Receivable, func(_ context.Context, r *domain.Receivable) error {
			return r.Unapply(a.Installment, a.Amount)
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s service) receivableUseCases(svc *Service) {
	svc.GetReceivable = guard(PermReceivableRead, func(ctx context.Context, q GetReceivable) (ReceivableDTO, error) {
		r, err := s.Receivables.Get(ctx, q.ID)
		if err != nil {
			return ReceivableDTO{}, err
		}
		if err := scopeOf(ctx).check(domain.ReceivableKind, r.ID(), r.State().Seller, false); err != nil {
			return ReceivableDTO{}, err
		}
		return receivableDTO(r), nil
	})
	svc.SearchReceivables = guard(PermReceivableRead, func(ctx context.Context, q SearchReceivables) (fw.Page[ReceivableDTO], error) {
		var v fw.Validation
		parts := []spec.Specification[*domain.Receivable]{within(scopeOf(ctx), domain.RecFieldSeller)}
		if q.Seller != "" {
			parts = append(parts, domain.RecFieldSeller.Eq(domain.OrganizationID{UUID: parseID(&v, "seller", q.Seller)}))
		}
		if q.Customer != "" {
			parts = append(parts, domain.RecFieldCustomer.Eq(domain.PartyID{UUID: parseID(&v, "customer", q.Customer)}))
		}
		if q.OpenOnly {
			parts = append(parts, domain.RecFieldSettled.Eq(false))
		}
		if err := v.Err(); err != nil {
			return fw.Page[ReceivableDTO]{}, err
		}
		page, err := s.Receivables.FindPage(ctx, spec.And(parts...), fw.NewPageRequest(q.Page, q.Size, domain.RecFieldIssued.Asc()))
		if err != nil {
			return fw.Page[ReceivableDTO]{}, err
		}
		return fw.MapPage(page, receivableDTO), nil
	})
}

func (s service) collectionUseCases(svc *Service) {
	update := func(ctx context.Context, id domain.CollectionID, fn func(context.Context, *domain.Collection) error) (CollectionDTO, error) {
		sc := scopeOf(ctx)
		c, err := s.collections.Update(ctx, id, func(ctx context.Context, c *domain.Collection) error {
			if err := sc.check(domain.CollectionKind, c.ID(), c.State().Seller, true); err != nil {
				return err
			}
			return fn(ctx, c)
		})
		if err != nil {
			return CollectionDTO{}, err
		}
		return collectionDTO(c), nil
	}

	svc.RegisterCollection = guard(PermCollectionWrite, func(ctx context.Context, c RegisterCollection) (CollectionDTO, error) {
		var v fw.Validation
		seller := domain.OrganizationID{UUID: parseID(&v, "seller", c.Seller)}
		payer := domain.PartyID{UUID: parseID(&v, "payer", c.Payer)}
		method, ok := domain.ParseMethod(c.Method)
		v.Require(ok && method != domain.Offset, "method", "enum", "cash, transfer, card, check or direct-debit")
		amount := parseDecimal(&v, "amount", c.Amount)
		if err := v.Err(); err != nil {
			return CollectionDTO{}, err
		}
		if err := scopeOf(ctx).check("parties.party", seller, seller, true); err != nil {
			return CollectionDTO{}, err
		}
		col, err := domain.RegisterCollection(domain.NewCollectionID(), domain.CollectionState{Seller: seller, Payer: payer, Date: c.Date, Amount: amount,
			Currency: vocab.MustCurrencyCode("EUR"), Method: method, Reference: c.Reference})
		if err != nil {
			return CollectionDTO{}, err
		}
		for _, a := range c.Allocations {
			if err := s.apply(ctx, col, a, c.Date); err != nil {
				return CollectionDTO{}, err
			}
		}
		if err := s.collections.Create(ctx, col); err != nil {
			return CollectionDTO{}, err
		}
		return collectionDTO(col), nil
	}, retry[RegisterCollection, CollectionDTO](), pipeline.Transactional[RegisterCollection, CollectionDTO](s.UoW))

	svc.Allocate = guard(PermCollectionWrite, func(ctx context.Context, c Allocate) (CollectionDTO, error) {
		return update(ctx, c.ID, func(ctx context.Context, col *domain.Collection) error {
			if col.State().Method == domain.Offset {
				return fw.Violation("receivables.offset_allocation", "an offset is registered whole")
			}
			return s.apply(ctx, col, c.AllocationInput, vocab.DateOf(fw.Now()))
		})
	}, retry[Allocate, CollectionDTO](), pipeline.Transactional[Allocate, CollectionDTO](s.UoW))

	svc.Deallocate = guard(PermCollectionWrite, func(ctx context.Context, c Deallocate) (CollectionDTO, error) {
		var v fw.Validation
		id := domain.AllocationID{UUID: parseID(&v, "allocation", c.Allocation)}
		if err := v.Err(); err != nil {
			return CollectionDTO{}, err
		}
		return update(ctx, c.ID, func(ctx context.Context, col *domain.Collection) error {
			a, err := col.Deallocate(id)
			if err != nil {
				return err
			}
			return s.revert(ctx, []domain.Allocation{a})
		})
	}, retry[Deallocate, CollectionDTO](), pipeline.Transactional[Deallocate, CollectionDTO](s.UoW))

	svc.CancelCollection = guard(PermCollectionWrite, func(ctx context.Context, c CancelCollection) (CollectionDTO, error) {
		return update(ctx, c.ID, func(ctx context.Context, col *domain.Collection) error {
			back, err := col.Cancel()
			if err != nil {
				return err
			}
			return s.revert(ctx, back)
		})
	}, retry[CancelCollection, CollectionDTO](), pipeline.Transactional[CancelCollection, CollectionDTO](s.UoW))

	svc.Offset = guard(PermCollectionWrite, func(ctx context.Context, c OffsetCredit) (CollectionDTO, error) {
		var v fw.Validation
		seller := domain.OrganizationID{UUID: parseID(&v, "seller", c.Seller)}
		credit := domain.ReceivableID{UUID: parseID(&v, "credit", c.Credit)}
		amount := parseDecimal(&v, "amount", c.Amount)
		v.Require(amount.IsPositive(), "amount", "range", "a positive amount to net")
		if err := v.Err(); err != nil {
			return CollectionDTO{}, err
		}
		if err := scopeOf(ctx).check("parties.party", seller, seller, true); err != nil {
			return CollectionDTO{}, err
		}
		cr, err := s.Receivables.Get(ctx, credit)
		if err != nil {
			return CollectionDTO{}, err
		}
		if cr.State().Seller != seller {
			return CollectionDTO{}, fw.NotFound(domain.ReceivableKind, credit)
		}
		if !cr.State().Total.IsNegative() {
			return CollectionDTO{}, fw.Violation("receivables.not_a_credit", "only a credit (corrective invoice) is offset")
		}
		on := c.Date
		if on.IsZero() {
			on = vocab.DateOf(fw.Now())
		}
		col, err := domain.RegisterCollection(domain.NewCollectionID(), domain.CollectionState{Seller: seller, Payer: cr.State().Customer, Date: on,
			Currency: vocab.MustCurrencyCode("EUR"), Method: domain.Offset, Reference: "offset " + cr.State().Number})
		if err != nil {
			return CollectionDTO{}, err
		}
		if err := s.apply(ctx, col, AllocationInput{Invoice: c.Invoice, Installment: c.Installment, Amount: amount.String()}, on); err != nil {
			return CollectionDTO{}, err
		}
		if err := s.apply(ctx, col, AllocationInput{Invoice: credit.String(), Installment: 1, Amount: amount.Neg().String()}, on); err != nil {
			return CollectionDTO{}, err
		}
		if !col.Balanced() {
			return CollectionDTO{}, fw.Violation("receivables.offset_unbalanced", "an offset nets to zero")
		}
		if err := s.collections.Create(ctx, col); err != nil {
			return CollectionDTO{}, err
		}
		return collectionDTO(col), nil
	}, retry[OffsetCredit, CollectionDTO](), pipeline.Transactional[OffsetCredit, CollectionDTO](s.UoW))

	svc.GetCollection = guard(PermCollectionRead, func(ctx context.Context, q GetCollection) (CollectionDTO, error) {
		c, err := s.Collections.Get(ctx, q.ID)
		if err != nil {
			return CollectionDTO{}, err
		}
		if err := scopeOf(ctx).check(domain.CollectionKind, c.ID(), c.State().Seller, false); err != nil {
			return CollectionDTO{}, err
		}
		return collectionDTO(c), nil
	})

	svc.SearchCollections = guard(PermCollectionRead, func(ctx context.Context, q SearchCollections) (fw.Page[CollectionDTO], error) {
		var v fw.Validation
		parts := []spec.Specification[*domain.Collection]{within(scopeOf(ctx), domain.ColFieldSeller)}
		if q.Seller != "" {
			parts = append(parts, domain.ColFieldSeller.Eq(domain.OrganizationID{UUID: parseID(&v, "seller", q.Seller)}))
		}
		if q.Payer != "" {
			parts = append(parts, domain.ColFieldPayer.Eq(domain.PartyID{UUID: parseID(&v, "payer", q.Payer)}))
		}
		if err := v.Err(); err != nil {
			return fw.Page[CollectionDTO]{}, err
		}
		page, err := s.Collections.FindPage(ctx, spec.And(parts...), fw.NewPageRequest(q.Page, q.Size, domain.ColFieldDate.Asc()))
		if err != nil {
			return fw.Page[CollectionDTO]{}, err
		}
		return fw.MapPage(page, collectionDTO), nil
	})
}

// InvoiceIssued is the Receivables copy of billing.invoice-issued.v1: only the fields it needs.
type InvoiceIssued struct {
	InvoiceID string `json:"invoiceId"`
	Number    string `json:"number"`
	Seller    string `json:"seller"`
	Customer  string `json:"customer"`
	IssueDate string `json:"issueDate"`
	DueDate   string `json:"dueDate"`
	Currency  string `json:"currency"`
	Total     string `json:"total"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (InvoiceIssued) IntegrationEventType() string { return "billing.invoice-issued.v1" }

// Subscribe opens the receivable of each issued invoice (approved decision of docs/FACTURACION.md:
// Billing publishes, Receivables consumes). The schedule is the explicit due date of the invoice;
// else the terms of the customer's credit profile; else a single installment on the issue date.
// Credits (corrective invoices) are due at once. Redeliveries are harmless.
func Subscribe(c *messaging.Consumer, d Deps) {
	s := newOrchestrators(d)
	messaging.Handle(c, func(ctx context.Context, e InvoiceIssued, _ app.Envelope) error {
		id, err1 := fw.ParseUUID(e.InvoiceID)
		seller, err2 := fw.ParseUUID(e.Seller)
		customer, err3 := fw.ParseUUID(e.Customer)
		issued, err4 := vocab.ParseDate(e.IssueDate)
		total, err5 := vocab.ParseDecimal(e.Total)
		cur, err6 := vocab.NewCurrencyCode(e.Currency)
		if err := errors.Join(err1, err2, err3, err4, err5, err6); err != nil {
			return fw.Violation("receivables.invalid_event", "billing.invoice-issued.v1: "+err.Error())
		}
		rid := domain.ReceivableID{UUID: id}
		if _, err := s.Receivables.Get(ctx, rid); err == nil || !errors.Is(err, fw.ErrNotFound) {
			return err
		}
		if total.IsZero() {
			return nil
		}
		st := domain.ReceivableState{Seller: domain.OrganizationID{UUID: seller}, Customer: domain.PartyID{UUID: customer}, Number: e.Number,
			Issued: issued, Currency: cur, Total: total}
		schedule := []domain.Due{{Date: issued, Amount: total}}
		if due, err := vocab.ParseDate(e.DueDate); err == nil && e.DueDate != "" && total.IsPositive() {
			schedule = []domain.Due{{Date: due, Amount: total}}
		} else if total.IsPositive() {
			ps, err := s.Credit.Find(ctx, domain.CredFieldSeller.Eq(st.Seller).And(domain.CredFieldCustomer.Eq(st.Customer)))
			if err != nil {
				return err
			}
			if len(ps) > 0 && !ps[0].State().Terms.IsZero() {
				t, err := s.Terms.Get(ctx, ps[0].State().Terms)
				if err != nil {
					return err
				}
				if schedule, err = t.Schedule(ctx, issued, total, s.Calendar); err != nil {
					return err
				}
			}
		}
		r, err := domain.OpenReceivable(rid, st, schedule)
		if err != nil {
			return err
		}
		return s.receivables.Create(ctx, r)
	})
}
