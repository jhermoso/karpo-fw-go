package domain

import (
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// SupplierKind is the stable aggregate type name.
const SupplierKind = "purchases.supplier_profile"

// SupplierState is the persisted state of a supplier profile.
type SupplierState struct {
	Company         OrganizationID
	Supplier        PartyID
	Category        Category      // default category of the lines
	WithholdingRate vocab.Decimal // default withholding (professionals)
	PaymentDays     int           // due date = issue date + days, by default
	IBAN            vocab.IBAN    // default account to pay to
	Blocked         bool          // no new invoices are registered
	Audit           traits.AuditStamp
}

// SupplierProfile is what a company knows of a supplier to book its invoices (the purchase side
// of the C# SupplierRelationshipCommercialProfile, whose withholding, VAT and accounts no code
// read). GL accounts are not here: Accounting's posting profile decides them.
type SupplierProfile struct {
	fw.BaseAggregateRoot[SupplierID]
	traits.Audited
	s SupplierState
}

// ReconstituteSupplier rebuilds a profile.
func ReconstituteSupplier(id SupplierID, s SupplierState) (*SupplierProfile, error) {
	base, err := fw.NewBaseAggregateRoot(SupplierKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(!s.Company.IsZero() && !s.Supplier.IsZero(), "supplier", "required", "company and supplier are required")
	v.Require(s.Category == "" || ValidCategory(s.Category), "category", "enum", "an expense category")
	v.Require(!s.WithholdingRate.IsNegative() && !s.WithholdingRate.GreaterThan(vocab.DecimalFromInt(50)), "withholdingRate", "range", "from 0 to 50")
	v.Require(s.PaymentDays >= 0 && s.PaymentDays <= 365, "paymentDays", "range", "from 0 to 365 days")
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &SupplierProfile{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// State returns the state.
func (p *SupplierProfile) State() SupplierState { return p.s }

// Change replaces the defaults (company and supplier stay).
func (p *SupplierProfile) Change(s SupplierState) error {
	s.Company, s.Supplier, s.Audit = p.s.Company, p.s.Supplier, p.s.Audit
	if _, err := ReconstituteSupplier(p.ID(), s); err != nil {
		return err
	}
	p.s = s
	return nil
}

// AuditSnapshot implements traits.Snapshotter.
func (p *SupplierProfile) AuditSnapshot() map[string]any {
	return map[string]any{"category": string(p.s.Category), "withholdingRate": p.s.WithholdingRate.String(), "blocked": p.s.Blocked}
}

// Supplier profile fields.
var (
	SupFieldCompany  = spec.Comparable("company", func(p *SupplierProfile) OrganizationID { return p.s.Company })
	SupFieldSupplier = spec.Comparable("supplier", func(p *SupplierProfile) PartyID { return p.s.Supplier })
)
