// Package contracts is what other bounded contexts may depend on: the Published Language of
// Purchases (Payments owes what is paid to the supplier, Accounting posts the expense, the input
// tax and the withholding, Fiscal counts the professional withholdings of Modelo 111 and 190).
package contracts

// Source is the name of the publishing bounded context.
const Source = "purchases"

// Expense is the cost of an expense category of an invoice (its bases plus, when the tax is not
// deductible, its share of the tax).
type Expense struct {
	Category string `json:"category"` // goods | rent | repairs | professional-services | transport | insurance | advertising | supplies | other-services | fixed-asset
	Amount   string `json:"amount"`
}

// Tax is a line of the tax breakdown of an invoice.
type Tax struct {
	TaxCode       string `json:"taxCode,omitempty"`
	Treatment     string `json:"treatment,omitempty"`
	TreatmentKind string `json:"treatmentKind"` // subject | exempt | not-subject
	Rate          string `json:"rate"`
	Base          string `json:"base"`
	Amount        string `json:"amount"`
}

// PayTo is an account the invoice is paid to.
type PayTo struct {
	IBAN   string `json:"iban"`
	Amount string `json:"amount"`
}

// InvoiceRegisteredV1 is published when a received invoice is booked. Amounts are decimal strings
// in euros with two decimals (negative on a corrective invoice); Total = Net + Tax and
// Payable = Total - Withholding; the expenses plus DeductibleTax add up to Total.
type InvoiceRegisteredV1 struct {
	InvoiceID       string    `json:"invoiceId"`
	Company         string    `json:"company"`
	Supplier        string    `json:"supplier"`
	SupplierNumber  string    `json:"supplierNumber"`
	Register        string    `json:"register"`
	Issued          string    `json:"issued"`
	Received        string    `json:"received"`
	Due             string    `json:"due"`
	Corrects        string    `json:"corrects,omitempty"`
	Net             string    `json:"net"`
	Tax             string    `json:"tax"`
	DeductibleTax   string    `json:"deductibleTax"`
	Total           string    `json:"total"`
	WithholdingRate string    `json:"withholdingRate"`
	Withholding     string    `json:"withholding"`
	WithholdingKey  string    `json:"withholdingKey,omitempty"` // Modelo 190 perception key (G: professional activities)
	Payable         string    `json:"payable"`
	Expenses        []Expense `json:"expenses"`
	Taxes           []Tax     `json:"taxes"`
	PayTo           []PayTo   `json:"payTo,omitempty"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (InvoiceRegisteredV1) IntegrationEventType() string { return "purchases.invoice-registered.v1" }

// InvoiceCancelledV1 is published when a received invoice booked by mistake is annulled:
// consumers reverse what they did with its registration.
type InvoiceCancelledV1 struct {
	InvoiceID string `json:"invoiceId"`
	Company   string `json:"company"`
	Register  string `json:"register"`
	Reason    string `json:"reason"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (InvoiceCancelledV1) IntegrationEventType() string { return "purchases.invoice-cancelled.v1" }
