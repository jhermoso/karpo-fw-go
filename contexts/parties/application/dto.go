package application

import (
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// PersonDTO es la representación de transporte de los datos de una persona.
type PersonDTO struct {
	GivenName     string `json:"givenName"`
	FirstSurname  string `json:"firstSurname"`
	SecondSurname string `json:"secondSurname,omitempty"`
	Gender        string `json:"gender,omitempty"`
	BirthDate     string `json:"birthDate,omitempty"`
	MaritalStatus string `json:"maritalStatus,omitempty"`
}

// OrganizationDTO es la representación de transporte de los datos de una organización.
type OrganizationDTO struct {
	LegalName string `json:"legalName"`
	TradeName string `json:"tradeName,omitempty"`
	LegalForm string `json:"legalForm,omitempty"`
}

// RoleDTO es un rol que desempeña una party.
type RoleDTO struct {
	ID       string     `json:"id"`
	RoleType string     `json:"roleType"`
	Name     string     `json:"name"`
	From     time.Time  `json:"from"`
	Until    *time.Time `json:"until,omitempty"`
	Active   bool       `json:"active"`
	Annulled bool       `json:"annulled,omitempty"`
	// SelfEmployed es el perfil de actividad de un rol Self Employed; ausente si no tiene (D-85).
	SelfEmployed *SelfEmployedDTO `json:"selfEmployed,omitempty"`
}

// SelfEmployedDTO es el perfil de actividad de un autónomo.
type SelfEmployedDTO struct {
	TradeName    string `json:"tradeName,omitempty"`
	ActivityCode string `json:"activityCode,omitempty"`
	RegisteredOn string `json:"registeredOn,omitempty"` // YYYY-MM-DD
}

// PartyDTO es la representación de transporte de una party.
type PartyDTO struct {
	ID           string           `json:"id"`
	Kind         string           `json:"kind"`
	Name         string           `json:"name"`
	Person       *PersonDTO       `json:"person,omitempty"`
	Organization *OrganizationDTO `json:"organization,omitempty"`
	Active       bool             `json:"active"`
	Test         bool             `json:"test,omitempty"`
	Shared       bool             `json:"shared,omitempty"`
	Duplicate    bool             `json:"duplicate,omitempty"` // el duplicado de otra party: sólo participa en consultas
	Language     string           `json:"language,omitempty"`  // idioma preferido, ISO 639-1 (D-122)
	// DocumentIssue está presente cuando el documento de identidad de la party se rechazó.
	DocumentIssue *DocumentIssueDTO `json:"documentIssue,omitempty"`
	// DataQualityIssues son los datos de la ficha guardados con formato erróneo (D-92); vacía si no hay.
	DataQualityIssues []DataQualityIssueDTO `json:"dataQualityIssues"`
	Organizations     []string              `json:"organizations"` // organizaciones internas a las que está afiliada ahora
	Roles             []RoleDTO             `json:"roles"`
	Identifications   []IdentificationDTO   `json:"identifications"`
	Contacts          []ContactDTO          `json:"contacts"`
	Classifications   []ClassificationDTO   `json:"classifications"`
	FacilityRoles     []FacilityRoleDTO     `json:"facilityRoles"`
	Version           int64                 `json:"version"`
	CreatedBy         string                `json:"createdBy,omitempty"`
	ModifiedBy        string                `json:"modifiedBy,omitempty"`
}

// DocumentIssueDTO es la marca de un documento de identidad rechazado.
type DocumentIssueDTO struct {
	Number string `json:"number,omitempty"`
	Reason string `json:"reason"`
}

// DataQualityIssueDTO es un dato de la ficha guardado con formato erróneo (D-92).
type DataQualityIssueDTO struct {
	ID       string    `json:"id"`
	Subject  string    `json:"subject,omitempty"` // el contacto
	Field    string    `json:"field"`
	Code     string    `json:"code"`     // INVALID_EMAIL, INVALID_PHONE
	Severity string    `json:"severity"` // warning
	Message  string    `json:"message"`
	Value    string    `json:"value,omitempty"` // tal como se guardó
	Since    time.Time `json:"since"`
}

// WarningDTO es un aviso de la respuesta: el dato se guardó, pero algo en él está mal (D-92).
type WarningDTO struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ContactResult es la respuesta al añadir un contacto: la party y los avisos del contacto nuevo,
// vacíos si su valor está bien.
type ContactResult struct {
	PartyDTO
	Warnings []WarningDTO `json:"warnings"`
}

// IdentificationDTO es un documento de identidad de una party.
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

// ContactDTO es un contacto de una party.
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
	// Owner es la organización dueña del contacto (D-122); ausente si es de todos.
	Owner string `json:"owner,omitempty"`
}

// ClassificationDTO es una clasificación de una party.
type ClassificationDTO struct {
	ID     string     `json:"id"`
	Type   string     `json:"type"`
	Name   string     `json:"name,omitempty"`
	From   time.Time  `json:"from"`
	Until  *time.Time `json:"until,omitempty"`
	Active bool       `json:"active"`
}

// Names son los catálogos con los que se nombran las partes de una party (cualquiera puede ser nil).
type Names struct {
	Roles           *domain.Catalog
	Documents       *domain.DocumentPolicy
	Classifications *domain.ClassificationCatalog
	// Visible dice si quien lee ve lo que es de una organización (un contacto con dueña y sus
	// incidencias, D-122); nil: lo ve todo.
	Visible func(owner domain.PartyID) bool
}

// ToDTO traduce una party, y nombra sus roles, documentos y clasificaciones con los catálogos.
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
	d.Shared, d.Duplicate, d.Organizations = p.IsShared(), p.IsDuplicate(), []string{}
	d.Language = p.Language().String()
	if issue := p.DocumentIssue(); !issue.IsZero() {
		d.DocumentIssue = &DocumentIssueDTO{Number: issue.Number, Reason: issue.Reason}
	}
	d.DataQualityIssues = []DataQualityIssueDTO{}
	for _, i := range p.DataQualityIssues() {
		if !names.sees(i.Owner) {
			continue
		}
		di := DataQualityIssueDTO{ID: i.ID.String(), Field: i.Field, Code: i.Code, Severity: string(i.Severity), Message: i.Message,
			Value: i.Value, Since: i.Since}
		if !i.Subject.IsZero() {
			di.Subject = i.Subject.String()
		}
		d.DataQualityIssues = append(d.DataQualityIssues, di)
	}
	for _, o := range p.OrganizationsAt(now) {
		d.Organizations = append(d.Organizations, o.String())
	}
	for _, r := range p.Roles() {
		rd := RoleDTO{ID: r.ID.String(), RoleType: r.RoleType.String(), From: r.Period.From(), Active: r.IsActiveAt(now), Annulled: r.Annulled}
		if t, ok := r.Period.To(); ok {
			rd.Until = &t
		}
		if se := r.Details.SelfEmployed; !se.IsZero() {
			rd.SelfEmployed = &SelfEmployedDTO{TradeName: se.TradeName, ActivityCode: se.ActivityCode, RegisteredOn: se.RegisteredOn.String()}
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
		if !names.sees(c.Owner) {
			continue // de una organización que quien lee no ve: para él no existe
		}
		cd := ContactDTO{ID: c.ID.String(), Kind: string(c.Kind), Value: c.Value, NonSolicitation: c.NonSolicitation,
			From: c.Period.From(), Active: c.IsActiveAt(now), Purposes: []string{}}
		for _, pp := range c.Purposes {
			cd.Purposes = append(cd.Purposes, string(pp))
		}
		if c.IsOwned() {
			cd.Owner = c.Owner.String()
		}
		if t, ok := c.Period.To(); ok {
			cd.Until = &t
		}
		if c.Kind == domain.ContactPostal {
			a := c.Address
			cd.Address = &AddressDTO{StreetType: a.StreetType, Line1: a.Line1, Line2: a.Line2, Directions: a.Directions,
				Number: a.Premises.Number, NumberQualifier: a.Premises.Qualifier, Block: a.Premises.Block, Portal: a.Premises.Portal,
				Staircase: a.Premises.Staircase, Floor: a.Premises.Floor, Door: a.Premises.Door,
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

// RelationshipDTO es la representación de transporte de una relación.
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
	// Prospect sólo está presente en las relaciones de prospecto.
	Prospect *ProspectDTO `json:"prospect,omitempty"`
	// Ownership sólo está presente en las relaciones de propiedad.
	Ownership *OwnershipDTO `json:"ownership,omitempty"`
	// Collaborator sólo está presente en las relaciones de colaborador.
	Collaborator *CollaboratorDTO `json:"collaborator,omitempty"`
	// Customer sólo está presente en las relaciones de cliente.
	Customer *CustomerDTO `json:"customer,omitempty"`
	// Supplier sólo está presente en las relaciones de proveedor.
	Supplier *SupplierDTO `json:"supplier,omitempty"`
	// DataProtectionConsents son los consentimientos de protección de datos de la pareja (D-122);
	// ausente si no tiene.
	DataProtectionConsents []ConsentDTO `json:"dataProtectionConsents,omitempty"`
	// TransferredFrom es la relación de otra party a la que ésta continúa: la que pasó a la
	// superviviente al fusionar o volvió al deshacer la fusión (D-93).
	TransferredFrom string `json:"transferredFrom,omitempty"`
	// Merge sólo está en la respuesta al marcar un duplicado o deshacer la marca: qué relaciones de
	// negocio pasaron (a la superviviente, o de vuelta) y cuáles se quedaron, para revisarlas una a una.
	Merge   *MergeDTO `json:"merge,omitempty"`
	Version int64     `json:"version"`
}

// MergeDTO es lo que hizo una fusión con las relaciones de negocio de la ficha fusionada (D-93).
type MergeDTO struct {
	Transferred []MergedRelationshipDTO `json:"transferred"`
	// Kept son, al fusionar, las de la fusionada que no pasan (la superviviente ya las tenía, empiezan
	// después, la fusionada es la empresa o quien fusiona no escribe en ella): siguen en la fusionada,
	// bloqueadas con ella, hasta que alguien decida caso por caso. Al deshacer, las de la superviviente
	// que no vuelven porque la que cedió ya tiene otra con esa empresa o quien deshace no escribe en ella.
	Kept []MergedRelationshipDTO `json:"kept"`
}

// MergedRelationshipDTO es una relación de negocio de la ficha fusionada.
type MergedRelationshipDTO struct {
	Relationship string `json:"relationship"`       // la nueva, o la que se quedó
	Previous     string `json:"previous,omitempty"` // la que terminó, si pasó
	Type         string `json:"type"`
	// Counterparty es la otra party de la relación: la empresa, salvo cuando la ficha fusionada es
	// la empresa (entonces, su cliente o proveedor).
	Counterparty string `json:"counterparty"`
}

// CustomerDTO son los datos de una relación de cliente.
type CustomerDTO struct {
	Number string `json:"number,omitempty"`
	// Los códigos con que Sage y Apiscore conocen a la pareja (D-29).
	SageCode     string `json:"sageCode,omitempty"`
	ApiscoreCode string `json:"apiscoreCode,omitempty"`
}

// CollaboratorDTO son los datos de una relación de colaborador.
type CollaboratorDTO struct {
	PromotionCode string `json:"promotionCode,omitempty"`
}

// OwnershipDTO son los datos de una relación de propiedad.
type OwnershipDTO struct {
	Share string `json:"share,omitempty"` // puntos con dos decimales ("30.00"); ausente si se desconoce
}

// ProspectDTO son los datos de una relación de prospecto.
type ProspectDTO struct {
	TrialUntil *time.Time `json:"trialUntil,omitempty"`
	InTrial    bool       `json:"inTrial"`
}

// RelationshipToDTO traduce una relación; types le da nombre y dice qué datos lleva (nil deja el
// nombre vacío y los datos fuera).
func RelationshipToDTO(r *domain.Relationship, types map[domain.RelationshipTypeID]domain.RelationshipType) RelationshipDTO {
	d := RelationshipDTO{ID: r.ID().String(), Type: r.Type().String(), FromParty: r.From().String(), ToParty: r.To().String(),
		FromRole: r.FromRole().String(), ToRole: r.ToRole().String(), Since: r.Since(), Until: r.Until(),
		Active: r.Period().IsActive(), Remark: r.Remark(), Version: r.Version()}
	if id := r.TransferredFrom(); !id.IsZero() {
		d.TransferredFrom = id.String()
	}
	d.DataProtectionConsents = consentDTOs(r)
	if t, ok := types[r.Type()]; ok {
		d.TypeName = t.Name.String()
		switch t.Code {
		case domain.CodeProspect:
			d.Prospect = &ProspectDTO{TrialUntil: r.TrialUntil(), InTrial: r.InTrialAt(fw.Now())}
		case domain.CodeOwnership:
			d.Ownership = &OwnershipDTO{}
			if s := r.OwnershipShare(); s != nil {
				d.Ownership.Share = s.Points().StringFixed(2)
			}
		case domain.CodeCollaborator:
			d.Collaborator = &CollaboratorDTO{PromotionCode: r.PromotionCode()}
		case domain.CodeCustomer:
			x := r.ExternalCodes()
			d.Customer = &CustomerDTO{Number: r.CustomerNumber(), SageCode: x.Sage, ApiscoreCode: x.Apiscore}
		case domain.CodeSupplier:
			x := r.ExternalCodes()
			d.Supplier = &SupplierDTO{Number: r.SupplierNumber(), SageCode: x.Sage, ApiscoreCode: x.Apiscore}
		}
	}
	return d
}

// RoleTypeDTO es una entrada del catálogo de tipos de rol.
type RoleTypeDTO struct {
	ID            string `json:"id"`
	Code          string `json:"code,omitempty"`
	Name          string `json:"name"`
	Description   string `json:"description,omitempty"`
	Parent        string `json:"parent,omitempty"`
	Category      bool   `json:"category"`
	Applicability string `json:"applicability"`
	// WellKnown distingue los tipos que vienen con el programa de los añadidos al catálogo.
	WellKnown bool `json:"wellKnown"`
}

func roleTypeDTO(cat *domain.Catalog, t domain.RoleType) RoleTypeDTO {
	dto := RoleTypeDTO{ID: t.ID.String(), Code: t.Code, Name: t.Name.String(), Description: t.Description, Category: t.Category,
		Applicability: string(cat.Applicability(t.ID)), WellKnown: domain.IsWellKnownRoleType(t.ID)}
	if t.Parent != nil {
		dto.Parent = t.Parent.String()
	}
	return dto
}

// RelationshipTypeDTO es una entrada del catálogo de tipos de relación.
type RelationshipTypeDTO struct {
	ID           string `json:"id"`
	Code         string `json:"code,omitempty"`
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	FromRole     string `json:"fromRole"`
	ToRole       string `json:"toRole"`
	Hierarchical bool   `json:"hierarchical,omitempty"`
	// WellKnown distingue los tipos que vienen con el programa de los añadidos al catálogo.
	WellKnown bool `json:"wellKnown"`
}

func relationshipTypeDTO(t domain.RelationshipType) RelationshipTypeDTO {
	return RelationshipTypeDTO{ID: t.ID.String(), Code: t.Code, Name: t.Name.String(), Description: t.Description,
		FromRole: t.FromRole.String(), ToRole: t.ToRole.String(), Hierarchical: t.Hierarchical,
		WellKnown: domain.IsWellKnownRelationshipType(t.ID)}
}

// sees dice si quien lee ve lo que es de una organización (dueña cero: de todos).
func (n Names) sees(owner domain.PartyID) bool {
	return owner.IsZero() || n.Visible == nil || n.Visible(owner)
}
