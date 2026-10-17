// Package contracts es aquello de lo que pueden depender los demás contextos delimitados: el
// Published Language de Payments y su puerto para Treasury (las obligaciones vencidas que se pagan
// por transferencia).
package contracts

import "context"

// Source es el nombre del contexto delimitado que publica.
const Source = "payments"

// PaymentAllocatedV1 se publica cuando parte de un pago se aplica a una obligación (Accounting
// contabiliza la cuenta de la obligación contra el banco o la caja). Kind es supplier-invoice,
// payroll o tax; Payee es un id de party, o la administración tributaria (AEAT).
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

// IntegrationEventType implementa application.IntegrationEvent.
func (PaymentAllocatedV1) IntegrationEventType() string { return "payments.payment-allocated.v1" }

// AllocationReversedV1 se publica cuando una aplicación se revierte (o se anula su pago).
type AllocationReversedV1 struct {
	PaymentID string `json:"paymentId"`
	Company   string `json:"company"`
	Payee     string `json:"payee"`
	PayableID string `json:"payableId"`
	Kind      string `json:"kind"`
	Amount    string `json:"amount"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (AllocationReversedV1) IntegrationEventType() string { return "payments.allocation-reversed.v1" }

// PayableSettledV1 se publica cuando una obligación queda pagada del todo.
type PayableSettledV1 struct {
	PayableID string `json:"payableId"`
	Company   string `json:"company"`
	Kind      string `json:"kind"`
	Document  string `json:"document"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (PayableSettledV1) IntegrationEventType() string { return "payments.payable-settled.v1" }

// PayTo es una cuenta bancaria a la que se paga una obligación, y cuánto.
type PayTo struct {
	IBAN   string `json:"iban"`
	Amount string `json:"amount"`
}

// DueItem es una obligación que se paga por transferencia: sin pagar, con sus cuentas.
type DueItem struct {
	PayableID string  `json:"payableId"`
	Kind      string  `json:"kind"`
	Document  string  `json:"document"`
	Payee     string  `json:"payee"`
	Due       string  `json:"due"`
	Amount    string  `json:"amount"`
	PayTo     []PayTo `json:"payTo"`
}

// Payable responde lo que una empresa tiene que pagar por transferencia hasta una fecha de
// vencimiento: las obligaciones sin pagar nada que llevan sus cuentas, las de vencimiento más
// antiguo primero, y cuántas vencidas no tienen cuentas.
type Payable interface {
	DueForTransfer(ctx context.Context, company, dueTo string) (items []DueItem, withoutAccount int, err error)
}
