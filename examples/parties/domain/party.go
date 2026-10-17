// Package domain es el modelo de dominio del contexto delimitado Parties (UDM Vol. 1 «Party»),
// escrito sólo contra los paquetes de dominio puros del framework. Muestra la traducción a Go de
// los patrones tácticos de Karpo C#: identidad tipada, objetos de valor con constructores que
// validan, una raíz de agregado con entidades hijas, invariantes, eventos de dominio y
// especificaciones.
package domain

import (
	"slices"
	"strings"
	"time"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
)

// Kind es el nombre estable del tipo de agregado.
const Kind = "parties.party"

// PartyID identifica una Party.
type PartyID struct{ fw.UUID }

// NewPartyID devuelve una identidad nueva.
func NewPartyID() PartyID { return PartyID{fw.NewUUID()} }

// ParsePartyID analiza una identidad en texto.
func ParsePartyID(s string) (PartyID, error) {
	u, err := fw.ParseUUID(s)
	return PartyID{u}, err
}

// PartyType distingue personas de organizaciones.
type PartyType string

// Tipos de party.
const (
	Person       PartyType = "person"
	Organization PartyType = "organization"
)

// TaxID es un identificador fiscal normalizado (objeto de valor).
type TaxID struct{ value string }

// NewTaxID valida y normaliza un identificador fiscal.
func NewTaxID(s string) (TaxID, error) {
	n := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(s), "-", ""))
	var v fw.Validation
	v.Require(len(n) >= 5 && len(n) <= 20, "taxId", "format", "tax id must have between 5 and 20 characters")
	if err := v.Err(); err != nil {
		return TaxID{}, err
	}
	return TaxID{value: n}, nil
}

func (t TaxID) String() string { return t.value }

// ContactKind es el canal de un medio de contacto.
type ContactKind string

// Tipos de contacto.
const (
	Email ContactKind = "email"
	Phone ContactKind = "phone"
)

// Contact es un valor hijo del agregado Party.
type Contact struct {
	Kind    ContactKind
	Value   string
	Primary bool
}

// Party es la raíz del agregado. Los rasgos transversales se componen, no se heredan: la
// activación (activa/inactiva) y el sello de auditoría (creado/modificado por, que pone la capa
// de aplicación).
type Party struct {
	fw.BaseAggregateRoot[PartyID]
	traits.Activation
	traits.Audited
	partyType    PartyType
	legalName    string
	taxID        TaxID
	registeredAt time.Time
	contacts     []Contact
}

// Register crea una party activa nueva y lanza PartyRegistered.
func Register(id PartyID, t PartyType, legalName string, taxID TaxID) (*Party, error) {
	p, err := Reconstitute(id, t, legalName, taxID, true, fw.Now(), traits.AuditStamp{}, nil)
	if err != nil {
		return nil, err
	}
	p.Raise(PartyRegistered{EventMeta: p.NewEventMeta(), Type: string(t), LegalName: p.legalName, TaxID: taxID.String()})
	return p, nil
}

// Reconstitute reconstruye una party a partir del estado persistido (sin eventos, con las
// invariantes comprobadas de nuevo).
func Reconstitute(id PartyID, t PartyType, legalName string, taxID TaxID, active bool, registeredAt time.Time,
	audit traits.AuditStamp, contacts []Contact) (*Party, error) {
	base, err := fw.NewBaseAggregateRoot(Kind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(t == Person || t == Organization, "type", "enum", "type must be person or organization")
	v.Require(strings.TrimSpace(legalName) != "", "legalName", "required", "legal name is required")
	v.Require(taxID != TaxID{}, "taxId", "required", "tax id is required")
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &Party{
		BaseAggregateRoot: base, Activation: traits.RestoredActivation(active), Audited: traits.RestoredAudit(audit),
		partyType: t, legalName: strings.TrimSpace(legalName), taxID: taxID,
		registeredAt: registeredAt.UTC(), contacts: slices.Clone(contacts),
	}, nil
}

// Type devuelve el tipo de party.
func (p *Party) Type() PartyType { return p.partyType }

// LegalName devuelve la razón social.
func (p *Party) LegalName() string { return p.legalName }

// TaxID devuelve el identificador fiscal.
func (p *Party) TaxID() TaxID { return p.taxID }

// RegisteredAt devuelve el instante del alta.
func (p *Party) RegisteredAt() time.Time { return p.registeredAt }

// Contacts devuelve una copia de los contactos.
func (p *Party) Contacts() []Contact { return slices.Clone(p.contacts) }

// Rename cambia la razón social.
func (p *Party) Rename(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		var v fw.Validation
		v.Add("legalName", "required", "legal name is required")
		return v.Err()
	}
	if !p.IsActive() {
		return fw.Violation("parties.inactive", "an inactive party cannot be renamed")
	}
	if name == p.legalName {
		return nil
	}
	old := p.legalName
	p.legalName = name
	p.Raise(PartyRenamed{EventMeta: p.NewEventMeta(), From: old, To: name})
	return nil
}

// AddContact añade un contacto. Invariantes: sin duplicados, como mucho un contacto principal
// por tipo.
func (p *Party) AddContact(c Contact) error {
	var v fw.Validation
	v.Require(c.Kind == Email || c.Kind == Phone, "kind", "enum", "kind must be email or phone")
	v.Require(strings.TrimSpace(c.Value) != "", "value", "required", "value is required")
	if c.Kind == Email {
		v.Require(strings.Contains(c.Value, "@"), "value", "format", "invalid e-mail")
	}
	if err := v.Err(); err != nil {
		return err
	}
	for _, existing := range p.contacts {
		if existing.Kind == c.Kind && strings.EqualFold(existing.Value, c.Value) {
			return fw.Violation("parties.duplicate_contact", "contact already registered")
		}
	}
	if c.Primary {
		for i := range p.contacts {
			if p.contacts[i].Kind == c.Kind {
				p.contacts[i].Primary = false
			}
		}
	}
	p.contacts = append(slices.Clone(p.contacts), c)
	p.Raise(ContactAdded{EventMeta: p.NewEventMeta(), Kind: string(c.Kind), Value: c.Value, Primary: c.Primary})
	return nil
}

// Deactivate marca la party como inactiva.
func (p *Party) Deactivate() {
	if p.Activation.Deactivate() {
		p.Raise(PartyDeactivated{EventMeta: p.NewEventMeta()})
	}
}

// AuditSnapshot implementa traits.Snapshotter: los campos cuyos cambios se auditan.
func (p *Party) AuditSnapshot() map[string]any {
	contacts := make([]string, len(p.contacts))
	for i, c := range p.contacts {
		contacts[i] = string(c.Kind) + ":" + c.Value
	}
	return map[string]any{
		"type":      string(p.partyType),
		"legalName": p.legalName,
		"taxId":     p.taxID.String(),
		"active":    p.IsActive(),
		"contacts":  strings.Join(contacts, ","),
	}
}
