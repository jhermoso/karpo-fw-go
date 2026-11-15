package application

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/jhermoso/karpo-fw-go/contexts/billing/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
)

// PermOverridePartialBilling deja facturar, en una operación concreta y con motivo, albaranes de
// un pedido que aún tiene algo pendiente de servir aunque la política sea require_complete_order
// (D-153). Es un permiso propio, aparte de preparar y editar borradores.
var PermOverridePartialBilling = authz.MustPermission("Billing.Invoice.OverridePartialBilling")

// MaxOverrideReason es lo que cabe en el motivo de saltar la política.
const MaxOverrideReason = 200

// OperationPartialBillingOverridden es la operación del registro de auditoría que deja el salto
// de la política, con su motivo y sus albaranes.
const OperationPartialBillingOverridden = "partial_billing_overridden"

// PartialBillingOverride salta, en la operación que lo lleva, la política de facturación parcial
// (D-153): exige PermOverridePartialBilling y un motivo, que queda en la auditoría de la factura.
// Sólo se mira si la política lo impide.
type PartialBillingOverride struct {
	OverridePartialBilling bool   `json:"overridePartialBilling,omitempty"`
	Reason                 string `json:"reason,omitempty"`
}

// DraftPendingDelivery prepara a petición el borrador de un albarán pendiente (D-146, D-153): el
// de un cliente mensual antes del cierre, o el de un pedido que aún tiene algo pendiente de
// servir. Si la política lo impide, 422 orders.partial_billing_not_allowed, salvo que se salte.
type DraftPendingDelivery struct {
	ID domain.PendingDeliveryID `json:"-"`
	PartialBillingOverride
}

// SalesOrderServingCompleted es la copia en Billing de orders.sales-order-serving-completed.v1.
type SalesOrderServingCompleted struct {
	OrderID string `json:"orderId"`
	Company string `json:"company"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (SalesOrderServingCompleted) IntegrationEventType() string {
	return "orders.sales-order-serving-completed.v1"
}

// SalesOrderServingReopened es la copia en Billing de orders.sales-order-serving-reopened.v1.
type SalesOrderServingReopened struct {
	OrderID string `json:"orderId"`
	Company string `json:"company"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (SalesOrderServingReopened) IntegrationEventType() string {
	return "orders.sales-order-serving-reopened.v1"
}

// partialPolicy es la política de facturación parcial de un cliente del vendedor: la del cliente
// en Orders, si la tiene; si no, la de la empresa; si no, allow_partial (D-153, D-144).
func (s service) partialPolicy(ctx context.Context, seller domain.OrganizationID, customer domain.PartyID) (domain.PartialBillingPolicy, error) {
	settings, err := s.settings(ctx, seller)
	if err != nil {
		return "", err
	}
	var own domain.PartialBillingPolicy
	if s.Terms != nil {
		t, err := s.Terms.Terms(ctx, seller, customer)
		if err != nil {
			return "", err
		}
		own = t.PartialBilling
	}
	return domain.ResolvePartialBilling(own, settings.PartialBilling), nil
}

// waiting dice si alguno de los albaranes es de un pedido que aún tiene algo pendiente de servir.
// Uno sin pedido conocido (de antes de D-153) no espera.
func (s service) waiting(ctx context.Context, deliveries []string) (bool, error) {
	for _, d := range deliveries {
		u, err := fw.ParseUUID(d)
		if err != nil {
			continue
		}
		o, err := s.SourceOrders.Get(ctx, domain.SourceOrderID{UUID: u})
		switch {
		case errors.Is(err, fw.ErrNotFound):
			continue
		case err != nil:
			return false, err
		case !o.State().Complete:
			return true, nil
		}
	}
	return false, nil
}

// billable dice si los albaranes de un cliente se pueden facturar ya: con allow_partial, siempre;
// con require_complete_order, si ninguno espera a su pedido.
func (s service) billable(ctx context.Context, seller domain.OrganizationID, customer domain.PartyID, deliveries []string) (bool, error) {
	policy, err := s.partialPolicy(ctx, seller, customer)
	if err != nil || policy != domain.RequireCompleteOrder {
		return err == nil, err
	}
	w, err := s.waiting(ctx, deliveries)
	return !w, err
}

// checkPartial aplica la política a una operación que pide facturar unos albaranes: si no se
// pueden facturar ya, 422 orders.partial_billing_not_allowed, salvo que la operación la salte con
// el permiso y un motivo. Devuelve el motivo del salto, si lo hubo, para la auditoría.
func (s service) checkPartial(ctx context.Context, seller domain.OrganizationID, customer domain.PartyID, deliveries []string,
	o PartialBillingOverride) (string, error) {
	ok, err := s.billable(ctx, seller, customer, deliveries)
	if err != nil || ok {
		return "", err
	}
	if !o.OverridePartialBilling {
		return "", fw.Violation(domain.ErrPartialBillingNotAllowed, "the partial billing policy requires the complete order")
	}
	if err := authz.Require(ctx, PermOverridePartialBilling); err != nil {
		return "", err
	}
	reason := strings.TrimSpace(o.Reason)
	if reason == "" || utf8.RuneCountInString(reason) > MaxOverrideReason {
		var v fw.Validation
		v.Add("reason", "required", "overriding the partial billing policy needs a reason of 1 to 200 characters")
		return "", v.Err()
	}
	return reason, nil
}

// auditOverride deja en la auditoría de la factura el salto de la política, con su motivo y sus
// albaranes.
func (s service) auditOverride(ctx context.Context, inv *domain.Invoice, reason string, deliveries []string) error {
	if s.Audit == nil || reason == "" {
		return nil
	}
	return s.Audit.Append(ctx, app.AuditRecord{ID: fw.NewUUID().String(), AggregateType: domain.InvoiceKind, AggregateID: inv.ID().String(),
		AggregateVersion: inv.Version(), Operation: OperationPartialBillingOverridden, Actor: app.ActorFrom(ctx), Channel: app.Channel(ctx),
		CorrelationID: app.CorrelationID(ctx), At: fw.Now(),
		Changes: traits.Diff(nil, map[string]any{"reason": reason, "deliveries": strings.Join(deliveries, ",")})})
}

// recordSourceOrder guarda el pedido de un albarán y si está servido entero. Un albarán sólo
// puede pasar el pedido de incompleto a completo, nunca al revés (eso sólo lo hace
// orders.sales-order-serving-reopened.v1): así no importa el orden de llegada de los mensajes ni
// que uno se entregue dos veces. Si el pedido ya consta servido entero (por otro albarán o por su
// evento, que pudo llegar antes), el albarán nace completo.
func (s service) recordSourceOrder(ctx context.Context, delivery fw.UUID, seller domain.OrganizationID, order fw.UUID, complete bool) error {
	if !complete {
		served, err := s.orderServed(ctx, order)
		if err != nil {
			return err
		}
		complete = served
	}
	id := domain.SourceOrderID{UUID: delivery}
	o, err := s.SourceOrders.Get(ctx, id)
	if errors.Is(err, fw.ErrNotFound) {
		if o, err = domain.ReconstituteSourceOrder(id, domain.SourceOrderState{Seller: seller, Order: order, Complete: complete}); err != nil {
			return err
		}
		return s.SourceOrders.Save(ctx, o)
	}
	if err != nil || o.State().Complete || !complete {
		return err
	}
	o.SetComplete(true)
	return s.SourceOrders.Save(ctx, o)
}

// orderServed dice si el pedido consta servido entero: su fila propia (la que dejan los eventos de
// servido y reabierto, con el id del pedido) o, si no la tiene, alguno de sus albaranes completo.
func (s service) orderServed(ctx context.Context, order fw.UUID) (bool, error) {
	rows, err := s.SourceOrders.Find(ctx, domain.SoFieldOrder.Eq(order))
	if err != nil {
		return false, err
	}
	found := false
	for _, r := range rows {
		if r.ID().UUID == order { // la fila del pedido manda
			return r.State().Complete, nil
		}
		found = found || r.State().Complete
	}
	return found, nil
}

// servingChanged registra que un pedido queda servido entero (complete) o deja de estarlo: en su
// fila propia (id = el pedido, para que un albarán que llegue después lo sepa) y en las de sus
// albaranes. Al quedar servido, los albaranes de clientes por albarán que esperaban por él tienen
// su borrador; los de clientes mensuales siguen pendientes del cierre, que ya los factura.
func (s service) servingChanged(ctx context.Context, seller domain.OrganizationID, order fw.UUID, complete bool) error {
	sources, err := s.SourceOrders.Find(ctx, domain.SoFieldOrder.Eq(order))
	if err != nil {
		return err
	}
	if !seller.IsZero() && !slices.ContainsFunc(sources, func(so *domain.SourceOrder) bool { return so.ID().UUID == order }) {
		own, err := domain.ReconstituteSourceOrder(domain.SourceOrderID{UUID: order}, domain.SourceOrderState{Seller: seller, Order: order, Complete: !complete})
		if err != nil {
			return err
		}
		sources = append(sources, own)
	}
	for _, so := range sources {
		if so.State().Complete != complete || so.Version() == 0 {
			so.SetComplete(complete)
			if err := s.SourceOrders.Save(ctx, so); err != nil {
				return err
			}
		}
	}
	if !complete {
		return nil
	}
	for _, so := range sources {
		if so.ID().UUID == order {
			continue
		}
		p, err := s.Pending.Get(ctx, domain.PendingDeliveryID(so.ID()))
		if errors.Is(err, fw.ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		st := p.State()
		if st.Periodicity != domain.PeriodicityPerDelivery || st.Drafted {
			continue
		}
		ok, err := s.billable(ctx, st.Seller, st.Customer, []string{p.ID().String()})
		if err != nil {
			return err
		}
		if !ok { // no puede pasar: el pedido acaba de quedar servido
			return fmt.Errorf("billing: the delivery note %s still waits for its order %s after it was served", p.ID(), order)
		}
		if _, err := s.draftPending(ctx, p); err != nil {
			return err
		}
	}
	return nil
}

// errAlreadyDrafted corta la marca de un albarán que otro ya ha facturado a la vez.
var errAlreadyDrafted = errors.New("billing: the delivery note is already drafted")

// draftPending prepara el borrador de un albarán pendiente y lo marca como facturado (D-154). Uno
// que ya lo estaba no se prepara otra vez: devuelve nil.
func (s service) draftPending(ctx context.Context, p *domain.PendingDelivery) (*domain.Invoice, error) {
	st := p.State()
	if st.Drafted {
		return nil, nil
	}
	if _, err := s.pending.Update(ctx, p.ID(), func(_ context.Context, p *domain.PendingDelivery) error {
		if p.State().Drafted {
			return errAlreadyDrafted
		}
		p.MarkDrafted()
		return nil
	}); errors.Is(err, errAlreadyDrafted) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	done, err := s.Invoices.Exists(ctx, domain.FromSource(DeliverySource, p.ID().String()))
	if err != nil || done {
		return nil, err
	}
	calc, err := s.deliveryCalculation(ctx, st.Seller, st.Customer)
	if err != nil {
		return nil, err
	}
	inv, err := s.periodDraft(ctx, st.Seller, st.Customer, calc, []*domain.PendingDelivery{p})
	if err != nil {
		return nil, err
	}
	if err := s.invoices.Create(ctx, inv); err != nil {
		return nil, err
	}
	return inv, nil
}

// partialBillingUseCases conecta la preparación a petición de un albarán pendiente.
func (s service) partialBillingUseCases(svc *Service) {
	svc.DraftPendingDelivery = guard(PermInvoiceCreate, func(ctx context.Context, c DraftPendingDelivery) (InvoiceDTO, error) {
		p, err := s.Pending.Get(ctx, c.ID)
		if err != nil {
			return InvoiceDTO{}, err
		}
		st := p.State()
		if err := scopeOf(ctx).check(domain.PendingDeliveryKind, p.ID(), st.Seller, true); err != nil {
			return InvoiceDTO{}, err
		}
		if err := s.notBlocked(ctx, st.Customer); err != nil {
			return InvoiceDTO{}, err
		}
		ids := []string{p.ID().String()}
		reason, err := s.checkPartial(ctx, st.Seller, st.Customer, ids, c.PartialBillingOverride)
		if err != nil {
			return InvoiceDTO{}, err
		}
		inv, err := s.draftPending(ctx, p)
		if err != nil {
			return InvoiceDTO{}, err
		}
		if inv == nil {
			return InvoiceDTO{}, fw.Violation(domain.ErrAlreadyInvoiced, "the delivery note is already in an invoice")
		}
		if err := s.auditOverride(ctx, inv, reason, ids); err != nil {
			return InvoiceDTO{}, err
		}
		return invoiceDTO(inv), nil
	}, retry[DraftPendingDelivery, InvoiceDTO](), pipeline.Transactional[DraftPendingDelivery, InvoiceDTO](s.UoW))
}

// subscribeServing recibe los eventos de Orders de pedido servido entero o reabierto (D-153).
func subscribeServing(c *messaging.Consumer, s service) {
	messaging.Handle(c, func(ctx context.Context, e SalesOrderServingCompleted, _ app.Envelope) error {
		order, err := fw.ParseUUID(e.OrderID)
		if err != nil || order.IsZero() {
			return fw.Violation("billing.invalid_event", "orders.sales-order-serving-completed.v1: "+e.OrderID)
		}
		return s.servingChanged(ctx, sellerOf(e.Company), order, true)
	})
	messaging.Handle(c, func(ctx context.Context, e SalesOrderServingReopened, _ app.Envelope) error {
		order, err := fw.ParseUUID(e.OrderID)
		if err != nil || order.IsZero() {
			return fw.Violation("billing.invalid_event", "orders.sales-order-serving-reopened.v1: "+e.OrderID)
		}
		return s.servingChanged(ctx, sellerOf(e.Company), order, false)
	})
}

// sellerOf es la empresa de un evento; cero si no la trae.
func sellerOf(company string) domain.OrganizationID {
	u, err := fw.ParseUUID(company)
	if err != nil {
		return domain.OrganizationID{}
	}
	return domain.OrganizationID{UUID: u}
}
