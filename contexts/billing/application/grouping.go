package application

import (
	"context"
	"fmt"
	"slices"

	"github.com/jhermoso/karpo-fw-go/contexts/billing/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// MergeDrafts une en el primero varios borradores de albaranes del mismo vendedor y cliente (D-146):
// se queda con las líneas y los orígenes de todos, y los demás desaparecen. Es el único modo de
// quitar un borrador de albarán.
type MergeDrafts struct {
	Invoices []string `json:"invoices"`
}

// ClosePeriod cierra el periodo de facturación de un vendedor (D-146): prepara un borrador por
// cliente con todos sus albaranes pendientes hasta UpTo, incluido; sólo los de Customer, si se da.
// Un segundo cierre no encuentra nada pendiente y no prepara nada. No hay tarea programada: el
// cierre se lanza por su ruta.
type ClosePeriod struct {
	Seller   string     `json:"seller"`
	Customer string     `json:"customer,omitempty"`
	UpTo     vocab.Date `json:"upTo"`
}

func (s service) groupingUseCases(svc *Service) {
	svc.MergeDrafts = guard(PermInvoiceUpdate, func(ctx context.Context, c MergeDrafts) (InvoiceDTO, error) {
		var v fw.Validation
		ids := make([]domain.InvoiceID, 0, len(c.Invoices))
		for _, raw := range c.Invoices {
			id := domain.InvoiceID{UUID: parseID(&v, "invoices", raw)}
			v.Require(!slices.Contains(ids, id), "invoices", "unique", "each draft once")
			ids = append(ids, id)
		}
		if err := v.Err(); err != nil {
			return InvoiceDTO{}, err
		}
		if len(ids) < 2 {
			return InvoiceDTO{}, fw.Violation("billing.merge_too_few", "at least two drafts are merged")
		}
		sc := scopeOf(ctx)
		others := make([]*domain.Invoice, 0, len(ids)-1)
		for _, id := range ids[1:] {
			o, err := s.Invoices.Get(ctx, id)
			if err != nil {
				return InvoiceDTO{}, err
			}
			if err := sc.check(domain.InvoiceKind, o.ID(), o.State().Seller, true); err != nil {
				return InvoiceDTO{}, err
			}
			others = append(others, o)
		}
		var all []domain.Source
		inv, err := s.invoices.Update(ctx, ids[0], func(ctx context.Context, i *domain.Invoice) error {
			if err := sc.check(domain.InvoiceKind, i.ID(), i.State().Seller, true); err != nil {
				return err
			}
			all = i.State().Sources
			for _, o := range others {
				all = append(all, o.State().Sources...)
			}
			for _, src := range all {
				if src.Type != DeliverySource {
					return fw.Violation("billing.merge_unsourced", "only drafts prepared from delivery notes are merged")
				}
			}
			if err := i.Merge(others, deliveryDescription("", "", all)); err != nil {
				return err
			}
			// Los demás se borran antes de guardar éste: un albarán no está en dos facturas.
			for _, o := range others {
				if err := s.invoices.Delete(ctx, o.ID(), func(_ context.Context, d *domain.Invoice) error {
					if d.Version() != o.Version() {
						return fmt.Errorf("%w: billing: the draft %s changed while it was merged", fw.ErrConflict, o.ID())
					}
					return nil
				}); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return InvoiceDTO{}, err
		}
		return invoiceDTO(inv), nil
	}, retry[MergeDrafts, InvoiceDTO](), pipeline.Transactional[MergeDrafts, InvoiceDTO](s.UoW))

	svc.ClosePeriod = guard(PermInvoiceCreate, func(ctx context.Context, c ClosePeriod) ([]InvoiceDTO, error) {
		var v fw.Validation
		seller := domain.OrganizationID{UUID: parseID(&v, "seller", c.Seller)}
		parts := []spec.Specification[*domain.PendingDelivery]{domain.PdFieldSeller.Eq(seller), domain.PdFieldDate.Le(c.UpTo)}
		if c.Customer != "" {
			parts = append(parts, domain.PdFieldCustomer.Eq(domain.PartyID{UUID: parseID(&v, "customer", c.Customer)}))
		}
		v.Require(!c.UpTo.IsZero(), "upTo", "required", "the last day of the period is required")
		if err := v.Err(); err != nil {
			return nil, err
		}
		if err := scopeOf(ctx).check("parties.party", seller, seller, true); err != nil {
			return nil, err
		}
		pending, err := s.Pending.Find(ctx, spec.And(parts...), domain.PdFieldDate.Asc(), domain.PdFieldNumber.Asc())
		if err != nil {
			return nil, err
		}
		// Un borrador por cliente, en el orden de su primer albarán.
		var customers []domain.PartyID
		byCustomer := map[domain.PartyID][]*domain.PendingDelivery{}
		for _, p := range pending {
			cu := p.State().Customer
			if _, ok := byCustomer[cu]; !ok {
				customers = append(customers, cu)
			}
			byCustomer[cu] = append(byCustomer[cu], p)
		}
		out := []InvoiceDTO{}
		for _, cu := range customers {
			var sources []domain.Source
			var lines []domain.LineInput
			var date vocab.Date
			var first domain.PendingDeliveryState
			for k, p := range byCustomer[cu] {
				st := p.State()
				if k == 0 {
					first = st
				}
				sources = append(sources, domain.Source{Type: DeliverySource, ID: p.ID().String(), Ref: st.Number})
				lines = append(lines, st.Lines...)
				date = st.Date // la del último albarán: el final del periodo que cubre
				if err := s.pending.Delete(ctx, p.ID(), nil); err != nil {
					return nil, err
				}
			}
			inv, err := s.deliveryDraft(ctx, seller, cu, deliveryDescription(first.Number, first.OrderNumber, sources), date, sources, lines)
			if err != nil {
				return nil, err
			}
			if err := s.invoices.Create(ctx, inv); err != nil {
				return nil, err
			}
			out = append(out, invoiceDTO(inv))
		}
		return out, nil
	}, retry[ClosePeriod, []InvoiceDTO](), pipeline.Transactional[ClosePeriod, []InvoiceDTO](s.UoW))
}
