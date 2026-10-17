package domain

import (
	"context"
	"strconv"
	"strings"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// EmployerAccountKind es el nombre estable del tipo de agregado.
const EmployerAccountKind = "payroll.employer_account"

// NormalizeCCC quita los separadores de un Código de Cuenta de Cotización.
func NormalizeCCC(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s)
}

// ValidCCC comprueba un CCC: 11 dígitos, provincia (2) + número (7) + control (2), donde el control
// es (provincia × 10⁷ + número) mod 97 (el C# no validaba el formato).
func ValidCCC(ccc string) bool {
	if len(ccc) != 11 || NormalizeCCC(ccc) != ccc {
		return false
	}
	province, _ := strconv.ParseInt(ccc[:2], 10, 64)
	number, _ := strconv.ParseInt(ccc[2:9], 10, 64)
	control, _ := strconv.ParseInt(ccc[9:], 10, 64)
	return province >= 1 && (province*10_000_000+number)%97 == control
}

// PaymentMethod es cómo paga el empleador sus cotizaciones (el C# tenía los códigos escritos a
// fuego).
type PaymentMethod int

// Formas de pago.
const (
	DirectDebit  PaymentMethod = iota + 1 // cargo en cuenta
	LatePayable                           // pagadero tardío
	EarlyPayable                          // pagadero anticipado
	ManualPayment
)

var paymentMethods = map[PaymentMethod]string{DirectDebit: "direct-debit", LatePayable: "late-payable", EarlyPayable: "early-payable",
	ManualPayment: "manual"}

// String devuelve el nombre estable.
func (m PaymentMethod) String() string { return paymentMethods[m] }

// ParsePaymentMethod interpreta el nombre de una forma de pago.
func ParsePaymentMethod(s string) (PaymentMethod, bool) {
	for k, n := range paymentMethods {
		if n == s {
			return k, true
		}
	}
	return 0, false
}

// EmployerAccount es una cuenta de cotización a la Seguridad Social (CCC) de un empleador (el
// CompanySocialSecurityAccount + Payment de C#; los representantes se dejan para más adelante).
type EmployerAccount struct {
	fw.BaseAggregateRoot[EmployerAccountID]
	traits.Audited
	employer OrganizationID
	code     string
	regime   string
	activity string
	method   PaymentMethod
	iban     vocab.IBAN
	active   bool
}

// EmployerAccountState es el estado persistido de una cuenta de cotización.
type EmployerAccountState struct {
	Employer OrganizationID
	Code     string
	Regime   string // código de régimen de la Seguridad Social (p. ej. 0111 general)
	Activity string // CNAE
	Method   PaymentMethod
	IBAN     vocab.IBAN
	Active   bool
	Audit    traits.AuditStamp
}

func (s *EmployerAccountState) check(v *fw.Validation) {
	s.Code = NormalizeCCC(s.Code)
	v.Require(ValidCCC(s.Code), "code", "format", "a valid CCC: 11 digits with control digits")
	s.Regime = checkText(v, "regime", s.Regime, 4)
	v.Require(len(s.Regime) == 4 && NormalizeCCC(s.Regime) == s.Regime, "regime", "format", "a regime code of 4 digits")
	s.Activity = checkText(v, "activity", s.Activity, 5)
	_, ok := paymentMethods[s.Method]
	v.Require(ok, "method", "enum", "unknown payment method")
	v.Require(s.Method != DirectDebit || !s.IBAN.IsZero(), "iban", "required", "a direct debit needs a bank account")
}

// ReconstituteEmployerAccount reconstruye una cuenta de cotización.
func ReconstituteEmployerAccount(id EmployerAccountID, s EmployerAccountState) (*EmployerAccount, error) {
	base, err := fw.NewBaseAggregateRoot(EmployerAccountKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(!s.Employer.IsZero(), "employer", "required", "an account belongs to an employer")
	s.check(&v)
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &EmployerAccount{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), employer: s.Employer, code: s.Code,
		regime: s.Regime, activity: s.Activity, method: s.Method, iban: s.IBAN, active: s.Active}, nil
}

// RegisterEmployerAccount da de alta un CCC de un empleador.
func RegisterEmployerAccount(id EmployerAccountID, s EmployerAccountState) (*EmployerAccount, error) {
	s.Active = true
	a, err := ReconstituteEmployerAccount(id, s)
	if err != nil {
		return nil, err
	}
	a.Raise(EmployerAccountRegistered{EventMeta: a.NewEventMeta(), Employer: s.Employer.String(), Code: a.code})
	return a, nil
}

// Employer devuelve el empleador.
func (a *EmployerAccount) Employer() OrganizationID { return a.employer }

// Code devuelve el CCC (11 dígitos).
func (a *EmployerAccount) Code() string { return a.code }

// Regime devuelve el código de régimen de la Seguridad Social.
func (a *EmployerAccount) Regime() string { return a.regime }

// Activity devuelve el CNAE.
func (a *EmployerAccount) Activity() string { return a.activity }

// Method devuelve la forma de pago.
func (a *EmployerAccount) Method() PaymentMethod { return a.method }

// IBAN devuelve la cuenta en la que se cargan las cotizaciones.
func (a *EmployerAccount) IBAN() vocab.IBAN { return a.iban }

// IsActive dice si la cuenta está de alta en la Seguridad Social (no «de baja»).
func (a *EmployerAccount) IsActive() bool { return a.active }

// SetPayment cambia cómo se pagan las cotizaciones.
func (a *EmployerAccount) SetPayment(m PaymentMethod, iban vocab.IBAN) error {
	s := EmployerAccountState{Employer: a.employer, Code: a.code, Regime: a.regime, Activity: a.activity, Method: m, IBAN: iban}
	var v fw.Validation
	s.check(&v)
	if err := v.Err(); err != nil {
		return err
	}
	a.method, a.iban = m, iban
	return nil
}

// Deactivate registra la baja de la cuenta en la Seguridad Social.
func (a *EmployerAccount) Deactivate() {
	if a.active {
		a.active = false
		a.Raise(EmployerAccountDeactivated{EventMeta: a.NewEventMeta()})
	}
}

// AuditSnapshot implementa traits.Snapshotter.
func (a *EmployerAccount) AuditSnapshot() map[string]any {
	return map[string]any{"code": a.code, "regime": a.regime, "method": a.method.String(), "active": a.active}
}

// Campos de la cuenta de cotización.
var (
	AccFieldID       = spec.Comparable("id", func(a *EmployerAccount) EmployerAccountID { return a.ID() })
	AccFieldEmployer = spec.Comparable("employer", (*EmployerAccount).Employer)
	AccFieldCode     = spec.Ordered("code", (*EmployerAccount).Code)
	AccFieldActive   = spec.Comparable("active", (*EmployerAccount).IsActive)
)

// Repositorios y catálogos del contexto.
type (
	PayslipRepository         = fw.Repository[PayslipID, *Payslip]
	ProfileRepository         = fw.Repository[ProfileID, *Profile]
	EmployerAccountRepository = fw.Repository[EmployerAccountID, *EmployerAccount]
)

// Catalogs lee el catálogo de conceptos retributivos.
type Catalogs interface {
	Concepts(ctx context.Context) ([]Concept, error)
}
