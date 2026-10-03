// Package contracts is what other bounded contexts may depend on: the Published Language of
// Treasury (Receivables registers the collections of settled direct debits and cancels the
// returned ones; Payments registers the payments of executed transfers and cancels the rejected
// ones; Accounting posts the bank movements).
package contracts

// Source is the name of the publishing bounded context.
const Source = "treasury"

// DirectDebitCollectedV1 is published for each direct debit of a remittance the bank charged.
type DirectDebitCollectedV1 struct {
	RemittanceID string `json:"remittanceId"`
	EndToEnd     string `json:"endToEnd"`
	Creditor     string `json:"creditor"`
	Debtor       string `json:"debtor"`
	InvoiceID    string `json:"invoiceId"`
	Installment  int    `json:"installment"`
	Amount       string `json:"amount"`
	CollectedOn  string `json:"collectedOn"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (DirectDebitCollectedV1) IntegrationEventType() string {
	return "treasury.direct-debit-collected.v1"
}

// DirectDebitReturnedV1 is published when a direct debit is returned by the debtor's bank.
type DirectDebitReturnedV1 struct {
	RemittanceID string `json:"remittanceId"`
	EndToEnd     string `json:"endToEnd"`
	Creditor     string `json:"creditor"`
	Debtor       string `json:"debtor"`
	InvoiceID    string `json:"invoiceId"`
	Installment  int    `json:"installment"`
	Amount       string `json:"amount"`
	ReturnedOn   string `json:"returnedOn"`
	Reason       string `json:"reason"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (DirectDebitReturnedV1) IntegrationEventType() string {
	return "treasury.direct-debit-returned.v1"
}

// TransferExecutedV1 is published for each credit transfer of an order the bank executed. Payee
// is a party id, or the tax authority.
type TransferExecutedV1 struct {
	OrderID    string `json:"orderId"`
	EndToEnd   string `json:"endToEnd"`
	Debtor     string `json:"debtor"`
	Payee      string `json:"payee"`
	PayableID  string `json:"payableId"`
	IBAN       string `json:"iban"`
	Amount     string `json:"amount"`
	ExecutedOn string `json:"executedOn"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (TransferExecutedV1) IntegrationEventType() string { return "treasury.transfer-executed.v1" }

// TransferRejectedV1 is published when a transfer is rejected or returned by the payee's bank.
type TransferRejectedV1 struct {
	OrderID    string `json:"orderId"`
	EndToEnd   string `json:"endToEnd"`
	Debtor     string `json:"debtor"`
	Payee      string `json:"payee"`
	PayableID  string `json:"payableId"`
	Amount     string `json:"amount"`
	RejectedOn string `json:"rejectedOn"`
	Reason     string `json:"reason"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (TransferRejectedV1) IntegrationEventType() string { return "treasury.transfer-rejected.v1" }
