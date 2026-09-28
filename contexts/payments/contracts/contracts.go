// Package contracts is what other bounded contexts may depend on: the Published Language of
// Payments and its port for Treasury (the payables due to pay by transfer).
package contracts

import "context"

// Source is the name of the publishing bounded context.
const Source = "payments"

// PaymentAllocatedV1 is published when part of a payment is applied to a payable (Accounting
// posts the payable account against the bank or cash). Kind is supplier-invoice, payroll or tax;
// Payee is a party id, or the tax authority (AEAT).
type PaymentAllocatedV1 struct {
	PaymentID string `json:"paymentId"`
	Company   string `json:"company"`
	Payee     string `json:"payee"`
	Method    string `json:"method"`
	PayableID string `json:"payableId"`
	Kind      string `json:"kind"`
	Amount    string `json:"amount"`
	On        string `json:"on"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (PaymentAllocatedV1) IntegrationEventType() string { return "payments.payment-allocated.v1" }

// AllocationReversedV1 is published when an allocation is reversed (or its payment cancelled).
type AllocationReversedV1 struct {
	PaymentID string `json:"paymentId"`
	Company   string `json:"company"`
	Payee     string `json:"payee"`
	PayableID string `json:"payableId"`
	Kind      string `json:"kind"`
	Amount    string `json:"amount"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (AllocationReversedV1) IntegrationEventType() string { return "payments.allocation-reversed.v1" }

// PayableSettledV1 is published when an obligation is fully paid.
type PayableSettledV1 struct {
	PayableID string `json:"payableId"`
	Company   string `json:"company"`
	Kind      string `json:"kind"`
	Document  string `json:"document"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (PayableSettledV1) IntegrationEventType() string { return "payments.payable-settled.v1" }

// PayTo is a bank account a payable is paid to, and how much.
type PayTo struct {
	IBAN   string `json:"iban"`
	Amount string `json:"amount"`
}

// DueItem is a payable to pay by transfer: unpaid, with its accounts.
type DueItem struct {
	PayableID string  `json:"payableId"`
	Kind      string  `json:"kind"`
	Document  string  `json:"document"`
	Payee     string  `json:"payee"`
	Due       string  `json:"due"`
	Amount    string  `json:"amount"`
	PayTo     []PayTo `json:"payTo"`
}

// Payable answers what a company has to pay by transfer up to a due date: the payables not paid
// at all that carry their accounts, oldest due first, and how many due ones lack accounts.
type Payable interface {
	DueForTransfer(ctx context.Context, company, dueTo string) (items []DueItem, withoutAccount int, err error)
}
