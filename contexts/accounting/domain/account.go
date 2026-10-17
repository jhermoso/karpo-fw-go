// Package domain es el modelo del contexto delimitado Accounting: el plan de cuentas de cada
// empresa, su contabilidad (ejercicio, periodos cerrados y el perfil de contabilización que decide
// las cuentas de los asientos automáticos) y los asientos del diario, siempre cuadrados, numerados
// sin huecos y enlazados al hecho que los produjo. Facturas, cobros, remesas y nóminas pertenecen a
// sus contextos: Accounting contabiliza lo que publican.
package domain

import (
	"strings"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
)

type (
	// AccountID identifica una cuenta.
	AccountID struct{ fw.UUID }
	// LedgerID identifica una contabilidad.
	LedgerID struct{ fw.UUID }
	// EntryID identifica un asiento del diario.
	EntryID struct{ fw.UUID }
	// CounterID identifica un contador de asientos.
	CounterID struct{ fw.UUID }
	// OrganizationID es una organización interna (una empresa) del contexto Parties.
	OrganizationID struct{ fw.UUID }
	// PartyID es una party (cliente, empleado…) del contexto Parties.
	PartyID struct{ fw.UUID }
)

// Nature de una cuenta según su grupo del PGC: balance (1–5), cuenta de pérdidas y ganancias (6–7)
// o ingresos y gastos imputados al patrimonio neto (8–9).
type Nature int

// Naturalezas.
const (
	BalanceSheet Nature = iota + 1
	IncomeStatement
	EquityIncome
)

var natures = map[Nature]string{BalanceSheet: "balance-sheet", IncomeStatement: "income-statement", EquityIncome: "equity-income"}

// String devuelve el nombre estable.
func (n Nature) String() string { return natures[n] }

// ValidCode comprueba un código de cuenta: de 1 a 12 dígitos, el primero un grupo del PGC (1–9).
func ValidCode(c string) bool {
	if len(c) == 0 || len(c) > 12 || c[0] == '0' {
		return false
	}
	for _, r := range c {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// NatureOf devuelve la naturaleza de un código según su grupo.
func NatureOf(code string) Nature {
	switch code[0] {
	case '6', '7':
		return IncomeStatement
	case '8', '9':
		return EquityIncome
	}
	return BalanceSheet
}

// AccountKind es el nombre estable del tipo de agregado.
const AccountKind = "accounting.account"

// AccountState es el estado persistido de una cuenta.
type AccountState struct {
	Company  OrganizationID
	Code     string
	Name     string
	Postable bool // cuenta de detalle; las cabeceras (grupos, subgrupos) sólo agregan
	Active   bool
	Audit    traits.AuditStamp
}

// Account es una cuenta del plan de una empresa (el C# la repartía en GeneralLedgerAccount, con el
// código en su Name, GlAccountProfile, otra vez con el código, y AccountingPlan; la jerarquía es el
// prefijo del código, como en el PGC).
type Account struct {
	fw.BaseAggregateRoot[AccountID]
	traits.Audited
	s AccountState
}

// ReconstituteAccount reconstruye una cuenta.
func ReconstituteAccount(id AccountID, s AccountState) (*Account, error) {
	base, err := fw.NewBaseAggregateRoot(AccountKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(!s.Company.IsZero(), "company", "required", "an account belongs to a company")
	s.Code = strings.TrimSpace(s.Code)
	v.Require(ValidCode(s.Code), "code", "format", "1 to 12 digits, starting with a PGC group 1–9")
	s.Name = strings.TrimSpace(s.Name)
	v.Require(s.Name != "" && utf8.RuneCountInString(s.Name) <= 200, "name", "length", "a name of 1 to 200 characters")
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &Account{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// State devuelve el estado.
func (a *Account) State() AccountState { return a.s }

// Nature devuelve la naturaleza de la cuenta.
func (a *Account) Nature() Nature { return NatureOf(a.s.Code) }

// Rename cambia el nombre.
func (a *Account) Rename(name string) error {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 200 {
		var v fw.Validation
		v.Add("name", "length", "a name of 1 to 200 characters")
		return v.Err()
	}
	a.s.Name = name
	return nil
}

// Deactivate da de baja la cuenta (los asientos ya contabilizados la conservan).
func (a *Account) Deactivate() { a.s.Active = false }

// AuditSnapshot implementa traits.Snapshotter.
func (a *Account) AuditSnapshot() map[string]any {
	return map[string]any{"code": a.s.Code, "name": a.s.Name, "postable": a.s.Postable, "active": a.s.Active}
}

// Campos de la cuenta.
var (
	AccFieldCompany = spec.Comparable("company", func(a *Account) OrganizationID { return a.s.Company })
	AccFieldCode    = spec.Ordered("code", func(a *Account) string { return a.s.Code })
)
