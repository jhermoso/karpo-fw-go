package domain

import (
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// InvoiceIssued se lanza cuando se emite una factura, con su estado congelado (el C# lanzaba un
// FactIssued genérico que nadie consumía).
type InvoiceIssued struct {
	fw.EventMeta
	Invoice InvoiceState `json:"invoice"`
}

// EventType implementa domain.Event.
func (InvoiceIssued) EventType() string { return "billing.invoice_issued" }

// DraftDiscarded se lanza cuando se descarta un borrador, con sus orígenes: lo que venía de ellos
// vuelve a pendiente de facturar (D-154, D-155).
type DraftDiscarded struct {
	fw.EventMeta
	Seller  string   `json:"seller"`
	Sources []Source `json:"sources"`
}

// EventType implementa domain.Event.
func (DraftDiscarded) EventType() string { return "billing.draft_discarded" }

// Identidades nuevas e interpretación.
func NewSeriesID() SeriesID   { return SeriesID{fw.NewUUID()} }
func NewInvoiceID() InvoiceID { return InvoiceID{fw.NewUUID()} }

// ParseSeriesID interpreta una identidad en texto.
func ParseSeriesID(s string) (SeriesID, error) { u, err := fw.ParseUUID(s); return SeriesID{u}, err }

// ParseInvoiceID interpreta una identidad en texto.
func ParseInvoiceID(s string) (InvoiceID, error) { u, err := fw.ParseUUID(s); return InvoiceID{u}, err }
