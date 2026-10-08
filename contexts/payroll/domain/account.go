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

// EmployerAccountKind is the stable aggregate type name.
const EmployerAccountKind = "payroll.employer_account"

// NormalizeCCC removes separators from a Código de Cuenta de Cotización.
func NormalizeCCC(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s)
}

// ValidCCC checks a CCC: 11 digits, province (2) + number (7) + control (2), where the control is
// (province × 10⁷ + number) mod 97 (the C# did not validate the format).
func ValidCCC(ccc string) bool {
	if len(ccc) != 11 || NormalizeCCC(ccc) != ccc {
		return false
	}
	province, _ := strconv.ParseInt(ccc[:2], 10, 64)
	number, _ := strconv.ParseInt(ccc[2:9], 10, 64)
	control, _ := strconv.ParseInt(ccc[9:], 10, 64)
	return province >= 1 && (province*10_000_000+number)%97 == control
}

// PaymentMethod is how the employer pays its contributions (the C# hardcoded codes).
type PaymentMethod int

// Payment methods.
const (
	DirectDebit  PaymentMethod = iota + 1 // cargo en cuenta
	LatePayable                           // pagadero tardío
	EarlyPayable                          // pagadero anticipado
	ManualPayment
)

var paymentMethods = map[PaymentMethod]string{DirectDebit: "direct-debit", LatePayable: "late-payable", EarlyPayable: "early-payable",
	ManualPayment: "manual"}

// String returns the stable name.
func (m PaymentMethod) String() string { return paymentMethods[m] }

// ParsePaymentMethod parses a payment method name.
func ParsePaymentMethod(s string) (PaymentMethod, bool) {
	for k, n := range paymentMethods {
		if n == s {
			return k, true
		}
	}
	return 0, false
}

// EmployerAccount is a Social Security contribution account (CCC) of an employer (the C#
// CompanySocialSecurityAccount + Payment; representatives are deferred).
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

// EmployerAccountState is the persisted state of an employer account.
type EmployerAccountState struct {
	Employer OrganizationID
	Code     string
	Regime   string // Social Security regime code (e.g. 0111 general)
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

// ReconstituteEmployerAccount rebuilds an employer account.
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

// RegisterEmployerAccount registers a CCC of an employer.
func RegisterEmployerAccount(id EmployerAccountID, s EmployerAccountState) (*EmployerAccount, error) {
	s.Active = true
	a, err := ReconstituteEmployerAccount(id, s)
	if err != nil {
		return nil, err
	}
	a.Raise(EmployerAccountRegistered{EventMeta: a.NewEventMeta(), Employer: s.Employer.String(), Code: a.code})
	return a, nil
}

// Employer returns the employer.
func (a *EmployerAccount) Employer() OrganizationID { return a.employer }

// Code returns the CCC (11 digits).
func (a *EmployerAccount) Code() string { return a.code }

// Regime returns the Social Security regime code.
func (a *EmployerAccount) Regime() string { return a.regime }

// Activity returns the CNAE.
func (a *EmployerAccount) Activity() string { return a.activity }

// Method returns the payment method.
func (a *EmployerAccount) Method() PaymentMethod { return a.method }

// IBAN returns the account the contributions are charged to.
func (a *EmployerAccount) IBAN() vocab.IBAN { return a.iban }

// IsActive reports whether the account is registered with Social Security (not "de baja").
func (a *EmployerAccount) IsActive() bool { return a.active }

// SetPayment changes how contributions are paid.
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

// Deactivate records the Social Security "baja" of the account.
func (a *EmployerAccount) Deactivate() {
	if a.active {
		a.active = false
		a.Raise(EmployerAccountDeactivated{EventMeta: a.NewEventMeta()})
	}
}

// AuditSnapshot implements traits.Snapshotter.
func (a *EmployerAccount) AuditSnapshot() map[string]any {
	return map[string]any{"code": a.code, "regime": a.regime, "method": a.method.String(), "active": a.active}
}

// Employer account fields.
var (
	AccFieldID       = spec.Comparable("id", func(a *EmployerAccount) EmployerAccountID { return a.ID() })
	AccFieldEmployer = spec.Comparable("employer", (*EmployerAccount).Employer)
	AccFieldCode     = spec.Ordered("code", (*EmployerAccount).Code)
	AccFieldActive   = spec.Comparable("active", (*EmployerAccount).IsActive)
)

// Repositories and catalogs of the context.
type (
	PayslipRepository         = fw.Repository[PayslipID, *Payslip]
	ProfileRepository         = fw.Repository[ProfileID, *Profile]
	EmployerAccountRepository = fw.Repository[EmployerAccountID, *EmployerAccount]
)

// Catalogs reads the pay concept catalog.
type Catalogs interface {
	Concepts(ctx context.Context) ([]Concept, error)
}
