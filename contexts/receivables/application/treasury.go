package application

import (
	"context"
	"errors"

	"github.com/jhermoso/karpo-fw-go/contexts/receivables/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/receivables/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// DueItemsPort implements contracts.Collectable (it serves Treasury, not users).
type DueItemsPort struct{ Receivables domain.ReceivableRepository }

var _ contracts.Collectable = DueItemsPort{}

// DueItems implements contracts.Collectable: the open installments (positive, of invoices not
// settled) due up to a date, oldest first.
func (p DueItemsPort) DueItems(ctx context.Context, seller, dueTo string) ([]contracts.DueItem, error) {
	var v fw.Validation
	sid := domain.OrganizationID{UUID: parseID(&v, "seller", seller)}
	to, err := vocab.ParseDate(dueTo)
	v.Require(err == nil, "dueTo", "format", "a date YYYY-MM-DD is required")
	if err := v.Err(); err != nil {
		return nil, err
	}
	rs, err := p.Receivables.Find(ctx, spec.And(domain.RecFieldSeller.Eq(sid), domain.RecFieldSettled.Eq(false)), domain.RecFieldIssued.Asc())
	if err != nil {
		return nil, err
	}
	out := []contracts.DueItem{}
	for _, r := range rs {
		s := r.State()
		for _, i := range s.Installments {
			if i.Open().IsPositive() && !i.Due.After(to) {
				out = append(out, contracts.DueItem{InvoiceID: r.ID().String(), Number: s.Number, Customer: s.Customer.String(), Installment: i.No,
					Due: i.Due.String(), Open: money(i.Open())})
			}
		}
	}
	return out, nil
}

// DirectDebitCollected is the Receivables copy of treasury.direct-debit-collected.v1.
type DirectDebitCollected struct {
	EndToEnd    string `json:"endToEnd"`
	Creditor    string `json:"creditor"`
	Debtor      string `json:"debtor"`
	InvoiceID   string `json:"invoiceId"`
	Installment int    `json:"installment"`
	Amount      string `json:"amount"`
	CollectedOn string `json:"collectedOn"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (DirectDebitCollected) IntegrationEventType() string {
	return "treasury.direct-debit-collected.v1"
}

// DirectDebitReturned is the Receivables copy of treasury.direct-debit-returned.v1.
type DirectDebitReturned struct {
	EndToEnd string `json:"endToEnd"`
	Creditor string `json:"creditor"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (DirectDebitReturned) IntegrationEventType() string { return "treasury.direct-debit-returned.v1" }

// SubscribeTreasury registers the reactions to Treasury (approved decision 5 of docs/COBROS.md:
// Receivables publishes, Treasury executes): a collected direct debit becomes a collection by
// direct debit allocated to its installment (left unallocated if the installment was collected
// meanwhile, for a person to decide); a returned one cancels that collection.
func SubscribeTreasury(c *messaging.Consumer, d Deps) {
	s := newOrchestrators(d)
	byReference := func(ctx context.Context, creditor domain.OrganizationID, ref string) ([]*domain.Collection, error) {
		return s.Collections.Find(ctx, spec.And(domain.ColFieldSeller.Eq(creditor), domain.ColFieldReference.Eq(ref)))
	}
	messaging.Handle(c, func(ctx context.Context, e DirectDebitCollected, _ app.Envelope) error {
		creditor, err1 := fw.ParseUUID(e.Creditor)
		debtor, err2 := fw.ParseUUID(e.Debtor)
		on, err3 := vocab.ParseDate(e.CollectedOn)
		amount, err4 := vocab.ParseDecimal(e.Amount)
		if err := errors.Join(err1, err2, err3, err4); err != nil {
			return fw.Violation("receivables.invalid_event", "treasury.direct-debit-collected.v1: "+err.Error())
		}
		seller := domain.OrganizationID{UUID: creditor}
		if found, err := byReference(ctx, seller, e.EndToEnd); err != nil || len(found) > 0 {
			return err
		}
		col, err := domain.RegisterCollection(domain.NewCollectionID(), domain.CollectionState{Seller: seller, Payer: domain.PartyID{UUID: debtor},
			Date: on, Amount: amount, Currency: vocab.MustCurrencyCode("EUR"), Method: domain.DirectDebit, Reference: e.EndToEnd})
		if err != nil {
			return err
		}
		if err := s.apply(ctx, col, AllocationInput{Invoice: e.InvoiceID, Installment: e.Installment, Amount: e.Amount}, on); err != nil &&
			!errors.Is(err, fw.ErrRuleViolation) && !errors.Is(err, fw.ErrNotFound) {
			return err
		}
		return s.collections.Create(ctx, col)
	})
	messaging.Handle(c, func(ctx context.Context, e DirectDebitReturned, _ app.Envelope) error {
		creditor, err := fw.ParseUUID(e.Creditor)
		if err != nil {
			return fw.Violation("receivables.invalid_event", "treasury.direct-debit-returned.v1 without creditor")
		}
		found, err := byReference(ctx, domain.OrganizationID{UUID: creditor}, e.EndToEnd)
		if err != nil {
			return err
		}
		for _, col := range found {
			if col.State().Cancelled {
				continue
			}
			if _, err := s.collections.Update(ctx, col.ID(), func(ctx context.Context, col *domain.Collection) error {
				back, err := col.Cancel()
				if err != nil {
					return err
				}
				return s.revert(ctx, back)
			}); err != nil {
				return err
			}
		}
		return nil
	})
}
