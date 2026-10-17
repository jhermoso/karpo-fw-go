// Package contracts es aquello de lo que pueden depender otros contextos delimitados: el Published
// Language de Billing (Fiscal lleva con él los libros de IVA y el Modelo 303, Accounting contabiliza
// la venta, Receivables abre el cobro).
package contracts

// Source es el nombre del contexto delimitado que publica.
const Source = "billing"

// TaxLineV1 es una línea del desglose de impuestos de una factura emitida.
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

// InvoiceIssuedV1 se publica cuando se emite una factura. Los importes son cadenas decimales en la
// moneda de la factura con dos decimales; las fechas son fechas civiles.
type InvoiceIssuedV1 struct {
	InvoiceID     string      `json:"invoiceId"`
	Number        string      `json:"number"`
	Kind          string      `json:"kind"` // ordinary | corrective
	Corrects      string      `json:"corrects,omitempty"`
	SourceType    string      `json:"sourceType,omitempty"` // el documento del que se preparó (orders.delivery)
	SourceID      string      `json:"sourceId,omitempty"`
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
	Country       string      `json:"country"` // jurisdicción fiscal
	Net           string      `json:"net"`
	Tax           string      `json:"tax"`
	Surcharge     string      `json:"surcharge"`
	Total         string      `json:"total"`
	Taxes         []TaxLineV1 `json:"taxes"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (InvoiceIssuedV1) IntegrationEventType() string { return "billing.invoice-issued.v1" }
