// Package contracts es aquello de lo que pueden depender los demás contextos delimitados: el
// Published Language de Treasury (Receivables registra los cobros de los adeudos liquidados y
// anula los devueltos; Payments registra los pagos de las transferencias ejecutadas y anula los de
// las rechazadas; Accounting contabiliza los movimientos bancarios).
package contracts

// Source es el nombre del contexto delimitado que publica.
const Source = "treasury"

// DirectDebitCollectedV1 se publica por cada adeudo de una remesa que cobró el banco.
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

// IntegrationEventType implementa application.IntegrationEvent.
func (DirectDebitCollectedV1) IntegrationEventType() string {
	return "treasury.direct-debit-collected.v1"
}

// DirectDebitReturnedV1 se publica cuando el banco del deudor devuelve un adeudo.
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

// IntegrationEventType implementa application.IntegrationEvent.
func (DirectDebitReturnedV1) IntegrationEventType() string {
	return "treasury.direct-debit-returned.v1"
}

// TransferExecutedV1 se publica por cada transferencia de una orden que ejecutó el banco. Payee es
// un id de party, o la administración tributaria.
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

// IntegrationEventType implementa application.IntegrationEvent.
func (TransferExecutedV1) IntegrationEventType() string { return "treasury.transfer-executed.v1" }

// TransferRejectedV1 se publica cuando el banco del beneficiario rechaza o devuelve una
// transferencia.
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

// IntegrationEventType implementa application.IntegrationEvent.
func (TransferRejectedV1) IntegrationEventType() string { return "treasury.transfer-rejected.v1" }
