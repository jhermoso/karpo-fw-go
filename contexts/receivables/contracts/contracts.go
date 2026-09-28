// Package contracts is what other bounded contexts may depend on: the Published Language of
// Receivables and its credit port (Orders checks the risk of a customer before confirming, the
// C# never computed the outstanding debt).
package contracts

import "context"

// Source is the name of the publishing bounded context.
const Source = "receivables"

// Exposure is the credit situation of a customer with a seller. Amounts are decimal strings with
// two decimals.
type Exposure struct {
	Open      string `json:"open"`
	Overdue   string `json:"overdue"`
	Limited   bool   `json:"limited"`
	Limit     string `json:"limit,omitempty"`
	Available string `json:"available,omitempty"`
	Blocked   bool   `json:"blocked"`
}

// Credit answers the exposure of a customer with a seller on a civil date (overdue: due before it).
type Credit interface {
	Exposure(ctx context.Context, seller, customer, on string) (Exposure, error)
}

// CollectionAllocatedV1 is published when part of a collection is applied to an installment of an
// invoice (Accounting posts 572/430, or nets 430 against 430 for an offset).
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

// IntegrationEventType implements application.IntegrationEvent.
func (CollectionAllocatedV1) IntegrationEventType() string {
	return "receivables.collection-allocated.v1"
}

// AllocationReversedV1 is published when an allocation is reversed (or its collection cancelled).
type AllocationReversedV1 struct {
	CollectionID string `json:"collectionId"`
	Seller       string `json:"seller"`
	Payer        string `json:"payer"`
	InvoiceID    string `json:"invoiceId"`
	Installment  int    `json:"installment"`
	Amount       string `json:"amount"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (AllocationReversedV1) IntegrationEventType() string {
	return "receivables.allocation-reversed.v1"
}

// ReceivableSettledV1 is published when an invoice is fully collected (the C# seeded a Paid
// status that nothing ever set).
type ReceivableSettledV1 struct {
	InvoiceID string `json:"invoiceId"`
	Number    string `json:"number"`
	Customer  string `json:"customer"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (ReceivableSettledV1) IntegrationEventType() string { return "receivables.receivable-settled.v1" }

// DueItem is an open installment of an invoice.
type DueItem struct {
	InvoiceID   string `json:"invoiceId"`
	Number      string `json:"number"`
	Customer    string `json:"customer"`
	Installment int    `json:"installment"`
	Due         string `json:"due"`
	Open        string `json:"open"`
}

// Collectable answers the open installments of a seller due up to a civil date (Treasury builds
// its direct debit remittances from them).
type Collectable interface {
	DueItems(ctx context.Context, seller, dueTo string) ([]DueItem, error)
}
