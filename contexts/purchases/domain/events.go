package domain

import (
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Identities of the context.
type (
	// InvoiceID identifies a received invoice.
	InvoiceID struct{ fw.UUID }
	// SupplierID identifies a supplier profile.
	SupplierID struct{ fw.UUID }
	// CounterID identifies a register counter.
	CounterID struct{ fw.UUID }
	// OrganizationID is the buying company, an internal organization of the Parties context.
	OrganizationID struct{ fw.UUID }
	// PartyID is the supplier, a party of the Parties context.
	PartyID struct{ fw.UUID }
)

// Events of the context.
type (
	// InvoiceRegistered is raised when a received invoice is booked, with its state.
	InvoiceRegistered struct {
		fw.EventMeta
		Snapshot InvoiceState `json:"snapshot"`
	}
	// InvoiceCancelled is raised when a received invoice is annulled.
	InvoiceCancelled struct {
		fw.EventMeta
		Company  string `json:"company"`
		Register string `json:"register"`
		Reason   string `json:"reason"`
	}
)

// EventType implementations.
func (InvoiceRegistered) EventType() string { return "purchases.invoice_registered" }
func (InvoiceCancelled) EventType() string  { return "purchases.invoice_cancelled" }

// New identities and parsing.
func NewInvoiceID() InvoiceID   { return InvoiceID{fw.NewUUID()} }
func NewSupplierID() SupplierID { return SupplierID{fw.NewUUID()} }
func NewCounterID() CounterID   { return CounterID{fw.NewUUID()} }

// ParseInvoiceID parses a textual identity.
func ParseInvoiceID(s string) (InvoiceID, error) { u, err := fw.ParseUUID(s); return InvoiceID{u}, err }

// ParseSupplierID parses a textual identity.
func ParseSupplierID(s string) (SupplierID, error) {
	u, err := fw.ParseUUID(s)
	return SupplierID{u}, err
}

// Repositories of the context.
type (
	InvoiceRepository  = fw.Repository[InvoiceID, *Invoice]
	SupplierRepository = fw.Repository[SupplierID, *SupplierProfile]
	CounterRepository  = fw.Repository[CounterID, *Counter]
)
