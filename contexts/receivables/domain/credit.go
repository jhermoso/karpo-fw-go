package domain

import (
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// CreditProfileKind es el nombre estable del tipo de agregado.
const CreditProfileKind = "receivables.credit_profile"

// CreditProfileState es el estado persistido de un perfil de crédito.
type CreditProfileState struct {
	Seller   OrganizationID
	Customer PartyID
	Terms    TermsID       // condiciones de pago por defecto; cero: vence en la fecha de la factura
	Limit    vocab.Decimal // límite de riesgo; sólo tiene sentido cuando Limited
	Limited  bool          // false: sin límite (el C# leía MaxRisk = 0 como «sin límite» en un sitio y como «límite 0» en otro)
	Blocked  bool          // se rechazan los pedidos
	Audit    traits.AuditStamp
}

// CreditProfile es la relación de crédito de un vendedor con un cliente: condiciones por defecto,
// límite de riesgo y bloqueo (el MaxRisk, BlockOrders y DefaultPaymentCondition del perfil comercial
// de cliente del C#).
type CreditProfile struct {
	fw.BaseAggregateRoot[CreditProfileID]
	traits.Audited
	s CreditProfileState
}

func (s CreditProfileState) check(v *fw.Validation) {
	v.Require(!s.Seller.IsZero() && !s.Customer.IsZero(), "customer", "required", "seller and customer are required")
	v.Require(!s.Limited || (!s.Limit.IsNegative() && s.Limit.Equal(s.Limit.Round(2))), "limit", "range", "a non-negative limit in cents")
	v.Require(s.Limited || s.Limit.IsZero(), "limit", "unlimited", "an unlimited profile has no limit amount")
}

// ReconstituteCreditProfile reconstruye un perfil de crédito.
func ReconstituteCreditProfile(id CreditProfileID, s CreditProfileState) (*CreditProfile, error) {
	base, err := fw.NewBaseAggregateRoot(CreditProfileKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	s.check(&v)
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &CreditProfile{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// State devuelve el estado.
func (p *CreditProfile) State() CreditProfileState { return p.s }

// Set sustituye las condiciones, el límite y el bloqueo.
func (p *CreditProfile) Set(terms TermsID, limited bool, limit vocab.Decimal, blocked bool) error {
	s := p.s
	s.Terms, s.Limited, s.Limit, s.Blocked = terms, limited, limit, blocked
	var v fw.Validation
	s.check(&v)
	if err := v.Err(); err != nil {
		return err
	}
	p.s = s
	return nil
}

// Available devuelve lo que aún se puede vender a crédito dada la exposición abierta (ok false: sin
// límite).
func (p *CreditProfile) Available(exposure vocab.Decimal) (vocab.Decimal, bool) {
	if !p.s.Limited {
		return vocab.Decimal{}, false
	}
	return p.s.Limit.Sub(exposure), true
}

// AuditSnapshot implementa traits.Snapshotter.
func (p *CreditProfile) AuditSnapshot() map[string]any {
	return map[string]any{"terms": p.s.Terms.String(), "limited": p.s.Limited, "limit": p.s.Limit.String(), "blocked": p.s.Blocked}
}

// Campos del perfil de crédito.
var (
	CredFieldSeller   = spec.Comparable("seller", func(p *CreditProfile) OrganizationID { return p.s.Seller })
	CredFieldCustomer = spec.Comparable("customer", func(p *CreditProfile) PartyID { return p.s.Customer })
)

// Eventos del contexto (el C# lanzaba un FactIssued genérico para los pagos que nadie consumía).
type (
	// ReceivableOpened se lanza cuando una factura emitida pasa a ser un cobro pendiente.
	ReceivableOpened struct {
		fw.EventMeta
		Customer string `json:"customer"`
		Number   string `json:"number"`
		Total    string `json:"total"`
	}
	// ReceivableSettled se lanza cuando no queda nada por cobrar.
	ReceivableSettled struct {
		fw.EventMeta
		Customer string `json:"customer"`
		Number   string `json:"number"`
	}
	// CollectionRegistered se lanza cuando se recibe dinero.
	CollectionRegistered struct {
		fw.EventMeta
		Payer  string `json:"payer"`
		Amount string `json:"amount"`
		Method string `json:"method"`
	}
	// CollectionAllocated se lanza cuando parte de un cobro se aplica a un vencimiento.
	CollectionAllocated struct {
		fw.EventMeta
		Seller      string `json:"seller"`
		Payer       string `json:"payer"`
		Method      string `json:"method"`
		Receivable  string `json:"receivable"`
		Installment int    `json:"installment"`
		Amount      string `json:"amount"`
		On          string `json:"on"`
	}
	// AllocationReversed se lanza cuando se deshace una aplicación.
	AllocationReversed struct {
		fw.EventMeta
		Seller      string `json:"seller"`
		Payer       string `json:"payer"`
		Receivable  string `json:"receivable"`
		Installment int    `json:"installment"`
		Amount      string `json:"amount"`
	}
)

// Implementaciones de EventType.
func (ReceivableOpened) EventType() string     { return "receivables.receivable_opened" }
func (ReceivableSettled) EventType() string    { return "receivables.receivable_settled" }
func (CollectionRegistered) EventType() string { return "receivables.collection_registered" }
func (CollectionAllocated) EventType() string  { return "receivables.collection_allocated" }
func (AllocationReversed) EventType() string   { return "receivables.allocation_reversed" }

// Identidades nuevas y análisis.
func NewTermsID() TermsID                 { return TermsID{fw.NewUUID()} }
func NewCollectionID() CollectionID       { return CollectionID{fw.NewUUID()} }
func NewCreditProfileID() CreditProfileID { return CreditProfileID{fw.NewUUID()} }

// ParseTermsID analiza una identidad textual.
func ParseTermsID(s string) (TermsID, error) { u, err := fw.ParseUUID(s); return TermsID{u}, err }

// ParseReceivableID analiza una identidad textual.
func ParseReceivableID(s string) (ReceivableID, error) {
	u, err := fw.ParseUUID(s)
	return ReceivableID{u}, err
}

// ParseCollectionID analiza una identidad textual.
func ParseCollectionID(s string) (CollectionID, error) {
	u, err := fw.ParseUUID(s)
	return CollectionID{u}, err
}

// ParseCreditProfileID analiza una identidad textual.
func ParseCreditProfileID(s string) (CreditProfileID, error) {
	u, err := fw.ParseUUID(s)
	return CreditProfileID{u}, err
}
