package domain

import (
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Identidades del contexto.
type (
	// InvoiceID identifica una factura recibida.
	InvoiceID struct{ fw.UUID }
	// SupplierID identifica un perfil de proveedor.
	SupplierID struct{ fw.UUID }
	// CounterID identifica un contador del registro.
	CounterID struct{ fw.UUID }
	// OrganizationID es la empresa compradora, una organización interna del contexto Parties.
	OrganizationID struct{ fw.UUID }
	// PartyID es el proveedor, una party del contexto Parties.
	PartyID struct{ fw.UUID }
)

// Eventos del contexto.
type (
	// InvoiceRegistered se lanza cuando se registra una factura recibida, con su estado.
	InvoiceRegistered struct {
		fw.EventMeta
		Snapshot InvoiceState `json:"snapshot"`
	}
	// InvoiceCancelled se lanza cuando se anula una factura recibida.
	InvoiceCancelled struct {
		fw.EventMeta
		Company  string `json:"company"`
		Register string `json:"register"`
		Reason   string `json:"reason"`
	}
)

// Implementaciones de EventType.
func (InvoiceRegistered) EventType() string { return "purchases.invoice_registered" }
func (InvoiceCancelled) EventType() string  { return "purchases.invoice_cancelled" }

// Identidades nuevas y análisis.
func NewInvoiceID() InvoiceID   { return InvoiceID{fw.NewUUID()} }
func NewSupplierID() SupplierID { return SupplierID{fw.NewUUID()} }
func NewCounterID() CounterID   { return CounterID{fw.NewUUID()} }

// ParseInvoiceID analiza una identidad textual.
func ParseInvoiceID(s string) (InvoiceID, error) { u, err := fw.ParseUUID(s); return InvoiceID{u}, err }

// ParseSupplierID analiza una identidad textual.
func ParseSupplierID(s string) (SupplierID, error) {
	u, err := fw.ParseUUID(s)
	return SupplierID{u}, err
}

// Repositorios del contexto.
type (
	InvoiceRepository  = fw.Repository[InvoiceID, *Invoice]
	SupplierRepository = fw.Repository[SupplierID, *SupplierProfile]
	CounterRepository  = fw.Repository[CounterID, *Counter]
)
