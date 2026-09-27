// Package vocab is the common ubiquitous language of Karpo: value objects every bounded context
// can share (names, contact data, identification documents, dates and periods, money...).
//
// Each type was re-evaluated before porting it from the C# framework (see
// docs/LENGUAJE-UBICUO.md): only generic, domain-agnostic terms live here. Person- or
// organization-specific terms (PersonalName, OrganizationName, Gender, MaritalStatus) belong to
// the Parties bounded context.
//
// Conventions shared by every type:
//   - constructors validate and normalize; they return a *domain.ValidationError with a field
//     error per problem, never panic (Must* variants exist for constants and tests);
//   - values are immutable and comparable with == unless documented otherwise (Decimal, Money
//     and TagSet define Equal/Cmp because they hold pointers or slices);
//   - the zero value means "absent" and reports IsZero;
//   - single-value types implement encoding.TextMarshaler, so they serialize as JSON strings.
//
// Decimal is github.com/shopspring/decimal: the only third-party dependency allowed in the
// domain (Go has no decimal type; floating point is not acceptable for amounts).
package vocab

import "github.com/jhermoso/karpo-fw-go/pkg/domain"

func invalid(field, code, message string) error {
	var v domain.Validation
	v.Add(field, code, message)
	return v.Err()
}
