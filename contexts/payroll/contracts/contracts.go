// Package contracts es aquello de lo que pueden depender los demás contextos delimitados: el
// Published Language de Payroll y su puerto de consulta. Fiscal (Modelo 111/190), Accounting
// (asientos de salarios) y Treasury (transferencias SEPA) consumen las nóminas aprobadas en vez de
// leer las tablas de nóminas, como hacían los generadores de modelos fiscales de C#.
package contracts

import "context"

// Source es el nombre del contexto delimitado que publica.
const Source = "payroll"

// MaxBatch es el número máximo de ids por llamada.
const MaxBatch = 900

// PayslipApprovedV1 se publica al aprobar una nómina. Los importes son cadenas decimales en euros
// con dos decimales; las fechas son fechas civiles.
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
	PerceptionKey    string `json:"perceptionKey,omitempty"` // clave del Modelo 190 (A: rendimientos del trabajo)
	Gross            string `json:"gross"`
	ContributionBase string `json:"contributionBase"`
	TaxableBase      string `json:"taxableBase"`
	SocialSecurity   string `json:"socialSecurity"`
	IncomeTax        string `json:"incomeTax"`
	OtherDeductions  string `json:"otherDeductions"`
	Net              string `json:"net"`
	EmployerCost     string `json:"employerCost"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (PayslipApprovedV1) IntegrationEventType() string { return "payroll.payslip-approved.v1" }

// PayslipCancelledV1 se publica al anular una nómina aprobada: los consumidores deshacen lo que
// hicieron con su aprobación.
type PayslipCancelledV1 struct {
	PayslipID string `json:"payslipId"`
	Reason    string `json:"reason"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (PayslipCancelledV1) IntegrationEventType() string { return "payroll.payslip-cancelled.v1" }

// PaymentRef es una transferencia de parte de un líquido.
type PaymentRef struct {
	IBAN        string `json:"iban"`
	Amount      string `json:"amount"`
	Garnishment bool   `json:"garnishment,omitempty"`
}

// Remittance responde cómo se reparte el líquido de las nóminas aprobadas entre cuentas bancarias
// (el C# guardaba los repartos y no los aplicaba nunca). Las nóminas no aprobadas, o cuya relación
// laboral no tiene repartos, no aparecen.
type Remittance interface {
	NetPayments(ctx context.Context, payslipIDs []string) (map[string][]PaymentRef, error)
}
