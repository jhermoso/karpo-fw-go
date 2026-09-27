package domain

import (
	"slices"
	"strings"
	"time"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Affiliation records that the party has an active relationship with an internal organization.
// It is what makes the party visible to that organization (decision P1 of the authorization
// contract): the relationship is the source, the affiliation its projection inside the party,
// kept in the same unit of work so visibility is a query over one aggregate. It also answers
// the C# IPartyOrganizationMembership.
type Affiliation struct {
	Organization PartyID
	Relationship RelationshipID
	Period       vocab.ValidPeriod
}

// Affiliate records an affiliation from a relationship (idempotent per relationship).
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

// EndAffiliation ends the affiliation of a relationship at a moment (no-op when unknown).
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

// Affiliations returns a copy of the affiliations, current or past.
func (p *Party) Affiliations() []Affiliation { return slices.Clone(p.affiliations) }

// OrganizationsAt returns the internal organizations the party is affiliated with at t.
func (p *Party) OrganizationsAt(t time.Time) []PartyID {
	var out []PartyID
	for _, a := range p.affiliations {
		if a.Period.IsActiveAt(t) && !slices.Contains(out, a.Organization) {
			out = append(out, a.Organization)
		}
	}
	return out
}

// Share makes the party a shared catalog entry, visible to every organization (the C# "Public
// Catalog" party type: banks, regulators, carriers everybody works with).
func (p *Party) Share(shared bool) {
	if p.shared == shared {
		return
	}
	p.shared = shared
	p.Raise(PartySharingChanged{EventMeta: p.NewEventMeta(), Shared: shared})
}

// IsShared reports whether the party is a shared catalog entry.
func (p *Party) IsShared() bool { return p.shared }

// LegalForm of an organization (the C# party types 3..8, which were legal forms, not
// classifications).
type LegalForm string

// Legal forms, with the GUID of the C# party_type they replace.
const (
	LegalFormUnspecified        LegalForm = ""
	LegalFormCorporation        LegalForm = "corporation"         // 20000000-0000-0000-0005-000000000003
	LegalFormGovernmentAgency   LegalForm = "government-agency"   // …000000000004
	LegalFormNonProfit          LegalForm = "non-profit"          // …000000000005
	LegalFormPartnership        LegalForm = "partnership"         // …000000000006
	LegalFormSoleProprietorship LegalForm = "sole-proprietorship" // …000000000007
	LegalFormTeam               LegalForm = "team"                // …000000000008 (informal)
)

// ParseLegalForm validates a legal form.
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

// SetLegalForm changes the legal form of an organization.
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
