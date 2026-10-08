// Package contracts is what other bounded contexts may depend on: the Published Language of
// Fiscal and its rate lookup (Billing asks the rate of a tax code on a date instead of copying
// VatGroup rows per company, as the C# did).
package contracts

import "context"

// Source is the name of the publishing bounded context.
const Source = "fiscal"

// RateRef is a tax rate in force.
type RateRef struct {
	ID        string `json:"id"`
	Type      string `json:"type"`      // vat | igic | ipsi
	Territory string `json:"territory"` // common | basque-country | navarre | canaries | ceuta-melilla
	Code      string `json:"code"`
	Rate      string `json:"rate"`                // percentage, two decimals
	Surcharge string `json:"surcharge,omitempty"` // equivalence surcharge, VAT only
}

// Rates answers the rate of a tax code in a territory on a civil date (YYYY-MM-DD).
type Rates interface {
	RateOn(ctx context.Context, taxType, territory, code, date string) (RateRef, bool, error)
}

// FilingSubmittedV1 is published when a tax form is submitted (Accounting settles the
// withholding payable, Treasury schedules the payment).
type FilingSubmittedV1 struct {
	FilingID    string `json:"filingId"`
	Declarant   string `json:"declarant"`
	Form        string `json:"form"`
	Year        int    `json:"year"`
	Period      string `json:"period"` // AEAT code: 01–12, 1T–4T, 0A
	Number      int64  `json:"number"`
	Recipients  int    `json:"recipients"`
	Perceptions string `json:"perceptions"`
	Withheld    string `json:"withheld"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (FilingSubmittedV1) IntegrationEventType() string { return "fiscal.filing-submitted.v1" }

// FilingRevertedV1 is published when a submitted form is reverted.
type FilingRevertedV1 struct {
	FilingID string `json:"filingId"`
	Reason   string `json:"reason"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (FilingRevertedV1) IntegrationEventType() string { return "fiscal.filing-reverted.v1" }

// TaxableLine is a line to tax: its base (rounded to cents) and its tax code or treatment code.
type TaxableLine struct {
	Ref       string `json:"ref,omitempty"`
	Base      string `json:"base"`
	TaxCode   string `json:"taxCode,omitempty"`
	Treatment string `json:"treatment,omitempty"`
}

// TaxableDocument is a document to tax: the seller (its fiscal profile decides the jurisdiction,
// the territory and the regime), the date of accrual and the lines.
type TaxableDocument struct {
	Seller               string        `json:"seller"`
	Date                 string        `json:"date"` // civil date
	EquivalenceSurcharge bool          `json:"equivalenceSurcharge,omitempty"`
	Lines                []TaxableLine `json:"lines"`
}

// TaxLine is a line of the tax breakdown: one per tax code and treatment.
type TaxLine struct {
	TaxType         string `json:"taxType,omitempty"` // vat | igic | ipsi; empty for exempt and not subject bases
	TaxCode         string `json:"taxCode,omitempty"`
	Treatment       string `json:"treatment,omitempty"`
	TreatmentKind   string `json:"treatmentKind"` // subject | exempt | not-subject
	Rate            string `json:"rate"`
	Base            string `json:"base"`
	Amount          string `json:"amount"`
	SurchargeRate   string `json:"surchargeRate,omitempty"`
	SurchargeAmount string `json:"surchargeAmount,omitempty"`
}

// TaxBreakdown is the result of a calculation. Amounts are decimal strings with two decimals.
type TaxBreakdown struct {
	Country   string    `json:"country"`
	Lines     []TaxLine `json:"lines"`
	Net       string    `json:"net"`
	Tax       string    `json:"tax"`
	Surcharge string    `json:"surcharge"`
}

// TaxEngine calculates the indirect taxes of a document with the jurisdiction of the seller
// (approved structure: one jurisdiction per country, one strategy per regime or sector).
type TaxEngine interface {
	Calculate(ctx context.Context, doc TaxableDocument) (TaxBreakdown, error)
}
