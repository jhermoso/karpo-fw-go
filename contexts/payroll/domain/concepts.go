// Package domain is the model of the Payroll bounded context: payslips with their lines and
// derived totals, the pay concept catalog, the payroll profile of each employment (contribution
// group, withholding rate, agreed salary and how the net pay is split across bank accounts) and
// the employer Social Security accounts (CCC). Employments and contracts belong to HR, people and
// organizations to Parties, tax forms to Fiscal: this context references them by identity.
package domain

import (
	"strings"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

type (
	// ConceptID identifies a pay concept.
	ConceptID struct{ fw.UUID }
	// PayslipID identifies a payslip.
	PayslipID struct{ fw.UUID }
	// ProfileID identifies a payroll profile.
	ProfileID struct{ fw.UUID }
	// EmployerAccountID identifies an employer Social Security account (CCC).
	EmployerAccountID struct{ fw.UUID }
	// EmploymentID is an employment of the HR context.
	EmploymentID struct{ fw.UUID }
	// PersonID is a person of the Parties context.
	PersonID struct{ fw.UUID }
	// OrganizationID is an internal organization (employer) of the Parties context.
	OrganizationID struct{ fw.UUID }
	// AgreementID is a collective agreement of the HR context.
	AgreementID struct{ fw.UUID }
	// WorkCenterID is a work center of the HR context.
	WorkCenterID struct{ fw.UUID }
)

// MustConceptID parses a well-known identity.
func MustConceptID(s string) ConceptID { return ConceptID{fw.MustParseUUID(s)} }

// ConceptKind says how a concept counts in the totals of a payslip.
type ConceptKind int

// Concept kinds. The C# mixed IRPF and Social Security in one "Withholding" line type, and
// counted Social Security as IRPF withheld in the Modelo 190.
const (
	KindEarning        ConceptKind = iota + 1 // devengo: adds to the gross
	KindSocialSecurity                        // employee contribution: deducted
	KindIncomeTax                             // IRPF withholding: deducted
	KindDeduction                             // other deductions (garnishment, advance): deducted
	KindEmployerCost                          // employer contribution: company cost, not deducted
	KindInformation                           // informative, no economic effect
)

var kindNames = map[ConceptKind]string{KindEarning: "earning", KindSocialSecurity: "social-security", KindIncomeTax: "income-tax",
	KindDeduction: "deduction", KindEmployerCost: "employer-cost", KindInformation: "information"}

// String returns the stable name of the kind.
func (k ConceptKind) String() string { return kindNames[k] }

// ParseConceptKind parses a kind name.
func ParseConceptKind(s string) (ConceptKind, bool) {
	for k, n := range kindNames {
		if n == s {
			return k, true
		}
	}
	return 0, false
}

// Concept is an entry of the pay concept catalog (it replaces the free-text concept codes and the
// DeductionType / RateType mix of the C#). Contributable and Taxable say whether an earning counts
// in the contribution base and in the withholding base; PerceptionKey is the Modelo 190 key.
type Concept struct {
	ID            ConceptID
	Code          string
	Name          string
	Kind          ConceptKind
	Contributable bool
	Taxable       bool
	PerceptionKey string
	Active        bool
}

// Validate checks a concept.
func (c Concept) Validate() error {
	var v fw.Validation
	code := strings.TrimSpace(c.Code)
	v.Require(code != "" && utf8.RuneCountInString(code) <= 20, "code", "length", "a code of 1 to 20 characters is required")
	v.Require(strings.TrimSpace(c.Name) != "" && utf8.RuneCountInString(c.Name) <= 100, "name", "length", "a name of 1 to 100 characters is required")
	_, known := kindNames[c.Kind]
	v.Require(known, "kind", "enum", "unknown concept kind")
	v.Require(c.Kind == KindEarning || (!c.Contributable && !c.Taxable), "kind", "bases", "only earnings count in the bases")
	v.Require(utf8.RuneCountInString(c.PerceptionKey) <= 1, "perceptionKey", "length", "a one-letter key")
	return v.Err()
}
