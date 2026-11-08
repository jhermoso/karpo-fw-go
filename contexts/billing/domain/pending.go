package domain

import (
	"slices"
	"strings"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// PendingDeliveryKind es el nombre estable del tipo de agregado.
const PendingDeliveryKind = "billing.pending_delivery"

// PendingDeliveryID identifica un albarán pendiente de facturar: es la identidad del albarán en
// Orders, así que un albarán no queda pendiente dos veces.
type PendingDeliveryID struct{ fw.UUID }

// PendingDeliveryState es el estado persistido de un albarán pendiente.
type PendingDeliveryState struct {
	Seller      OrganizationID
	Customer    PartyID
	Number      string // el número del albarán
	OrderNumber string
	Date        vocab.Date
	Lines       []LineInput // con su precio, su descuento (D-149) y su código de impuesto
	Audit       traits.AuditStamp
}

// PendingDelivery es un albarán de un cliente con facturación periódica (D-146) que espera al
// cierre del periodo: Billing guarda lo que necesita para facturarlo, y el cierre lo pasa a un
// borrador con los demás albaranes del cliente y lo borra.
type PendingDelivery struct {
	fw.BaseAggregateRoot[PendingDeliveryID]
	traits.Audited
	s PendingDeliveryState
}

// ReconstitutePendingDelivery reconstruye un albarán pendiente.
func ReconstitutePendingDelivery(id PendingDeliveryID, s PendingDeliveryState) (*PendingDelivery, error) {
	base, err := fw.NewBaseAggregateRoot(PendingDeliveryKind, id)
	if err != nil {
		return nil, err
	}
	s.Number, s.OrderNumber = strings.TrimSpace(s.Number), strings.TrimSpace(s.OrderNumber)
	var v fw.Validation
	v.Require(!s.Seller.IsZero() && !s.Customer.IsZero(), "customer", "required", "seller and customer are required")
	v.Require(s.Number != "" && len(s.Number) <= 40 && len(s.OrderNumber) <= 40, "number", "length", "a delivery number of 1 to 40 characters")
	v.Require(!s.Date.IsZero(), "date", "required", "the delivery date is required")
	v.Require(len(s.Lines) > 0 && len(s.Lines) <= MaxLines, "lines", "count", "1 to 500 lines")
	for _, l := range s.Lines {
		v.Require(strings.TrimSpace(l.Description) != "" && utf8.RuneCountInString(l.Description) <= 500, "lines", "description",
			"a description of 1 to 500 characters")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	s.Lines = slices.Clone(s.Lines)
	return &PendingDelivery{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// State devuelve el estado (las líneas son una copia).
func (p *PendingDelivery) State() PendingDeliveryState {
	s := p.s
	s.Lines = slices.Clone(s.Lines)
	return s
}

// AuditSnapshot implementa traits.Snapshotter.
func (p *PendingDelivery) AuditSnapshot() map[string]any {
	return map[string]any{"number": p.s.Number, "date": p.s.Date.String(), "lines": len(p.s.Lines)}
}

// Campos del albarán pendiente.
var (
	PdFieldSeller   = spec.Comparable("seller", func(p *PendingDelivery) OrganizationID { return p.s.Seller })
	PdFieldCustomer = spec.Comparable("customer", func(p *PendingDelivery) PartyID { return p.s.Customer })
	PdFieldDate     = spec.OrderedBy("delivery_date", func(p *PendingDelivery) vocab.Date { return p.s.Date }, vocab.CompareDates)
	PdFieldNumber   = spec.Ordered("delivery_number", func(p *PendingDelivery) string { return p.s.Number })
)

// ParsePendingDeliveryID interpreta una identidad en texto.
func ParsePendingDeliveryID(s string) (PendingDeliveryID, error) {
	u, err := fw.ParseUUID(s)
	return PendingDeliveryID{u}, err
}
