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
	LegalForm string `json:"legalForm,omitempty"`
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
	ID              string              `json:"id"`
	Kind            string              `json:"kind"`
	Name            string              `json:"name"`
	Person          *PersonDTO          `json:"person,omitempty"`
	Organization    *OrganizationDTO    `json:"organization,omitempty"`
	Active          bool                `json:"active"`
	Test            bool                `json:"test,omitempty"`
	Shared          bool                `json:"shared,omitempty"`
	Organizations   []string            `json:"organizations"` // internal organizations it is affiliated with now
	Roles           []RoleDTO           `json:"roles"`
	Identifications []IdentificationDTO `json:"identifications"`
	Contacts        []ContactDTO        `json:"contacts"`
	Classifications []ClassificationDTO `json:"classifications"`
	FacilityRoles   []FacilityRoleDTO   `json:"facilityRoles"`
	Version         int64               `json:"version"`
	CreatedBy       string              `json:"createdBy,omitempty"`
	ModifiedBy      string              `json:"modifiedBy,omitempty"`
}

// IdentificationDTO is an identity document of a party.
type IdentificationDTO struct {
	ID               string `json:"id"`
	DocumentType     string `json:"documentType"`
	Code             string `json:"code,omitempty"`
	Country          string `json:"country"`
	Number           string `json:"number"`
	IssuingAuthority string `json:"issuingAuthority,omitempty"`
	IssuedOn         string `json:"issuedOn,omitempty"`
	ExpiresOn        string `json:"expiresOn,omitempty"`
	Primary          bool   `json:"primary"`
}

// ContactDTO is a contact of a party.
type ContactDTO struct {
	ID              string      `json:"id"`
	Kind            string      `json:"kind"`
	Value           string      `json:"value,omitempty"`
	Address         *AddressDTO `json:"address,omitempty"`
	Purposes        []string    `json:"purposes"`
	NonSolicitation bool        `json:"nonSolicitation,omitempty"`
	From            time.Time   `json:"from"`
	Until           *time.Time  `json:"until,omitempty"`
	Active          bool        `json:"active"`
}

// ClassificationDTO is a classification of a party.
type ClassificationDTO struct {
	ID     string     `json:"id"`
	Type   string     `json:"type"`
	Name   string     `json:"name,omitempty"`
	From   time.Time  `json:"from"`
	Until  *time.Time `json:"until,omitempty"`
	Active bool       `json:"active"`
}

// Names are the catalogs used to name the parts of a party (any may be nil).
type Names struct {
	Roles           *domain.Catalog
	Documents       *domain.DocumentPolicy
	Classifications *domain.ClassificationCatalog
}

// ToDTO maps a party, naming its roles, documents and classifications with the catalogs.
func ToDTO(p *domain.Party, names Names) PartyDTO {
	cat := names.Roles
	d := PartyDTO{ID: p.ID().String(), Kind: string(p.Kind()), Name: p.Name(), Active: p.IsActive(), Test: p.IsTest(),
		Version: p.Version(), CreatedBy: p.CreatedBy().Name, ModifiedBy: p.ModifiedBy().Name, Roles: []RoleDTO{},
		Identifications: []IdentificationDTO{}, Contacts: []ContactDTO{}, Classifications: []ClassificationDTO{}}
	if p.Kind() == domain.KindPerson {
		x := p.Person()
		d.Person = &PersonDTO{GivenName: x.Name.Given(), FirstSurname: x.Name.FirstSurname(), SecondSurname: x.Name.SecondSurname(),
			Gender: string(x.Gender), MaritalStatus: string(x.MaritalStatus)}
		if !x.BirthDate.IsZero() {
			d.Person.BirthDate = x.BirthDate.String()
		}
	} else {
		n := p.Organization().Name
		d.Organization = &OrganizationDTO{LegalName: n.Legal(), TradeName: n.Trade(), LegalForm: string(p.Organization().LegalForm)}
	}
	now := fw.Now()
	d.Shared, d.Organizations = p.IsShared(), []string{}
	for _, o := range p.OrganizationsAt(now) {
		d.Organizations = append(d.Organizations, o.String())
	}
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
	for _, i := range p.Identifications() {
		id := IdentificationDTO{ID: i.ID.String(), DocumentType: i.Type.String(), Country: i.Country.String(), Number: i.Number,
			IssuingAuthority: i.IssuingAuthority, Primary: i.Primary}
		if !i.IssuedOn.IsZero() {
			id.IssuedOn = i.IssuedOn.String()
		}
		if !i.ExpiresOn.IsZero() {
			id.ExpiresOn = i.ExpiresOn.String()
		}
		if names.Documents != nil {
			if t, ok := names.Documents.DocumentType(i.Type); ok {
				id.Code = t.Code
			}
		}
		d.Identifications = append(d.Identifications, id)
	}
	for _, c := range p.Contacts() {
		cd := ContactDTO{ID: c.ID.String(), Kind: string(c.Kind), Value: c.Value, NonSolicitation: c.NonSolicitation,
			From: c.Period.From(), Active: c.IsActiveAt(now), Purposes: []string{}}
		for _, pp := range c.Purposes {
			cd.Purposes = append(cd.Purposes, string(pp))
		}
		if t, ok := c.Period.To(); ok {
			cd.Until = &t
		}
		if c.Kind == domain.ContactPostal {
			a := c.Address
			cd.Address = &AddressDTO{StreetType: a.StreetType, Line1: a.Line1, Line2: a.Line2, Directions: a.Directions,
				PostalCode: a.PostalCode, Locality: a.Locality, Region: a.Region, Country: a.Country.String()}
			if !a.Geo.PostalCode.IsZero() {
				cd.Address.GeoPostalCode = a.Geo.PostalCode.String()
			}
			if !a.Geo.Boundary.IsZero() {
				cd.Address.GeoBoundary = a.Geo.Boundary.String()
			}
		}
		d.Contacts = append(d.Contacts, cd)
	}
	d.FacilityRoles = facilityRoleDTOs(p)
	for _, c := range p.Classifications() {
		cd := ClassificationDTO{ID: c.ID.String(), Type: c.Type.String(), From: c.Period.From(), Active: c.Period.IsActiveAt(now)}
		if t, ok := c.Period.To(); ok {
			cd.Until = &t
		}
		if names.Classifications != nil {
			if t, ok := names.Classifications.Type(c.Type); ok {
				cd.Name = t.Name.String()
			}
		}
		d.Classifications = append(d.Classifications, cd)
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
	ID           string `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	FromRole     string `json:"fromRole"`
	ToRole       string `json:"toRole"`
	Hierarchical bool   `json:"hierarchical,omitempty"`
}
