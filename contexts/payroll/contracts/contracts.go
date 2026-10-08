// Package contracts is what other bounded contexts may depend on: the Published Language of
// Payroll and its query port. Fiscal (Modelo 111/190), Accounting (salary journal entries) and
// Treasury (SEPA transfers) consume approved payslips instead of reading payroll tables, as the
// C# tax-form generators did.
package contracts

import "context"

// Source is the name of the publishing bounded context.
const Source = "payroll"

// MaxBatch is the maximum number of ids per call.
const MaxBatch = 900

// PayslipApprovedV1 is published when a payslip is approved. Amounts are decimal strings in euros
// with two decimals; dates are civil dates.
type PayslipApprovedV1 struct {
	PayslipID        string `json:"payslipId"`
	Employment       string `json:"employment"`
	Person           string `json:"person"`
	Employer         string `json:"employer"`
	Kind             string `json:"kind"` // ordinary | extra-pay | settlement
	PeriodStart      string `json:"periodStart"`
	PeriodEnd        string `json:"periodEnd"`
	PaymentDate      string `json:"paymentDate"`
	EmployerAccount  string `json:"employerAccount,omitempty"`
	PerceptionKey    string `json:"perceptionKey,omitempty"` // Modelo 190 key (A: employment income)
	Gross            string `json:"gross"`
	ContributionBase string `json:"contributionBase"`
	TaxableBase      string `json:"taxableBase"`
	SocialSecurity   string `json:"socialSecurity"`
	IncomeTax        string `json:"incomeTax"`
	OtherDeductions  string `json:"otherDeductions"`
	Net              string `json:"net"`
	EmployerCost     string `json:"employerCost"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (PayslipApprovedV1) IntegrationEventType() string { return "payroll.payslip-approved.v1" }

// PayslipCancelledV1 is published when an approved payslip is cancelled: consumers reverse what
// they did with its approval.
type PayslipCancelledV1 struct {
	PayslipID string `json:"payslipId"`
	Reason    string `json:"reason"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (PayslipCancelledV1) IntegrationEventType() string { return "payroll.payslip-cancelled.v1" }

// PaymentRef is a transfer of part of a net pay.
type PaymentRef struct {
	IBAN        string `json:"iban"`
	Amount      string `json:"amount"`
	Garnishment bool   `json:"garnishment,omitempty"`
}

// Remittance answers how the net pay of approved payslips is split across bank accounts (the C#
// stored the splits and never applied them). Payslips that are not approved, or whose employment
// has no splits, are absent.
type Remittance interface {
	NetPayments(ctx context.Context, payslipIDs []string) (map[string][]PaymentRef, error)
}
