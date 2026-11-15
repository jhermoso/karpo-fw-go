package domain

import (
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
)

// PartialBillingPolicy dice si se factura parte de un pedido (D-153; Billing.PartialBillingPolicy
// de Trench). Se fija por empresa (Parties) y por cliente (Orders), y gana el cliente (D-144).
type PartialBillingPolicy string

// Políticas de facturación parcial.
const (
	// AllowPartial: cada albarán se factura en cuanto se emite, aunque su pedido tenga algo
	// pendiente de servir. Es la de siempre y la que rige sin ajuste.
	AllowPartial PartialBillingPolicy = "allow_partial"
	// RequireCompleteOrder: los albaranes de un pedido esperan a que no le quede nada pendiente de
	// servir.
	RequireCompleteOrder PartialBillingPolicy = "require_complete_order"
)

// ErrPartialBillingNotAllowed es el código de facturar, sin saltar la política, un albarán de un
// pedido que aún tiene algo pendiente de servir con require_complete_order: 422, el
// Orders.PartialBillingNotAllowed de Trench, en minúsculas como los demás códigos de Go.
const ErrPartialBillingNotAllowed = "orders.partial_billing_not_allowed"

// ErrAlreadyInvoiced es el código de pedir el borrador de un albarán pendiente que ya está en una
// factura.
const ErrAlreadyInvoiced = "billing.already_invoiced"

// ResolvePartialBilling es la política que rige: la del cliente, si la tiene; si no, la de la
// empresa; si tampoco, AllowPartial. Un valor desconocido no cuenta (Parties y Orders ya los
// validan): rechazarlo haría que el borrador de un albarán se reintentara sin fin.
func ResolvePartialBilling(customer, company PartialBillingPolicy) PartialBillingPolicy {
	for _, p := range []PartialBillingPolicy{customer, company} {
		if p == AllowPartial || p == RequireCompleteOrder {
			return p
		}
	}
	return AllowPartial
}

// SourceOrderKind es el nombre estable del tipo de agregado.
const SourceOrderKind = "billing.source_order"

// SourceOrderID es la identidad del albarán en Orders: un albarán sirve un pedido.
type SourceOrderID struct{ fw.UUID }

// SourceOrderState es el estado persistido: el pedido que sirve el albarán y si a ese pedido ya
// no le queda nada pendiente de servir.
type SourceOrderState struct {
	Seller   OrganizationID
	Order    fw.UUID
	Complete bool
}

// SourceOrder es lo que Billing sabe del pedido de un albarán (el invoicing_source_order de
// Trench): lo escribe al recibir el albarán, con su OrderComplete, y lo cambian los eventos de
// Orders de pedido servido entero o reabierto. Billing no lee las tablas de Orders.
type SourceOrder struct {
	fw.BaseAggregateRoot[SourceOrderID]
	s SourceOrderState
}

// ReconstituteSourceOrder reconstruye el pedido de un albarán.
func ReconstituteSourceOrder(id SourceOrderID, s SourceOrderState) (*SourceOrder, error) {
	base, err := fw.NewBaseAggregateRoot(SourceOrderKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(!s.Seller.IsZero() && !s.Order.IsZero(), "order", "required", "seller and order are required")
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &SourceOrder{BaseAggregateRoot: base, s: s}, nil
}

// State devuelve el estado.
func (o *SourceOrder) State() SourceOrderState { return o.s }

// SetComplete fija si al pedido ya no le queda nada pendiente de servir.
func (o *SourceOrder) SetComplete(complete bool) { o.s.Complete = complete }

// Campos del pedido de un albarán.
var (
	SoFieldOrder = spec.Comparable("order_id", func(o *SourceOrder) fw.UUID { return o.s.Order })
)

// SourceOrderRepository guarda el pedido de cada albarán.
type SourceOrderRepository = fw.Repository[SourceOrderID, *SourceOrder]
