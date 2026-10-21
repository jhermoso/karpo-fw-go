package domain

import (
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// SupplierKind es el nombre estable del tipo de agregado.
const SupplierKind = "purchases.supplier_profile"

// SupplierState es el estado persistido de un perfil de proveedor.
type SupplierState struct {
	Company         OrganizationID
	Supplier        PartyID
	Category        Category      // categoría por defecto de las líneas
	WithholdingRate vocab.Decimal // retención por defecto (profesionales)
	PaymentDays     int           // vencimiento = fecha de emisión + días, por defecto
	IBAN            vocab.IBAN    // cuenta por defecto a la que pagar
	Blocked         bool          // no se registran facturas nuevas
	Audit           traits.AuditStamp
}

// SupplierProfile es lo que una empresa sabe de un proveedor para registrar sus facturas (la parte
// de compras del SupplierRelationshipCommercialProfile del C#, cuya retención, IVA y cuentas no
// leía ningún código). Las cuentas contables no están aquí: las decide el perfil de contabilización
// de Accounting.
type SupplierProfile struct {
	fw.BaseAggregateRoot[SupplierID]
	traits.Audited
	s SupplierState
}

// ReconstituteSupplier reconstruye un perfil.
func ReconstituteSupplier(id SupplierID, s SupplierState) (*SupplierProfile, error) {
	base, err := fw.NewBaseAggregateRoot(SupplierKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(!s.Company.IsZero() && !s.Supplier.IsZero(), "supplier", "required", "company and supplier are required")
	v.Require(s.Category == "" || ValidCategory(s.Category), "category", "enum", "an expense category")
	v.Require(!s.WithholdingRate.IsNegative() && !s.WithholdingRate.GreaterThan(vocab.DecimalFromInt(50)) && cents(s.WithholdingRate), "withholdingRate",
		"range", "from 0 to 50, with two decimals")
	v.Require(s.PaymentDays >= 0 && s.PaymentDays <= 365, "paymentDays", "range", "from 0 to 365 days")
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &SupplierProfile{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// State devuelve el estado.
func (p *SupplierProfile) State() SupplierState { return p.s }

// Change sustituye los valores por defecto (la empresa y el proveedor se mantienen).
func (p *SupplierProfile) Change(s SupplierState) error {
	s.Company, s.Supplier, s.Audit = p.s.Company, p.s.Supplier, p.s.Audit
	if _, err := ReconstituteSupplier(p.ID(), s); err != nil {
		return err
	}
	p.s = s
	return nil
}

// AuditSnapshot implementa traits.Snapshotter.
func (p *SupplierProfile) AuditSnapshot() map[string]any {
	return map[string]any{"category": string(p.s.Category), "withholdingRate": p.s.WithholdingRate.String(), "blocked": p.s.Blocked}
}

// Campos del perfil de proveedor.
var (
	SupFieldCompany  = spec.Comparable("company", func(p *SupplierProfile) OrganizationID { return p.s.Company })
	SupFieldSupplier = spec.Comparable("supplier", func(p *SupplierProfile) PartyID { return p.s.Supplier })
)
