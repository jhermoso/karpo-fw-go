package domain

import (
	"strings"
	"unicode"
	"unicode/utf8"

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
	// BlockReason dice por qué está bloqueado (D-122); se borra al desbloquear.
	BlockReason string
	// UnblockReason dice por qué se desbloqueó la última vez; se borra al bloquear. Bloquear y
	// desbloquear exigen su motivo (decisión de 2026-11-14).
	UnblockReason string
	Audit         traits.AuditStamp
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

// MaxBlockReason es lo que cabe en el motivo de un bloqueo (el BlockReason de Trench).
const MaxBlockReason = 200

// Códigos de las infracciones del bloqueo: bloquear y desbloquear exigen un motivo.
const (
	CodeBlockReasonRequired   = "purchases.block_reason_required"
	CodeUnblockReasonRequired = "purchases.unblock_reason_required"
)

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
	if s.Blocked {
		s.UnblockReason = ""
	} else {
		s.BlockReason = ""
	}
	s.BlockReason, s.UnblockReason = strings.TrimSpace(s.BlockReason), strings.TrimSpace(s.UnblockReason)
	for _, r := range [][2]string{{"blockReason", s.BlockReason}, {"unblockReason", s.UnblockReason}} {
		v.Require(utf8.RuneCountInString(r[1]) <= MaxBlockReason, r[0], "length", "the reason has at most 200 characters")
		v.Require(!strings.ContainsFunc(r[1], unicode.IsControl), r[0], "format", "no control characters")
	}
	v.Require(s.PaymentDays >= 0 && s.PaymentDays <= 365, "paymentDays", "range", "from 0 to 365 days")
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &SupplierProfile{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// NewSupplier da de alta un perfil: si nace bloqueado, con su motivo.
func NewSupplier(id SupplierID, s SupplierState) (*SupplierProfile, error) {
	s.UnblockReason = ""
	p, err := ReconstituteSupplier(id, s)
	if err != nil {
		return nil, err
	}
	if p.s.Blocked && p.s.BlockReason == "" {
		return nil, fw.Violation(CodeBlockReasonRequired, "blocking a supplier needs a reason")
	}
	return p, nil
}

// State devuelve el estado.
func (p *SupplierProfile) State() SupplierState { return p.s }

// Change sustituye los valores por defecto (la empresa y el proveedor se mantienen). Bloquear exige
// el motivo del bloqueo y desbloquear el del desbloqueo; si el estado no cambia, el motivo que no
// llega se queda como estaba.
func (p *SupplierProfile) Change(s SupplierState) error {
	s.Company, s.Supplier, s.Audit = p.s.Company, p.s.Supplier, p.s.Audit
	n, err := ReconstituteSupplier(p.ID(), s)
	if err != nil {
		return err
	}
	switch {
	case n.s.Blocked && !p.s.Blocked && n.s.BlockReason == "":
		return fw.Violation(CodeBlockReasonRequired, "blocking a supplier needs a reason")
	case !n.s.Blocked && p.s.Blocked && n.s.UnblockReason == "":
		return fw.Violation(CodeUnblockReasonRequired, "unblocking a supplier needs a reason")
	case n.s.Blocked && n.s.BlockReason == "":
		n.s.BlockReason = p.s.BlockReason
	case !n.s.Blocked && n.s.UnblockReason == "":
		n.s.UnblockReason = p.s.UnblockReason
	}
	p.s = n.s
	return nil
}

// AuditSnapshot implementa traits.Snapshotter.
func (p *SupplierProfile) AuditSnapshot() map[string]any {
	s := map[string]any{"category": string(p.s.Category), "withholdingRate": p.s.WithholdingRate.String(), "blocked": p.s.Blocked}
	if p.s.BlockReason != "" {
		s["blockReason"] = p.s.BlockReason
	}
	if p.s.UnblockReason != "" {
		s["unblockReason"] = p.s.UnblockReason
	}
	return s
}

// Campos del perfil de proveedor.
var (
	SupFieldCompany  = spec.Comparable("company", func(p *SupplierProfile) OrganizationID { return p.s.Company })
	SupFieldSupplier = spec.Comparable("supplier", func(p *SupplierProfile) PartyID { return p.s.Supplier })
	SupFieldCategory = spec.Comparable("category", func(p *SupplierProfile) Category { return p.s.Category })
	SupFieldBlocked  = spec.Comparable("blocked", func(p *SupplierProfile) bool { return p.s.Blocked })
	// SupFieldPaymentDays ordena por los días de pago, el único orden de la lista de mantenimiento.
	SupFieldPaymentDays = spec.Ordered("payment_days", func(p *SupplierProfile) int { return p.s.PaymentDays })
	// SupFieldIDOrder ordena por la identidad: deshace los empates de una lista paginada.
	SupFieldIDOrder = spec.Ordered("id", func(p *SupplierProfile) string { return p.ID().String() })
)
