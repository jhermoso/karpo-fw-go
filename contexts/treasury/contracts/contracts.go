// Package contracts is what other bounded contexts may depend on: the Published Language of
// Treasury (Receivables registers the collections of settled direct debits and cancels the
// returned ones; Accounting posts the bank movements).
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
