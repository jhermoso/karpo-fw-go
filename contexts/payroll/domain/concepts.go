// Package domain es el modelo del contexto delimitado Payroll: las nóminas con sus líneas y
// totales derivados, el catálogo de conceptos retributivos, el perfil de nómina de cada relación
// laboral (grupo de cotización, tipo de retención, salario pactado y cómo se reparte el líquido
// entre cuentas bancarias) y las cuentas de cotización a la Seguridad Social del empleador (CCC).
// Las relaciones laborales y los contratos son de HR, las personas y organizaciones de Parties,
// los modelos fiscales de Fiscal: este contexto los referencia por su identidad.
package domain

import (
	"strings"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

type (
	// ConceptID identifica un concepto retributivo.
	ConceptID struct{ fw.UUID }
	// PayslipID identifica una nómina.
	PayslipID struct{ fw.UUID }
	// ProfileID identifica un perfil de nómina.
	ProfileID struct{ fw.UUID }
	// EmployerAccountID identifica una cuenta de cotización a la Seguridad Social del empleador (CCC).
	EmployerAccountID struct{ fw.UUID }
	// EmploymentID es una relación laboral del contexto HR.
	EmploymentID struct{ fw.UUID }
	// PersonID es una persona del contexto Parties.
	PersonID struct{ fw.UUID }
	// OrganizationID es una organización interna (empleador) del contexto Parties.
	OrganizationID struct{ fw.UUID }
	// AgreementID es un convenio colectivo del contexto HR.
	AgreementID struct{ fw.UUID }
	// WorkCenterID es un centro de trabajo del contexto HR.
	WorkCenterID struct{ fw.UUID }
)

// MustConceptID interpreta una identidad conocida.
func MustConceptID(s string) ConceptID { return ConceptID{fw.MustParseUUID(s)} }

// ConceptKind dice cómo cuenta un concepto en los totales de una nómina.
type ConceptKind int

// Clases de concepto. El C# mezclaba IRPF y Seguridad Social en un solo tipo de línea
// «Withholding», y contaba la Seguridad Social como IRPF retenido en el Modelo 190.
const (
	KindEarning        ConceptKind = iota + 1 // devengo: suma al bruto
	KindSocialSecurity                        // cotización del trabajador: se deduce
	KindIncomeTax                             // retención de IRPF: se deduce
	KindDeduction                             // otras deducciones (embargo, anticipo): se deducen
	KindEmployerCost                          // cotización de la empresa: coste de empresa, no se deduce
	KindInformation                           // informativo, sin efecto económico
)

var kindNames = map[ConceptKind]string{KindEarning: "earning", KindSocialSecurity: "social-security", KindIncomeTax: "income-tax",
	KindDeduction: "deduction", KindEmployerCost: "employer-cost", KindInformation: "information"}

// String devuelve el nombre estable de la clase.
func (k ConceptKind) String() string { return kindNames[k] }

// ParseConceptKind interpreta el nombre de una clase.
func ParseConceptKind(s string) (ConceptKind, bool) {
	for k, n := range kindNames {
		if n == s {
			return k, true
		}
	}
	return 0, false
}

// Concept es una entrada del catálogo de conceptos retributivos (sustituye los códigos de concepto
// de texto libre y la mezcla DeductionType / RateType de C#). Contributable y Taxable dicen si un
// devengo cuenta en la base de cotización y en la base de retención; PerceptionKey es la clave del
// Modelo 190.
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

// Validate comprueba un concepto.
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
