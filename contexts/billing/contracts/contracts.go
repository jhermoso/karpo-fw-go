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
	InvoiceID string `json:"invoiceId"`
	Number    string `json:"number"`
	Kind      string `json:"kind"` // ordinary | corrective
	Corrects  string `json:"corrects,omitempty"`
	// SourceType y SourceID son el primer documento del que se preparó (orders.delivery), para los
	// consumidores de antes de D-146; Sources los lleva todos.
	SourceType   string     `json:"sourceType,omitempty"`
	SourceID     string     `json:"sourceId,omitempty"`
	Sources      []SourceV1 `json:"sources,omitempty"`
	Reason       string     `json:"reason,omitempty"` // R1–R5
	Seller       string     `json:"seller"`
	SellerNIF    string     `json:"sellerNif"`
	Customer     string     `json:"customer"`
	CustomerNIF  string     `json:"customerNif"`
	CustomerName string     `json:"customerName"`
	// Los domicilios fiscales congelados al emitir; nil si Parties no tenía ninguno.
	SellerAddress   *AddressV1  `json:"sellerAddress,omitempty"`
	CustomerAddress *AddressV1  `json:"customerAddress,omitempty"`
	IssueDate       string      `json:"issueDate"`
	OperationDate   string      `json:"operationDate,omitempty"`
	DueDate         string      `json:"dueDate,omitempty"`
	Currency        string      `json:"currency"`
	Country         string      `json:"country"` // jurisdicción fiscal
	Net             string      `json:"net"`
	Tax             string      `json:"tax"`
	Surcharge       string      `json:"surcharge"`
	Total           string      `json:"total"`
	Taxes           []TaxLineV1 `json:"taxes"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (InvoiceIssuedV1) IntegrationEventType() string { return "billing.invoice-issued.v1" }

// SourceV1 es un documento del que se preparó una factura: un albarán de Orders (orders.delivery),
// con su Id y su número.
type SourceV1 struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Ref  string `json:"ref,omitempty"`
}

// AddressV1 es un domicilio fiscal tal como se congela en la factura.
type AddressV1 struct {
	Street     string `json:"street"`
	PostalCode string `json:"postalCode,omitempty"`
	Locality   string `json:"locality,omitempty"`
	Region     string `json:"region,omitempty"`
	Country    string `json:"country,omitempty"`
}
