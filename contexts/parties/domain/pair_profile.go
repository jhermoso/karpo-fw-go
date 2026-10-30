package domain

import (
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// El perfil de la pareja empresa–tercero que vive en la relación (docs/DECISIONES-COMUNES.md, D-122):
// el número de proveedor, los códigos externos y los consentimientos de protección de datos. Lo
// demás del perfil (crédito, bloqueos, condiciones) está en los contextos que lo usan.

// SupplierDetails son los datos de una relación de proveedor: el número con el que la organización
// interna conoce al proveedor, como el número de cliente de la relación de cliente (D-10).
type SupplierDetails struct {
	// Number se escribe en mayúsculas; vacío cuando la organización no ha dado ninguno.
	Number string
}

// ExternalCodes son los códigos con los que otros sistemas conocen a la pareja (D-29): el de Sage y
// el de Apiscore. Se guardan tal como llegan, recortados.
type ExternalCodes struct {
	Sage     string
	Apiscore string
}

// Longitudes de los códigos externos (las de Trench: el código de Sage es su CustomerCodeInTenant,
// de 15, y el de Apiscore su CustomerNumber, de 50).
const (
	MaxSageCode     = 15
	MaxApiscoreCode = 50
)

// SupplierNumber devuelve el número con el que la organización conoce al proveedor ("" si no
// tiene o no es una relación de proveedor).
func (r *Relationship) SupplierNumber() string { return r.details.Supplier.Number }

// ExternalCodes devuelve los códigos externos de la pareja.
func (r *Relationship) ExternalCodes() ExternalCodes { return r.details.External }

// SetSupplierNumber da al proveedor su número en la organización, lo cambia o (con "") se lo quita,
// con las reglas del número de cliente: hasta 20 letras, dígitos, guiones, puntos, barras y guiones
// bajos, en mayúsculas. Sólo los tipos con código CodeSupplier lo llevan. Que ningún otro proveedor
// de la organización tenga el número lo comprueba quien llama.
func (r *Relationship) SetSupplierNumber(t RelationshipType, number string) error {
	if t.ID != r.relType || t.Code != CodeSupplier {
		return fw.Violation("parties.not_a_supplier_relationship", "only a supplier relationship has a supplier number")
	}
	if err := r.requireOpen(); err != nil {
		return err
	}
	number = NormalizeCustomerNumber(number)
	if !validPairNumber(number) {
		var v fw.Validation
		v.Add("supplierNumber", "format", "up to 20 letters, digits, dashes, dots, slashes and underscores")
		return v.Err()
	}
	r.details.Supplier.Number = number
	return nil
}

// validPairNumber dice si un número de cliente o de proveedor, ya normalizado, tiene buena forma.
func validPairNumber(number string) bool {
	valid := len(number) <= MaxCustomerNumber
	for _, c := range number {
		valid = valid && (c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' || c == '/')
	}
	return valid
}

// SetExternalCodes sustituye los códigos externos de una relación de cliente o de proveedor; uno
// vacío se quita.
func (r *Relationship) SetExternalCodes(t RelationshipType, codes ExternalCodes) error {
	if t.ID != r.relType || (t.Code != CodeCustomer && t.Code != CodeSupplier) {
		return fw.Violation("parties.not_a_pair_relationship", "only a customer or supplier relationship has external codes")
	}
	if err := r.requireOpen(); err != nil {
		return err
	}
	codes.Sage, codes.Apiscore = strings.TrimSpace(codes.Sage), strings.TrimSpace(codes.Apiscore)
	var v fw.Validation
	v.Require(utf8.RuneCountInString(codes.Sage) <= MaxSageCode, "sageCode", "length", "the Sage code has at most 15 characters")
	v.Require(utf8.RuneCountInString(codes.Apiscore) <= MaxApiscoreCode, "apiscoreCode", "length", "the Apiscore code has at most 50 characters")
	v.Require(!strings.ContainsFunc(codes.Sage, unicode.IsControl), "sageCode", "format", "no control characters")
	v.Require(!strings.ContainsFunc(codes.Apiscore, unicode.IsControl), "apiscoreCode", "format", "no control characters")
	if err := v.Err(); err != nil {
		return err
	}
	r.details.External = codes
	return nil
}

// ConsentID identifica un consentimiento de protección de datos (entidad hija de la relación).
type ConsentID struct{ fw.UUID }

// NewConsentID devuelve una identidad nueva.
func NewConsentID() ConsentID { return ConsentID{fw.NewUUID()} }

// ParseConsentID interpreta una identidad en texto.
func ParseConsentID(s string) (ConsentID, error) { u, err := fw.ParseUUID(s); return ConsentID{u}, err }

// LegalBasis es la base legal de un tratamiento de datos personales (RGPD, artículo 6.1).
type LegalBasis string

// Bases legales.
const (
	BasisConsent             LegalBasis = "consent"
	BasisContract            LegalBasis = "contract"
	BasisLegalObligation     LegalBasis = "legal-obligation"
	BasisVitalInterests      LegalBasis = "vital-interests"
	BasisPublicTask          LegalBasis = "public-task"
	BasisLegitimateInterests LegalBasis = "legitimate-interests"
)

// ParseLegalBasis valida una base legal.
func ParseLegalBasis(s string) (LegalBasis, error) {
	switch b := LegalBasis(strings.ToLower(strings.TrimSpace(s))); b {
	case BasisConsent, BasisContract, BasisLegalObligation, BasisVitalInterests, BasisPublicTask, BasisLegitimateInterests:
		return b, nil
	}
	var v fw.Validation
	v.Add("legalBasis", "enum", "legal basis must be consent, contract, legal-obligation, vital-interests, public-task or legitimate-interests")
	return "", v.Err()
}

// Límites de un consentimiento.
const (
	MaxConsentPurpose  = 50
	MaxConsentNotes    = 500
	MaxRetentionMonths = 1200
)

// DataProtectionConsent es lo que una empresa, como responsable del tratamiento, sabe de cómo trata
// los datos de un tercero (D-122): con qué base legal, para qué, desde cuándo, hasta cuándo
// (retirada), cuánto tiempo los conserva y si el tercero se ha opuesto. Cada empresa es un
// responsable distinto, así que va en la pareja, no en la party.
type DataProtectionConsent struct {
	ID         ConsentID
	LegalBasis LegalBasis
	Purpose    string
	GrantedAt  time.Time
	// WithdrawnAt es cuándo se retiró; nil mientras está vigente.
	WithdrawnAt *time.Time
	// RetentionMonths es el plazo de conservación en meses; 0 si no se ha dicho.
	RetentionMonths int
	// Objection dice que el tercero se ha opuesto al tratamiento (RGPD, artículo 21).
	Objection bool
	Notes     string
}

// IsActive dice si el consentimiento no se ha retirado.
func (c DataProtectionConsent) IsActive() bool { return c.WithdrawnAt == nil }

// ConsentData es lo que se aporta al registrar un consentimiento.
type ConsentData struct {
	LegalBasis      LegalBasis
	Purpose         string
	GrantedAt       time.Time
	RetentionMonths int
	Objection       bool
	Notes           string
}

func (d ConsentData) normalized() (ConsentData, error) {
	d.Purpose, d.Notes = strings.Join(strings.Fields(d.Purpose), " "), strings.TrimSpace(d.Notes)
	var v fw.Validation
	v.Require(d.LegalBasis != "", "legalBasis", "required", "the legal basis is required")
	v.Require(d.Purpose != "", "purpose", "required", "the purpose is required")
	v.Require(utf8.RuneCountInString(d.Purpose) <= MaxConsentPurpose, "purpose", "length", "the purpose has at most 50 characters")
	v.Require(utf8.RuneCountInString(d.Notes) <= MaxConsentNotes, "notes", "length", "the notes have at most 500 characters")
	v.Require(!strings.ContainsFunc(d.Notes, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' }),
		"notes", "format", "no control characters")
	v.Require(d.RetentionMonths >= 0 && d.RetentionMonths <= MaxRetentionMonths, "retentionMonths", "range", "from 1 to 1200 months, or none")
	v.Require(!d.GrantedAt.IsZero(), "grantedAt", "required", "when it was granted is required")
	v.Require(!d.GrantedAt.After(fw.Now()), "grantedAt", "future", "a consent cannot be granted in the future")
	d.GrantedAt = d.GrantedAt.UTC()
	return d, v.Err()
}

// Consents devuelve una copia de los consentimientos de la pareja, vigentes o retirados.
func (r *Relationship) Consents() []DataProtectionConsent { return slices.Clone(r.consents) }

// RecordConsent registra un consentimiento de la pareja; org es la empresa (el otro lado es el
// tercero). La relación tiene que estar en vigor, y no puede haber otro vigente con la misma
// finalidad.
func (r *Relationship) RecordConsent(org PartyID, d ConsentData) (ConsentID, error) {
	if !r.Involves(org) {
		return ConsentID{}, fw.Violation("parties.consent_needs_company", "a data protection consent is given to a company of the pair")
	}
	if err := r.requireOpen(); err != nil {
		return ConsentID{}, err
	}
	d, err := d.normalized()
	if err != nil {
		return ConsentID{}, err
	}
	for _, c := range r.consents {
		if c.IsActive() && strings.EqualFold(c.Purpose, d.Purpose) {
			return ConsentID{}, fw.Violation("parties.consent_duplicate_purpose", "the pair already has a consent in force for that purpose; withdraw it first")
		}
	}
	c := DataProtectionConsent{ID: NewConsentID(), LegalBasis: d.LegalBasis, Purpose: d.Purpose, GrantedAt: d.GrantedAt,
		RetentionMonths: d.RetentionMonths, Objection: d.Objection, Notes: d.Notes}
	r.consents = append(slices.Clone(r.consents), c)
	r.Raise(DataProtectionConsentRecorded{EventMeta: r.NewEventMeta(), ConsentID: c.ID.String(), Organization: org.String(),
		Party: r.other(org).String(), LegalBasis: string(c.LegalBasis), Purpose: c.Purpose, GrantedAt: c.GrantedAt, Objection: c.Objection})
	return c.ID, nil
}

// WithdrawConsent retira un consentimiento en un instante (no antes de que se otorgara). Se puede
// retirar aunque la relación haya terminado: retirar el consentimiento es un derecho del tercero.
func (r *Relationship) WithdrawConsent(org PartyID, id ConsentID, at time.Time) error {
	k := slices.IndexFunc(r.consents, func(c DataProtectionConsent) bool { return c.ID == id })
	if k < 0 {
		return fw.NotFound("parties.data_protection_consent", id)
	}
	c := r.consents[k]
	if !c.IsActive() {
		return fw.Violation("parties.consent_withdrawn", "the consent has already been withdrawn")
	}
	at = at.UTC()
	if at.After(fw.Now()) {
		var v fw.Validation
		v.Add("at", "future", "a consent cannot be withdrawn in the future")
		return v.Err()
	}
	if at.Before(c.GrantedAt) {
		return fw.Violation("parties.consent_withdrawn_before_granted", "a consent cannot be withdrawn before it was granted")
	}
	r.consents = slices.Clone(r.consents)
	r.consents[k].WithdrawnAt = &at
	r.Raise(DataProtectionConsentWithdrawn{EventMeta: r.NewEventMeta(), ConsentID: id.String(), Organization: org.String(),
		Party: r.other(org).String(), Purpose: c.Purpose, WithdrawnAt: at})
	return nil
}

// other devuelve el lado de la relación que no es p.
func (r *Relationship) other(p PartyID) PartyID {
	if r.from == p {
		return r.to
	}
	return r.from
}

// consentsText resume los consentimientos para el registro de auditoría ("" si no hay).
func consentsText(cs []DataProtectionConsent) string {
	parts := make([]string, 0, len(cs))
	for _, c := range cs {
		s := c.ID.String() + ":" + string(c.LegalBasis) + ":" + c.Purpose + ":" + c.GrantedAt.Format(time.RFC3339)
		if c.WithdrawnAt != nil {
			s += ":" + c.WithdrawnAt.Format(time.RFC3339)
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, ",")
}
