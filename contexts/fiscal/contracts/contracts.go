// Package contracts es aquello de lo que pueden depender otros contextos delimitados: el Published
// Language de Fiscal y su consulta de tipos (Billing pregunta el tipo de un código de impuesto en
// una fecha en vez de copiar filas de VatGroup por empresa, como hacía el C#).
package contracts

import "context"

// Source es el nombre del contexto delimitado que publica.
const Source = "fiscal"

// RateRef es un tipo impositivo vigente.
type RateRef struct {
	ID        string `json:"id"`
	Type      string `json:"type"`      // vat | igic | ipsi
	Territory string `json:"territory"` // common | basque-country | navarre | canaries | ceuta-melilla
	Code      string `json:"code"`
	Rate      string `json:"rate"`                // porcentaje, dos decimales
	Surcharge string `json:"surcharge,omitempty"` // recargo de equivalencia, sólo IVA
}

// Rates responde con el tipo de un código de impuesto en un territorio en una fecha civil
// (YYYY-MM-DD).
type Rates interface {
	RateOn(ctx context.Context, taxType, territory, code, date string) (RateRef, bool, error)
}

// FilingSubmittedV1 se publica cuando se presenta un modelo fiscal (Accounting liquida la retención
// a pagar, Treasury programa el pago).
type FilingSubmittedV1 struct {
	FilingID    string `json:"filingId"`
	Declarant   string `json:"declarant"`
	Form        string `json:"form"`
	Year        int    `json:"year"`
	Period      string `json:"period"` // código AEAT: 01–12, 1T–4T, 0A
	Number      int64  `json:"number"`
	Recipients  int    `json:"recipients"`
	Perceptions string `json:"perceptions"`
	Withheld    string `json:"withheld"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (FilingSubmittedV1) IntegrationEventType() string { return "fiscal.filing-submitted.v1" }

// FilingRevertedV1 se publica cuando se revierte un modelo presentado.
type FilingRevertedV1 struct {
	FilingID string `json:"filingId"`
	Reason   string `json:"reason"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (FilingRevertedV1) IntegrationEventType() string { return "fiscal.filing-reverted.v1" }

// TaxableLine es una línea que gravar: su base (redondeada a céntimos) y su código de impuesto o de
// tratamiento.
type TaxableLine struct {
	Ref       string `json:"ref,omitempty"`
	Base      string `json:"base"`
	TaxCode   string `json:"taxCode,omitempty"`
	Treatment string `json:"treatment,omitempty"`
}

// TaxableDocument es un documento que gravar: el vendedor (su perfil fiscal decide la jurisdicción,
// el territorio y el régimen), la fecha de devengo y las líneas.
type TaxableDocument struct {
	Seller               string        `json:"seller"`
	Date                 string        `json:"date"` // fecha civil
	EquivalenceSurcharge bool          `json:"equivalenceSurcharge,omitempty"`
	Lines                []TaxableLine `json:"lines"`
}

// TaxLine es una línea del desglose de impuestos: una por código de impuesto y tratamiento.
type TaxLine struct {
	TaxType         string `json:"taxType,omitempty"` // vat | igic | ipsi; vacío para bases exentas y no sujetas
	TaxCode         string `json:"taxCode,omitempty"`
	Treatment       string `json:"treatment,omitempty"`
	TreatmentKind   string `json:"treatmentKind"` // subject | exempt | not-subject
	Rate            string `json:"rate"`
	Base            string `json:"base"`
	Amount          string `json:"amount"`
	SurchargeRate   string `json:"surchargeRate,omitempty"`
	SurchargeAmount string `json:"surchargeAmount,omitempty"`
}

// TaxBreakdown es el resultado de un cálculo. Los importes son cadenas decimales con dos decimales.
type TaxBreakdown struct {
	Country   string    `json:"country"`
	Lines     []TaxLine `json:"lines"`
	Net       string    `json:"net"`
	Tax       string    `json:"tax"`
	Surcharge string    `json:"surcharge"`
}

// TaxEngine calcula los impuestos indirectos de un documento con la jurisdicción del vendedor
// (estructura aprobada: una jurisdicción por país, una estrategia por régimen o sector).
type TaxEngine interface {
	Calculate(ctx context.Context, doc TaxableDocument) (TaxBreakdown, error)
}
