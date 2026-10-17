package domain

import (
	"time"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
)

// Repository es el contrato del repositorio de Parties: el contrato del framework especializado
// para Party.
type Repository = fw.Repository[PartyID, *Party]

// Campos tipados: el vocabulario en el que se escriben las especificaciones. Los nombres son
// lógicos; la infraestructura los traduce a columnas.
var (
	FieldID           = spec.Comparable[*Party, PartyID]("id", (*Party).ID)
	FieldType         = spec.Comparable[*Party, PartyType]("type", (*Party).Type)
	FieldLegalName    = spec.Text[*Party]("legal_name", (*Party).LegalName)
	FieldTaxID        = spec.Text[*Party]("tax_id", func(p *Party) string { return p.TaxID().String() })
	FieldActive       = spec.Comparable[*Party, bool]("active", (*Party).IsActive)
	FieldRegisteredAt = spec.Time[*Party]("registered_at", (*Party).RegisteredAt)
	FieldContacts     = spec.Collection[*Party, Contact]("contacts", (*Party).Contacts)

	ContactKindField  = spec.Comparable[Contact, ContactKind]("kind", func(c Contact) ContactKind { return c.Kind })
	ContactValueField = spec.Text[Contact]("value", func(c Contact) string { return c.Value })
	ContactPrimary    = spec.Comparable[Contact, bool]("is_primary", func(c Contact) bool { return c.Primary })
)

// Active selecciona las parties activas.
func Active() spec.Spec[*Party] { return FieldActive.Eq(true) }

// WithTaxID selecciona la party con un identificador fiscal dado.
func WithTaxID(t TaxID) spec.Spec[*Party] { return FieldTaxID.Eq(t.String()) }

// NameContains selecciona las parties cuya razón social contiene text, sin distinguir
// mayúsculas.
func NameContains(text string) spec.Spec[*Party] { return FieldLegalName.ContainsFold(text) }

// RegisteredSince selecciona las parties dadas de alta en t o después.
func RegisteredSince(t time.Time) spec.Spec[*Party] { return FieldRegisteredAt.AtOrAfter(t) }

// ReachableBy selecciona las parties con un contacto principal del tipo dado.
func ReachableBy(kind ContactKind) spec.Spec[*Party] {
	return FieldContacts.Any(ContactKindField.Eq(kind).And(ContactPrimary.Eq(true)))
}

// MinContactsName es el nombre de la especificación propia MinContacts.
const MinContactsName = "parties.min_contacts"

// MinContacts selecciona las parties con al menos n contactos. No se puede expresar con los
// nodos estándar (agrega), así que es una especificación propia: cada adaptador da su
// traducción.
func MinContacts(n int) spec.Spec[*Party] {
	return spec.Custom(MinContactsName, func(p *Party) bool { return len(p.contacts) >= n }, int64(n))
}
