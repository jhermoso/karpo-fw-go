package domain

import (
	"context"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Identities of the context.
type (
	// PayableID identifies a payable.
	PayableID struct{ fw.UUID }
	// PaymentID identifies a payment.
	PaymentID struct{ fw.UUID }
	// AllocationID identifies an allocation.
	AllocationID struct{ fw.UUID }
	// OrganizationID is the paying company, an internal organization of the Parties context.
	OrganizationID struct{ fw.UUID }
	// PartyID is the payee, a party of the Parties context.
	PartyID struct{ fw.UUID }
)

// Events of the context (the C# raised a generic FactIssued for payments that nobody consumed).
type (
	// PayableRegistered is raised when an obligation is recorded.
	PayableRegistered struct {
		fw.EventMeta
		Kind     string `json:"kind"`
		Document string `json:"document"`
		Amount   string `json:"amount"`
		Due      string `json:"due"`
	}
	// PayableSettled is raised when nothing remains to pay.
	PayableSettled struct {
		fw.EventMeta
		Company  string `json:"company"`
		Kind     string `json:"kind"`
		Document string `json:"document"`
	}
	// PayableCancelled is raised when an unpaid obligation is withdrawn.
	PayableCancelled struct {
		fw.EventMeta
		Kind     string `json:"kind"`
		Document string `json:"document"`
		Reason   string `json:"reason"`
	}
	// PaymentRegistered is raised when money is paid out.
	PaymentRegistered struct {
		fw.EventMeta
		Payee  string `json:"payee"`
		Amount string `json:"amount"`
		Method string `json:"method"`
	}
	// PaymentAllocated is raised when part of a payment is applied to a payable.
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
	// AllocationReversed is raised when an allocation is reversed (or its payment cancelled).
	AllocationReversed struct {
		fw.EventMeta
		Company string `json:"company"`
		Payee   string `json:"payee"`
		Payable string `json:"payable"`
		Kind    string `json:"kind"`
		Amount  string `json:"amount"`
	}
)

// EventType implementations.
func (PayableRegistered) EventType() string  { return "payments.payable_registered" }
func (PayableSettled) EventType() string     { return "payments.payable_settled" }
func (PayableCancelled) EventType() string   { return "payments.payable_cancelled" }
func (PaymentRegistered) EventType() string  { return "payments.payment_registered" }
func (PaymentAllocated) EventType() string   { return "payments.payment_allocated" }
func (AllocationReversed) EventType() string { return "payments.allocation_reversed" }

// New identities and parsing.
func NewPayableID() PayableID { return PayableID{fw.NewUUID()} }
func NewPaymentID() PaymentID { return PaymentID{fw.NewUUID()} }

// ParsePayableID parses a textual identity.
func ParsePayableID(s string) (PayableID, error) { u, err := fw.ParseUUID(s); return PayableID{u}, err }

// ParsePaymentID parses a textual identity.
func ParsePaymentID(s string) (PaymentID, error) { u, err := fw.ParseUUID(s); return PaymentID{u}, err }

// ParseAllocationID parses a textual identity.
func ParseAllocationID(s string) (AllocationID, error) {
	u, err := fw.ParseUUID(s)
	return AllocationID{u}, err
}

// Repositories of the context.
type (
	PayableRepository = fw.Repository[PayableID, *Payable]
	PaymentRepository = fw.Repository[PaymentID, *Payment]
)

// NetPaySplits answers how the net pay of approved payslips is split across bank accounts (the
// Payroll Remittance port, through an adapter). Payslips without splits are absent.
type NetPaySplits interface {
	NetPayments(ctx context.Context, payslipIDs []string) (map[string][]PayTo, error)
}
