// Package contracts is what other bounded contexts may depend on: the Published Language of
// Billing (Fiscal keeps the VAT books and the Modelo 303 from it, Accounting posts the sale,
// Receivables opens the collection).
package contracts

// Source is the name of the publishing bounded context.
const Source = "billing"

// TaxLineV1 is a line of the tax breakdown of an issued invoice.
type TaxLineV1 struct {
	TaxType         string `json:"taxType,omitempty"`
	TaxCode         string `json:"taxCode,omitempty"`
	Treatment       string `json:"treatment,omitempty"`
	TreatmentKind   string `json:"treatmentKind"`
	Rate            string `json:"rate"`
	Base            string `json:"base"`
	Amount          string `json:"amount"`
	SurchargeRate   string `json:"surchargeRate,omitempty"`
	SurchargeAmount string `json:"surchargeAmount,omitempty"`
}

// InvoiceIssuedV1 is published when an invoice is issued. Amounts are decimal strings in the
// invoice currency with two decimals; dates are civil dates.
type InvoiceIssuedV1 struct {
	InvoiceID     string      `json:"invoiceId"`
	Number        string      `json:"number"`
	Kind          string      `json:"kind"` // ordinary | corrective
	Corrects      string      `json:"corrects,omitempty"`
	Reason        string      `json:"reason,omitempty"` // R1–R5
	Seller        string      `json:"seller"`
	SellerNIF     string      `json:"sellerNif"`
	Customer      string      `json:"customer"`
	CustomerNIF   string      `json:"customerNif"`
	CustomerName  string      `json:"customerName"`
	IssueDate     string      `json:"issueDate"`
	OperationDate string      `json:"operationDate,omitempty"`
	DueDate       string      `json:"dueDate,omitempty"`
	Currency      string      `json:"currency"`
	Country       string      `json:"country"` // tax jurisdiction
	Net           string      `json:"net"`
	Tax           string      `json:"tax"`
	Surcharge     string      `json:"surcharge"`
	Total         string      `json:"total"`
	Taxes         []TaxLineV1 `json:"taxes"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (InvoiceIssuedV1) IntegrationEventType() string { return "billing.invoice-issued.v1" }
