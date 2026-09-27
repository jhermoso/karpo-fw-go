package application

import (
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// PersonDTO is the transport representation of a person's data.
type PersonDTO struct {
	GivenName     string `json:"givenName"`
	FirstSurname  string `json:"firstSurname"`
	SecondSurname string `json:"secondSurname,omitempty"`
	Gender        string `json:"gender,omitempty"`
	BirthDate     string `json:"birthDate,omitempty"`
	MaritalStatus string `json:"maritalStatus,omitempty"`
}

// OrganizationDTO is the transport representation of an organization's data.
type OrganizationDTO struct {
	LegalName string `json:"legalName"`
	TradeName string `json:"tradeName,omitempty"`
}

// RoleDTO is a role played by a party.
type RoleDTO struct {
	ID       string     `json:"id"`
	RoleType string     `json:"roleType"`
	Name     string     `json:"name"`
	From     time.Time  `json:"from"`
	Until    *time.Time `json:"until,omitempty"`
	Active   bool       `json:"active"`
}

// PartyDTO is the transport representation of a party.
type PartyDTO struct {
	ID           string           `json:"id"`
	Kind         string           `json:"kind"`
	Name         string           `json:"name"`
	Person       *PersonDTO       `json:"person,omitempty"`
	Organization *OrganizationDTO `json:"organization,omitempty"`
	Active       bool             `json:"active"`
	Test         bool             `json:"test,omitempty"`
	Roles        []RoleDTO        `json:"roles"`
	Version      int64            `json:"version"`
	CreatedBy    string           `json:"createdBy,omitempty"`
	ModifiedBy   string           `json:"modifiedBy,omitempty"`
}

// ToDTO maps a party; cat names the roles (nil leaves the names empty).
func ToDTO(p *domain.Party, cat domain.RoleCatalog) PartyDTO {
	d := PartyDTO{ID: p.ID().String(), Kind: string(p.Kind()), Name: p.Name(), Active: p.IsActive(), Test: p.IsTest(),
		Version: p.Version(), CreatedBy: p.CreatedBy().Name, ModifiedBy: p.ModifiedBy().Name, Roles: []RoleDTO{}}
	if p.Kind() == domain.KindPerson {
		x := p.Person()
		d.Person = &PersonDTO{GivenName: x.Name.Given(), FirstSurname: x.Name.FirstSurname(), SecondSurname: x.Name.SecondSurname(),
			Gender: string(x.Gender), MaritalStatus: string(x.MaritalStatus)}
		if !x.BirthDate.IsZero() {
			d.Person.BirthDate = x.BirthDate.String()
		}
	} else {
		n := p.Organization().Name
		d.Organization = &OrganizationDTO{LegalName: n.Legal(), TradeName: n.Trade()}
	}
	now := fw.Now()
	for _, r := range p.Roles() {
		rd := RoleDTO{ID: r.ID.String(), RoleType: r.RoleType.String(), From: r.Period.From(), Active: r.IsActiveAt(now)}
		if t, ok := r.Period.To(); ok {
			rd.Until = &t
		}
		if cat != nil {
			if rt, ok := cat.RoleType(r.RoleType); ok {
				rd.Name = rt.Name.String()
			}
		}
		d.Roles = append(d.Roles, rd)
	}
	return d
}

// RelationshipDTO is the transport representation of a relationship.
type RelationshipDTO struct {
	ID        string     `json:"id"`
	Type      string     `json:"type"`
	TypeName  string     `json:"typeName,omitempty"`
	FromParty string     `json:"fromParty"`
	ToParty   string     `json:"toParty"`
	FromRole  string     `json:"fromRole"`
	ToRole    string     `json:"toRole"`
	Since     time.Time  `json:"since"`
	Until     *time.Time `json:"until,omitempty"`
	Active    bool       `json:"active"`
	Remark    string     `json:"remark,omitempty"`
	Version   int64      `json:"version"`
}

// RelationshipToDTO maps a relationship; types names it (nil leaves the name empty).
func RelationshipToDTO(r *domain.Relationship, types map[domain.RelationshipTypeID]domain.RelationshipType) RelationshipDTO {
	d := RelationshipDTO{ID: r.ID().String(), Type: r.Type().String(), FromParty: r.From().String(), ToParty: r.To().String(),
		FromRole: r.FromRole().String(), ToRole: r.ToRole().String(), Since: r.Since(), Until: r.Until(),
		Active: r.Period().IsActive(), Remark: r.Remark(), Version: r.Version()}
	if t, ok := types[r.Type()]; ok {
		d.TypeName = t.Name.String()
	}
	return d
}

// RoleTypeDTO is an entry of the role type catalog.
type RoleTypeDTO struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description,omitempty"`
	Parent        string `json:"parent,omitempty"`
	Category      bool   `json:"category"`
	Applicability string `json:"applicability"`
}

// RelationshipTypeDTO is an entry of the relationship type catalog.
type RelationshipTypeDTO struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	FromRole    string `json:"fromRole"`
	ToRole      string `json:"toRole"`
}
