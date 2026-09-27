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
