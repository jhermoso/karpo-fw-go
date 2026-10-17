package domain

import (
	"context"
	"slices"
	"strings"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Nombres de los tipos de agregado.
const (
	ProductKind   = "financial.product"
	AgreementKind = "financial.agreement"
)

// Identidades.
type (
	// ProductID identifica un producto financiero.
	ProductID struct{ fw.UUID }
	// AgreementID identifica un acuerdo.
	AgreementID struct{ fw.UUID }
)

// Identidades nuevas e interpretación.
func NewProductID() ProductID     { return ProductID{fw.NewUUID()} }
func NewAgreementID() AgreementID { return AgreementID{fw.NewUUID()} }

// ParseProductID interpreta una identidad en texto.
func ParseProductID(s string) (ProductID, error) { u, err := fw.ParseUUID(s); return ProductID{u}, err }

// ParseAgreementID interpreta una identidad en texto.
func ParseAgreementID(s string) (AgreementID, error) {
	u, err := fw.ParseUUID(s)
	return AgreementID{u}, err
}

// Institutions dice si una empresa es una entidad financiera. Este contexto es el sector financiero
// de Karpo: sólo existe para las empresas que lo son (en Parties, una organización interna que
// además tiene el papel de entidad financiera).
type Institutions interface {
	IsFinancialInstitution(ctx context.Context, company OrganizationID) (bool, error)
}

// Families son las familias de producto financiero y de los acuerdos y cuentas a los que dan lugar (el C# guardaba
// texto libre: savings_account, checking_account, mortgage_loan_account, loan_agreement,
// investment_agreement, leasing_agreement, generic).
var Families = []string{"payment", "deposit", "loan", "investment", "leasing", "other"}

// ValidCode dice si s es un código: de 1 a max mayúsculas, dígitos, guiones, puntos o guiones
// bajos.
func ValidCode(s string, max int) bool {
	if s == "" || len(s) > max {
		return false
	}
	for _, c := range s {
		if !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '.' || c == '_') {
			return false
		}
	}
	return true
}

// NormalizeCode recorta un código y lo pasa a mayúsculas.
func NormalizeCode(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }

// ProductState es el estado persistido de un producto financiero.
type ProductState struct {
	Company        OrganizationID
	Code           string // el propio de la entidad, único en ella
	Name           string
	Description    string
	Family         string
	RegulatoryCode string     // aquél por el que lo conoce el supervisor (CNMV, MiFID II)
	Discontinued   vocab.Date // desde este día ya no se contrata
	Audit          traits.AuditStamp
}

// Product es algo que una entidad financiera ofrece a sus clientes: un tipo de cuenta, de préstamo,
// de depósito. En el C# colgaba de tres niveles de la jerarquía de productos y tenía un campo.
type Product struct {
	fw.BaseAggregateRoot[ProductID]
	traits.Audited
	s ProductState
}

// ReconstituteProduct reconstruye un producto.
func ReconstituteProduct(id ProductID, s ProductState) (*Product, error) {
	base, err := fw.NewBaseAggregateRoot(ProductKind, id)
	if err != nil {
		return nil, err
	}
	s.Code, s.RegulatoryCode = NormalizeCode(s.Code), NormalizeCode(s.RegulatoryCode)
	s.Name, s.Description, s.Family = strings.TrimSpace(s.Name), strings.TrimSpace(s.Description), strings.ToLower(strings.TrimSpace(s.Family))
	var v fw.Validation
	v.Require(!s.Company.IsZero(), "company", "required", "the institution that offers it")
	v.Require(ValidCode(s.Code, 30), "code", "format", "a code of 1 to 30 letters, digits, dashes, dots or underscores")
	v.Require(s.Name != "" && utf8.RuneCountInString(s.Name) <= 120, "name", "length", "a name of 1 to 120 characters")
	v.Require(utf8.RuneCountInString(s.Description) <= 500, "description", "length", "a description of at most 500 characters")
	v.Require(slices.Contains(Families, s.Family), "family", "enum", "payment, deposit, loan, investment, leasing or other")
	v.Require(s.RegulatoryCode == "" || ValidCode(s.RegulatoryCode, 50), "regulatoryCode", "format", "a code of at most 50 characters")
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &Product{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// State devuelve el estado.
func (p *Product) State() ProductState { return p.s }

// Offered dice si el producto se puede contratar en un día.
func (p *Product) Offered(on vocab.Date) bool {
	return p.s.Discontinued.IsZero() || on.Before(p.s.Discontinued)
}

// Change sustituye lo que describe el producto; su empresa y su código se quedan.
func (p *Product) Change(name, description, family, regulatoryCode string) error {
	s := p.s
	s.Name, s.Description, s.Family, s.RegulatoryCode = name, description, family, regulatoryCode
	n, err := ReconstituteProduct(p.ID(), s)
	if err != nil {
		return err
	}
	p.s = n.s
	return nil
}

// Discontinue deja de ofrecer el producto desde un día: lo contratado sigue.
func (p *Product) Discontinue(on vocab.Date) error {
	if on.IsZero() {
		return fw.Violation("financial.date", "say from which day")
	}
	p.s.Discontinued = on
	return nil
}

// Reinstate vuelve a ofrecer el producto.
func (p *Product) Reinstate() { p.s.Discontinued = vocab.Date{} }

// AuditSnapshot implementa traits.Snapshotter.
func (p *Product) AuditSnapshot() map[string]any {
	return map[string]any{"code": p.s.Code, "name": p.s.Name, "family": p.s.Family, "regulatoryCode": p.s.RegulatoryCode,
		"discontinued": p.s.Discontinued.String()}
}

// AgreementStatus es si un acuerdo está vigente.
type AgreementStatus string

// Estados de un acuerdo (el C# no tenía ninguno).
const (
	StatusInForce    AgreementStatus = "in-force"
	StatusTerminated AgreementStatus = "terminated"
)

// AgreementState es el estado persistido de un acuerdo.
type AgreementState struct {
	Company     OrganizationID
	Customer    PartyID // con quién está firmado (el acuerdo del C# no tenía contraparte)
	Number      string  // el propio de la entidad, único en ella
	Name        string
	Description string
	Family      string
	Product     ProductID // lo que se contrató
	Signed      vocab.Date
	From        vocab.Date
	Thru        vocab.Date // el último día; cero mientras no tenga fin
	Status      AgreementStatus
	Reason      string // de su terminación
	Audit       traits.AuditStamp
}

// Agreement es un contrato entre una entidad financiera y un cliente: lo que rige las cuentas
// abiertas bajo él.
type Agreement struct {
	fw.BaseAggregateRoot[AgreementID]
	traits.Audited
	s AgreementState
}

// ReconstituteAgreement reconstruye un acuerdo.
func ReconstituteAgreement(id AgreementID, s AgreementState) (*Agreement, error) {
	base, err := fw.NewBaseAggregateRoot(AgreementKind, id)
	if err != nil {
		return nil, err
	}
	s.Number = NormalizeCode(s.Number)
	s.Name, s.Description, s.Reason = strings.TrimSpace(s.Name), strings.TrimSpace(s.Description), strings.TrimSpace(s.Reason)
	s.Family = strings.ToLower(strings.TrimSpace(s.Family))
	var v fw.Validation
	v.Require(!s.Company.IsZero() && !s.Customer.IsZero(), "customer", "required", "the institution and the customer are required")
	v.Require(ValidCode(s.Number, 40), "number", "format", "a number of 1 to 40 letters, digits, dashes, dots or underscores")
	v.Require(s.Name != "" && utf8.RuneCountInString(s.Name) <= 200, "name", "length", "a name of 1 to 200 characters")
	v.Require(utf8.RuneCountInString(s.Description) <= 500 && utf8.RuneCountInString(s.Reason) <= 200, "description", "length",
		"a description of at most 500 and a reason of at most 200 characters")
	v.Require(slices.Contains(Families, s.Family), "family", "enum", "payment, deposit, loan, investment, leasing or other")
	v.Require(!s.Signed.IsZero() && !s.From.IsZero(), "signed", "required", "the day it was signed and the day it takes effect")
	v.Require(s.Thru.IsZero() || !s.Thru.Before(s.From), "thru", "range", "it does not end before it takes effect")
	v.Require(s.Status == StatusInForce || s.Status == StatusTerminated, "status", "enum", "in-force or terminated")
	v.Require(s.Status != StatusTerminated || !s.Thru.IsZero(), "thru", "state", "a terminated agreement has its last day")
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &Agreement{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// Sign registra un acuerdo firmado con un cliente; sin día de efecto, el de la firma.
func Sign(id AgreementID, s AgreementState) (*Agreement, error) {
	s.Status, s.Reason = StatusInForce, ""
	if s.From.IsZero() {
		s.From = s.Signed
	}
	a, err := ReconstituteAgreement(id, s)
	if err != nil {
		return nil, err
	}
	a.Raise(AgreementSigned{EventMeta: a.NewEventMeta(), Company: s.Company.String(), Customer: s.Customer.String(), Number: a.s.Number, Family: a.s.Family,
		Product: optString(s.Product.UUID), Signed: s.Signed, From: a.s.From, Thru: s.Thru})
	return a, nil
}

func optString(u fw.UUID) string {
	if u.IsZero() {
		return ""
	}
	return u.String()
}

// State devuelve el estado.
func (a *Agreement) State() AgreementState { return a.s }

// InForce dice si el acuerdo rige en un día.
func (a *Agreement) InForce(on vocab.Date) bool {
	return !on.Before(a.s.From) && (a.s.Thru.IsZero() || !on.After(a.s.Thru)) &&
		(a.s.Status == StatusInForce || !on.After(a.s.Thru))
}

// Change sustituye el nombre, la descripción y el último día de un acuerdo vigente.
func (a *Agreement) Change(name, description string, thru vocab.Date) error {
	if a.s.Status != StatusInForce {
		return fw.Violation("financial.terminated", "a terminated agreement does not change")
	}
	s := a.s
	s.Name, s.Description, s.Thru = name, description, thru
	n, err := ReconstituteAgreement(a.ID(), s)
	if err != nil {
		return err
	}
	a.s = n.s
	return nil
}

// Terminate termina un acuerdo vigente en un día, diciendo por qué.
func (a *Agreement) Terminate(on vocab.Date, reason string) error {
	if a.s.Status != StatusInForce {
		return fw.Violation("financial.terminated", "the agreement is already terminated")
	}
	if on.Before(a.s.From) {
		return fw.Violation("financial.termination_date", "an agreement does not end before it takes effect")
	}
	s := a.s
	s.Status, s.Thru, s.Reason = StatusTerminated, on, reason
	n, err := ReconstituteAgreement(a.ID(), s)
	if err != nil {
		return err
	}
	a.s = n.s
	a.Raise(AgreementTerminated{EventMeta: a.NewEventMeta(), Company: s.Company.String(), Customer: s.Customer.String(), Number: s.Number, On: on,
		Reason: a.s.Reason})
	return nil
}

// AuditSnapshot implementa traits.Snapshotter.
func (a *Agreement) AuditSnapshot() map[string]any {
	return map[string]any{"number": a.s.Number, "name": a.s.Name, "status": string(a.s.Status), "thru": a.s.Thru.String(), "reason": a.s.Reason}
}

// Campos.
var (
	ProFieldCompany = spec.Comparable("company", func(p *Product) OrganizationID { return p.s.Company })
	ProFieldCode    = spec.Ordered("product_code", func(p *Product) string { return p.s.Code })
	ProFieldFamily  = spec.Comparable("family", func(p *Product) string { return p.s.Family })

	AgrFieldCompany  = spec.Comparable("company", func(a *Agreement) OrganizationID { return a.s.Company })
	AgrFieldCustomer = spec.Comparable("customer", func(a *Agreement) PartyID { return a.s.Customer })
	AgrFieldNumber   = spec.Ordered("agreement_number", func(a *Agreement) string { return a.s.Number })
	AgrFieldStatus   = spec.Comparable("status", func(a *Agreement) string { return string(a.s.Status) })
	AgrFieldProduct  = spec.Comparable("product", func(a *Agreement) ProductID { return a.s.Product })
)

// Repositorios.
type (
	ProductRepository   = fw.Repository[ProductID, *Product]
	AgreementRepository = fw.Repository[AgreementID, *Agreement]
)

// Eventos de un acuerdo.
type (
	// AgreementSigned se lanza cuando se firma un acuerdo.
	AgreementSigned struct {
		fw.EventMeta
		Company  string     `json:"company"`
		Customer string     `json:"customer"`
		Number   string     `json:"number"`
		Family   string     `json:"family"`
		Product  string     `json:"product"`
		Signed   vocab.Date `json:"signed"`
		From     vocab.Date `json:"from"`
		Thru     vocab.Date `json:"thru"`
	}
	// AgreementTerminated se lanza cuando se termina un acuerdo.
	AgreementTerminated struct {
		fw.EventMeta
		Company  string     `json:"company"`
		Customer string     `json:"customer"`
		Number   string     `json:"number"`
		On       vocab.Date `json:"on"`
		Reason   string     `json:"reason"`
	}
)

// Implementaciones de EventType.
func (AgreementSigned) EventType() string     { return "financial.agreement_signed" }
func (AgreementTerminated) EventType() string { return "financial.agreement_terminated" }
