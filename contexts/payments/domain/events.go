package domain

import (
	"context"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Identidades del contexto.
type (
	// PayableID identifica una obligación de pago.
	PayableID struct{ fw.UUID }
	// PaymentID identifica un pago.
	PaymentID struct{ fw.UUID }
	// AllocationID identifica una aplicación.
	AllocationID struct{ fw.UUID }
	// OrganizationID es la empresa que paga, una organización interna del contexto Parties.
	OrganizationID struct{ fw.UUID }
	// PartyID es el beneficiario, una party del contexto Parties.
	PartyID struct{ fw.UUID }
)

// Eventos del contexto (el C# lanzaba para los pagos un FactIssued genérico que nadie consumía).
type (
	// PayableRegistered se lanza al registrar una obligación.
	PayableRegistered struct {
		fw.EventMeta
		Kind     string `json:"kind"`
		Document string `json:"document"`
		Amount   string `json:"amount"`
		Due      string `json:"due"`
	}
	// PayableSettled se lanza cuando no queda nada por pagar.
	PayableSettled struct {
		fw.EventMeta
		Company  string `json:"company"`
		Kind     string `json:"kind"`
		Document string `json:"document"`
	}
	// PayableCancelled se lanza al retirar una obligación sin pagar.
	PayableCancelled struct {
		fw.EventMeta
		Kind     string `json:"kind"`
		Document string `json:"document"`
		Reason   string `json:"reason"`
	}
	// PaymentRegistered se lanza cuando sale dinero en un pago.
	PaymentRegistered struct {
		fw.EventMeta
		Payee  string `json:"payee"`
		Amount string `json:"amount"`
		Method string `json:"method"`
	}
	// PaymentAllocated se lanza cuando parte de un pago se aplica a una obligación.
	PaymentAllocated struct {
		fw.EventMeta
		Company string `json:"company"`
		Payee   string `json:"payee"`
		Method  string `json:"method"`
		Payable string `json:"payable"`
		Kind    string `json:"kind"`
		Amount  string `json:"amount"`
		On      string `json:"on"`
	}
	// AllocationReversed se lanza cuando una aplicación se revierte (o se anula su pago).
	AllocationReversed struct {
		fw.EventMeta
		Company string `json:"company"`
		Payee   string `json:"payee"`
		Payable string `json:"payable"`
		Kind    string `json:"kind"`
		Amount  string `json:"amount"`
	}
)

// Implementaciones de EventType.
func (PayableRegistered) EventType() string  { return "payments.payable_registered" }
func (PayableSettled) EventType() string     { return "payments.payable_settled" }
func (PayableCancelled) EventType() string   { return "payments.payable_cancelled" }
func (PaymentRegistered) EventType() string  { return "payments.payment_registered" }
func (PaymentAllocated) EventType() string   { return "payments.payment_allocated" }
func (AllocationReversed) EventType() string { return "payments.allocation_reversed" }

// Identidades nuevas e interpretación.
func NewPayableID() PayableID { return PayableID{fw.NewUUID()} }
func NewPaymentID() PaymentID { return PaymentID{fw.NewUUID()} }

// ParsePayableID interpreta una identidad en texto.
func ParsePayableID(s string) (PayableID, error) { u, err := fw.ParseUUID(s); return PayableID{u}, err }

// ParsePaymentID interpreta una identidad en texto.
func ParsePaymentID(s string) (PaymentID, error) { u, err := fw.ParseUUID(s); return PaymentID{u}, err }

// ParseAllocationID interpreta una identidad en texto.
func ParseAllocationID(s string) (AllocationID, error) {
	u, err := fw.ParseUUID(s)
	return AllocationID{u}, err
}

// Repositorios del contexto.
type (
	PayableRepository = fw.Repository[PayableID, *Payable]
	PaymentRepository = fw.Repository[PaymentID, *Payment]
)

// NetPaySplits responde cómo se reparte el líquido de las nóminas aprobadas entre cuentas
// bancarias (el puerto Remittance de Payroll, a través de un adaptador). Las nóminas sin reparto no
// aparecen.
type NetPaySplits interface {
	NetPayments(ctx context.Context, payslipIDs []string) (map[string][]PayTo, error)
}
