package domain

import (
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// InvoiceIssued is raised when an invoice is issued, with its frozen state (the C# raised a
// generic FactIssued that nobody consumed).
type InvoiceIssued struct {
	fw.EventMeta
	Invoice InvoiceState `json:"invoice"`
}

// EventType implements domain.Event.
func (InvoiceIssued) EventType() string { return "billing.invoice_issued" }

// New identities and parsing.
func NewSeriesID() SeriesID   { return SeriesID{fw.NewUUID()} }
func NewInvoiceID() InvoiceID { return InvoiceID{fw.NewUUID()} }

// ParseSeriesID parses a textual identity.
func ParseSeriesID(s string) (SeriesID, error) { u, err := fw.ParseUUID(s); return SeriesID{u}, err }

// ParseInvoiceID parses a textual identity.
func ParseInvoiceID(s string) (InvoiceID, error) { u, err := fw.ParseUUID(s); return InvoiceID{u}, err }
