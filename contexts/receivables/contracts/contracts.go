// Package contracts es aquello de lo que pueden depender otros contextos delimitados: el Published
// Language de Receivables y su puerto de crédito (Orders comprueba el riesgo de un cliente antes de
// confirmar; el C# nunca calculaba la deuda pendiente).
package contracts

import "context"

// Source es el nombre del contexto delimitado que publica.
const Source = "receivables"

// Exposure es la situación de crédito de un cliente con un vendedor. Los importes son cadenas
// decimales con dos decimales.
type Exposure struct {
	Open      string `json:"open"`
	Overdue   string `json:"overdue"`
	Limited   bool   `json:"limited"`
	Limit     string `json:"limit,omitempty"`
	Available string `json:"available,omitempty"`
	Blocked   bool   `json:"blocked"`
}

// Credit responde la exposición de un cliente con un vendedor en una fecha civil (vencido: lo que
// vence antes de ella).
type Credit interface {
	Exposure(ctx context.Context, seller, customer, on string) (Exposure, error)
}

// CollectionAllocatedV1 se publica cuando parte de un cobro se aplica a un vencimiento de una
// factura (Accounting contabiliza 572/430, o compensa 430 contra 430 en una compensación).
type CollectionAllocatedV1 struct {
	CollectionID string `json:"collectionId"`
	Seller       string `json:"seller"`
	Payer        string `json:"payer"`
	Method       string `json:"method"`
	InvoiceID    string `json:"invoiceId"`
	Installment  int    `json:"installment"`
	Amount       string `json:"amount"`
	On           string `json:"on"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (CollectionAllocatedV1) IntegrationEventType() string {
	return "receivables.collection-allocated.v1"
}

// AllocationReversedV1 se publica cuando se deshace una aplicación (o se anula su cobro).
type AllocationReversedV1 struct {
	CollectionID string `json:"collectionId"`
	Seller       string `json:"seller"`
	Payer        string `json:"payer"`
	InvoiceID    string `json:"invoiceId"`
	Installment  int    `json:"installment"`
	Amount       string `json:"amount"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (AllocationReversedV1) IntegrationEventType() string {
	return "receivables.allocation-reversed.v1"
}

// ReceivableSettledV1 se publica cuando una factura está cobrada del todo (el C# sembraba un
// estado Paid que nada fijaba nunca).
type ReceivableSettledV1 struct {
	InvoiceID string `json:"invoiceId"`
	Number    string `json:"number"`
	Customer  string `json:"customer"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (ReceivableSettledV1) IntegrationEventType() string { return "receivables.receivable-settled.v1" }

// DueItem es un vencimiento abierto de una factura.
type DueItem struct {
	InvoiceID   string `json:"invoiceId"`
	Number      string `json:"number"`
	Customer    string `json:"customer"`
	Installment int    `json:"installment"`
	Due         string `json:"due"`
	Open        string `json:"open"`
}

// Collectable responde los vencimientos abiertos de un vendedor que vencen hasta una fecha civil
// (Treasury construye con ellos sus remesas de adeudos directos).
type Collectable interface {
	DueItems(ctx context.Context, seller, dueTo string) ([]DueItem, error)
}
