package domain

import (
	"slices"
	"strings"
	"time"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Affiliation deja constancia de que la party tiene una relación activa con una organización
// interna. Es lo que hace visible la party a esa organización (decisión P1 del contrato de
// autorización): la relación es la fuente y la afiliación su proyección dentro de la party,
// guardada en la misma unidad de trabajo para que la visibilidad sea una consulta sobre un solo
// agregado. También responde a IPartyOrganizationMembership de C#.
type Affiliation struct {
	Organization PartyID
	Relationship RelationshipID
	Period       vocab.ValidPeriod
}

// Affiliate registra una afiliación a partir de una relación (idempotente por relación).
func (p *Party) Affiliate(org PartyID, rel RelationshipID, from time.Time) error {
	if org == p.ID() {
		return nil
	}
	if slices.ContainsFunc(p.affiliations, func(a Affiliation) bool { return a.Relationship == rel }) {
		return nil
	}
	period, err := vocab.OpenPeriodFrom(from)
	if err != nil {
		return err
	}
	p.affiliations = append(slices.Clone(p.affiliations), Affiliation{Organization: org, Relationship: rel, Period: period})
	p.Raise(PartyAffiliated{EventMeta: p.NewEventMeta(), Organization: org.String(), Relationship: rel.String(), From: period.From()})
	return nil
}

// EndAffiliation termina en un instante la afiliación de una relación (no hace nada si no la conoce).
func (p *Party) EndAffiliation(rel RelationshipID, at time.Time) error {
	k := slices.IndexFunc(p.affiliations, func(a Affiliation) bool { return a.Relationship == rel })
	if k < 0 {
		return nil
	}
	a := p.affiliations[k]
	if end, closed := a.Period.To(); closed && !at.Before(end) {
		return nil
	}
	period, err := vocab.NewValidPeriod(a.Period.From(), &at)
	if err != nil {
		return fw.Violation("parties.affiliation_end_before_start", "an affiliation cannot end before it starts")
	}
	p.affiliations = slices.Clone(p.affiliations)
	p.affiliations[k].Period = period
	p.Raise(PartyAffiliationEnded{EventMeta: p.NewEventMeta(), Organization: a.Organization.String(), Relationship: rel.String(), At: at.UTC()})
	return nil
}

// Affiliations devuelve una copia de las afiliaciones, actuales o pasadas.
func (p *Party) Affiliations() []Affiliation { return slices.Clone(p.affiliations) }

// OrganizationsAt devuelve las organizaciones internas a las que la party está afiliada en t.
func (p *Party) OrganizationsAt(t time.Time) []PartyID {
	var out []PartyID
	for _, a := range p.affiliations {
		if a.Period.IsActiveAt(t) && !slices.Contains(out, a.Organization) {
			out = append(out, a.Organization)
		}
	}
	return out
}

// Share convierte la party en una entrada del catálogo compartido, visible para todas las
// organizaciones (el tipo de party «Public Catalog» de C#: bancos, reguladores, transportistas con
// los que trabaja todo el mundo).
func (p *Party) Share(shared bool) {
	if p.shared == shared {
		return
	}
	p.shared = shared
	p.Raise(PartySharingChanged{EventMeta: p.NewEventMeta(), Shared: shared})
}

// IsShared dice si la party es una entrada del catálogo compartido.
func (p *Party) IsShared() bool { return p.shared }

// LegalForm es la forma jurídica de una organización (los tipos de party 3..8 de C#, que eran
// formas jurídicas, no clasificaciones).
type LegalForm string

// Formas jurídicas, con el GUID del party_type de C# al que sustituyen.
const (
	LegalFormUnspecified        LegalForm = ""
	LegalFormCorporation        LegalForm = "corporation"         // 20000000-0000-0000-0005-000000000003
	LegalFormGovernmentAgency   LegalForm = "government-agency"   // …000000000004
	LegalFormNonProfit          LegalForm = "non-profit"          // …000000000005
	LegalFormPartnership        LegalForm = "partnership"         // …000000000006
	LegalFormSoleProprietorship LegalForm = "sole-proprietorship" // …000000000007
	LegalFormTeam               LegalForm = "team"                // …000000000008 (informal)
)

// ParseLegalForm valida una forma jurídica.
func ParseLegalForm(s string) (LegalForm, error) {
	switch f := LegalForm(strings.ToLower(strings.TrimSpace(s))); f {
	case LegalFormUnspecified, LegalFormCorporation, LegalFormGovernmentAgency, LegalFormNonProfit, LegalFormPartnership,
		LegalFormSoleProprietorship, LegalFormTeam:
		return f, nil
	}
	var v fw.Validation
	v.Add("legalForm", "enum", "legal form must be corporation, government-agency, non-profit, partnership, sole-proprietorship, team or empty")
	return "", v.Err()
}

// SetLegalForm cambia la forma jurídica de una organización.
func (p *Party) SetLegalForm(f LegalForm) error {
	if p.kind != KindOrganization {
		return fw.Violation("parties.kind_mismatch", "only an organization has a legal form")
	}
	if err := p.requireActive("change its legal form"); err != nil {
		return err
	}
	p.organization.LegalForm = f
	return nil
}
