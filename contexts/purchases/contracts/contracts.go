// Package contracts es aquello de lo que pueden depender otros contextos delimitados: el Published
// Language de Purchases (Payments debe lo que se paga al proveedor, Accounting contabiliza el gasto,
// el impuesto soportado y la retención, Fiscal cuenta las retenciones profesionales de los Modelos
// 111 y 190).
package contracts

// Source es el nombre del contexto delimitado que publica.
const Source = "purchases"

// Expense es el coste de una categoría de gasto de una factura (sus bases más, cuando el impuesto
// no es deducible, su parte del impuesto).
type Expense struct {
	Category string `json:"category"` // goods | rent | repairs | professional-services | transport | insurance | advertising | supplies | other-services | fixed-asset
	Amount   string `json:"amount"`
}

// Tax es una línea del desglose de impuestos de una factura.
type Tax struct {
	TaxCode       string `json:"taxCode,omitempty"`
	Treatment     string `json:"treatment,omitempty"`
	TreatmentKind string `json:"treatmentKind"` // subject | exempt | not-subject
	Rate          string `json:"rate"`
	Base          string `json:"base"`
	Amount        string `json:"amount"`
}

// PayTo es una cuenta a la que se paga la factura.
type PayTo struct {
	IBAN   string `json:"iban"`
	Amount string `json:"amount"`
}

// InvoiceRegisteredV1 se publica cuando se registra una factura recibida. Los importes son cadenas
// decimales en euros con dos decimales (negativos en una factura rectificativa); Total = Net + Tax y
// Payable = Total - Withholding; los gastos más DeductibleTax suman Total.
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
	WithholdingKey  string    `json:"withholdingKey,omitempty"` // clave de percepción del Modelo 190 (G: actividades profesionales)
	Payable         string    `json:"payable"`
	Expenses        []Expense `json:"expenses"`
	Taxes           []Tax     `json:"taxes"`
	PayTo           []PayTo   `json:"payTo,omitempty"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (InvoiceRegisteredV1) IntegrationEventType() string { return "purchases.invoice-registered.v1" }

// InvoiceCancelledV1 se publica cuando se anula una factura recibida registrada por error: los
// consumidores deshacen lo que hicieron con su registro.
type InvoiceCancelledV1 struct {
	InvoiceID string `json:"invoiceId"`
	Company   string `json:"company"`
	Register  string `json:"register"`
	Reason    string `json:"reason"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (InvoiceCancelledV1) IntegrationEventType() string { return "purchases.invoice-cancelled.v1" }
