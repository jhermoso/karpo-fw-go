package application

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jhermoso/karpo-fw-go/contexts/billing/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
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

// ClosePeriod cierra el periodo de facturación de un vendedor (D-146): prepara para cada cliente un
// borrador con todos sus albaranes pendientes hasta UpTo, incluido (varios si pasan de MaxLines
// líneas); sólo los de Customer, si se da. Un segundo cierre no encuentra nada pendiente y no
// prepara nada. No hay tarea programada: el cierre se lanza por su ruta.
type ClosePeriod struct {
	Seller   string     `json:"seller"`
	Customer string     `json:"customer,omitempty"`
	UpTo     vocab.Date `json:"upTo"`
}

// ClosePeriodResult son los borradores que preparó el cierre y los clientes que no pudo cerrar,
// cuyos albaranes siguen pendientes.
type ClosePeriodResult struct {
	Drafts []InvoiceDTO   `json:"drafts"`
	Failed []CloseFailure `json:"failed"`
}

// CloseFailure es un cliente que el cierre no pudo cerrar, con el código del error.
type CloseFailure struct {
	Customer string `json:"customer"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

// SearchPendingDeliveries lista los albaranes pendientes del cierre del periodo.
type SearchPendingDeliveries struct {
	Seller, Customer string
	Page, Size       int
}

// PendingDeliveryDTO es un albarán pendiente del cierre del periodo. Net es la suma de sus líneas,
// antes de impuestos.
type PendingDeliveryDTO struct {
	Delivery    string `json:"delivery"`
	Seller      string `json:"seller"`
	Customer    string `json:"customer"`
	Number      string `json:"number"`
	OrderNumber string `json:"orderNumber,omitempty"`
	Date        string `json:"date"`
	Lines       int    `json:"lines"`
	Net         string `json:"net"`
}

func (s service) groupingUseCases(svc *Service) {
	closeOne := s.closeCustomerHandler()
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

	svc.ClosePeriod = guard(PermInvoiceCreate, func(ctx context.Context, c ClosePeriod) (ClosePeriodResult, error) {
		var v fw.Validation
		seller := domain.OrganizationID{UUID: parseID(&v, "seller", c.Seller)}
		parts := []spec.Specification[*domain.PendingDelivery]{domain.PdFieldSeller.Eq(seller), domain.PdFieldDate.Le(c.UpTo)}
		if c.Customer != "" {
			parts = append(parts, domain.PdFieldCustomer.Eq(domain.PartyID{UUID: parseID(&v, "customer", c.Customer)}))
		}
		v.Require(!c.UpTo.IsZero(), "upTo", "required", "the last day of the period is required")
		if err := v.Err(); err != nil {
			return ClosePeriodResult{}, err
		}
		if err := scopeOf(ctx).check("parties.party", seller, seller, true); err != nil {
			return ClosePeriodResult{}, err
		}
		pending, err := s.Pending.Find(ctx, spec.And(parts...), domain.PdFieldDate.Asc(), domain.PdFieldNumber.Asc())
		if err != nil {
			return ClosePeriodResult{}, err
		}
		// Cliente a cliente, en el orden de su primer albarán, cada uno en su unidad de trabajo: el
		// que falla no impide el cierre de los demás y sus albaranes siguen pendientes.
		var customers []domain.PartyID
		for _, p := range pending {
			if cu := p.State().Customer; !slices.Contains(customers, cu) {
				customers = append(customers, cu)
			}
		}
		out := ClosePeriodResult{Drafts: []InvoiceDTO{}, Failed: []CloseFailure{}}
		for _, cu := range customers {
			invs, err := closeOne.Handle(ctx, closeCustomer{seller: seller, customer: cu, upTo: c.UpTo})
			if err != nil {
				out.Failed = append(out.Failed, CloseFailure{Customer: cu.String(), Code: failureCode(err), Message: err.Error()})
				continue
			}
			for _, inv := range invs {
				out.Drafts = append(out.Drafts, invoiceDTO(inv))
			}
		}
		return out, nil
	})

	svc.SearchPendingDeliveries = guard(PermInvoiceRead, func(ctx context.Context, q SearchPendingDeliveries) (fw.Page[PendingDeliveryDTO], error) {
		var v fw.Validation
		parts := []spec.Specification[*domain.PendingDelivery]{within(scopeOf(ctx), domain.PdFieldSeller)}
		if q.Seller != "" {
			parts = append(parts, domain.PdFieldSeller.Eq(domain.OrganizationID{UUID: parseID(&v, "seller", q.Seller)}))
		}
		if q.Customer != "" {
			parts = append(parts, domain.PdFieldCustomer.Eq(domain.PartyID{UUID: parseID(&v, "customer", q.Customer)}))
		}
		if err := v.Err(); err != nil {
			return fw.Page[PendingDeliveryDTO]{}, err
		}
		page, err := s.Pending.FindPage(ctx, spec.And(parts...), fw.NewPageRequest(q.Page, q.Size, domain.PdFieldDate.Asc(), domain.PdFieldNumber.Asc()))
		if err != nil {
			return fw.Page[PendingDeliveryDTO]{}, err
		}
		return fw.MapPage(page, pendingDTO), nil
	})
}

// closeCustomer es el cierre del periodo de un cliente.
type closeCustomer struct {
	seller   domain.OrganizationID
	customer domain.PartyID
	upTo     vocab.Date
}

// closeCustomerHandler prepara, en una unidad de trabajo, los borradores del periodo de un cliente:
// lee sus albaranes pendientes dentro de la transacción y los borra antes de facturarlos. Uno que ya
// está en una factura (llegó otra vez después de facturado) se borra sin más; uno que otro cierre
// borró a la vez no está (NotFound) y se salta: ese cierre lo factura. Si los dos cierres chocan
// de otro modo (la versión del pendiente o el índice único de los orígenes), es un conflicto y se
// reintenta, ya sin nada pendiente.
func (s service) closeCustomerHandler() app.Handler[closeCustomer, []*domain.Invoice] {
	fn := func(ctx context.Context, c closeCustomer) ([]*domain.Invoice, error) {
		pending, err := s.Pending.Find(ctx, spec.And(domain.PdFieldSeller.Eq(c.seller), domain.PdFieldCustomer.Eq(c.customer),
			domain.PdFieldDate.Le(c.upTo)), domain.PdFieldDate.Asc(), domain.PdFieldNumber.Asc())
		if err != nil {
			return nil, err
		}
		var take []*domain.PendingDelivery
		for _, p := range pending {
			done, err := s.Invoices.Exists(ctx, domain.FromSource(DeliverySource, p.ID().String()))
			if err != nil {
				return nil, err
			}
			err = s.pending.Delete(ctx, p.ID(), nil)
			switch {
			case errors.Is(err, fw.ErrNotFound):
				continue
			case err != nil:
				return nil, err
			case !done:
				take = append(take, p)
			}
		}
		// Un borrador tiene como mucho MaxLines líneas: los albaranes que no caben van a otro, sin
		// partir ninguno (un albarán pendiente tiene como mucho MaxLines).
		var out []*domain.Invoice
		for len(take) > 0 {
			n, lines := 0, 0
			for n < len(take) && lines+len(take[n].State().Lines) <= domain.MaxLines {
				lines += len(take[n].State().Lines)
				n++
			}
			inv, err := s.periodDraft(ctx, c.seller, c.customer, take[:n])
			if err != nil {
				return nil, err
			}
			if err := s.invoices.Create(ctx, inv); err != nil {
				return nil, err
			}
			out = append(out, inv)
			take = take[n:]
		}
		return out, nil
	}
	return app.Chain[closeCustomer, []*domain.Invoice](app.HandlerFunc[closeCustomer, []*domain.Invoice](fn),
		retry[closeCustomer, []*domain.Invoice](), pipeline.Transactional[closeCustomer, []*domain.Invoice](s.UoW))
}

// periodDraft prepara el borrador de unos albaranes pendientes de un cliente, con la fecha de
// operación del último: el final del periodo que cubre.
func (s service) periodDraft(ctx context.Context, seller domain.OrganizationID, customer domain.PartyID, ps []*domain.PendingDelivery) (*domain.Invoice, error) {
	var sources []domain.Source
	var lines []domain.LineInput
	first, last := ps[0].State(), ps[len(ps)-1].State()
	for _, p := range ps {
		st := p.State()
		sources = append(sources, domain.Source{Type: DeliverySource, ID: p.ID().String(), Ref: st.Number})
		lines = append(lines, st.Lines...)
	}
	return s.deliveryDraft(ctx, seller, customer, deliveryDescription(first.Number, first.OrderNumber, sources), last.Date, sources, lines)
}

// failureCode es el código del error de un cliente en el cierre: el de la regla que incumple; el
// de una línea que la factura no admite; o, si es otra cosa, billing.close_failed.
func failureCode(err error) string {
	var rv *fw.RuleViolationError
	switch {
	case errors.As(err, &rv):
		return rv.Code
	case errors.Is(err, fw.ErrValidation):
		return "billing.close_invalid_line"
	}
	return "billing.close_failed"
}

func pendingDTO(p *domain.PendingDelivery) PendingDeliveryDTO {
	s := p.State()
	net := vocab.DecimalFromInt(0)
	for _, l := range s.Lines {
		net = net.Add(l.Quantity.Mul(l.UnitPrice).Round(2))
	}
	return PendingDeliveryDTO{Delivery: p.ID().String(), Seller: s.Seller.String(), Customer: s.Customer.String(), Number: s.Number,
		OrderNumber: s.OrderNumber, Date: s.Date.String(), Lines: len(s.Lines), Net: money(net)}
}
