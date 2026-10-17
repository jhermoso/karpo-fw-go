// Código generado a partir del WellKnownCatalog de C# (RoleTypes, RelationshipTypes). Los GUID son
// los mismos que en el catálogo de C#, así que los datos migrados de Karpo conservan sus
// referencias. Diferencias con el catálogo de C# (ver docs/PARTIES.md):
//   - los roles de pedido (…0001-0000000000C0..C8) no son roles de party: pertenecen a Orders;
//   - Salesperson y Collaborator, que usan los tipos de relación pero faltan en la jerarquía de
//     C#, cuelgan de Person y de la raíz;
//   - DepartmentAssignment (…0002-000000000012) está declarado en C# sin fila en el catálogo.
//   - Organization Rollup relaciona cualquier unidad organizativa con cualquier organización y es
//     jerárquico (un padre, sin ciclos). La fila de C# decía Department -> Division, pero el
//     IOrganizationHierarchy de C# recorría los rollups hacia abajo desde las organizaciones legales.
//   - Prospect Relationship (…0002-000000000017) y los códigos de los tipos de relación no
//     existen en C#, donde «potencial» es un estado de la relación de cliente (docs/PARTIES-UDM.md,
//     decisión PU-5). Era …0016 hasta que Javier devolvió ese id al Duplicate Of de Trench
//     (docs/DECISIONES-COMUNES.md, D-62).

package domain

import "github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"

// Tipos de rol conocidos.
var (
	RolePartyRoot                   = MustRoleTypeID("10000000-0000-0000-0001-000000000100")
	RolePersonCategory              = MustRoleTypeID("10000000-0000-0000-0001-000000000101")
	RoleOrganizationCategory        = MustRoleTypeID("10000000-0000-0000-0001-000000000102")
	RoleLegalCategory               = MustRoleTypeID("10000000-0000-0000-0001-000000000103")
	RoleInformalCategory            = MustRoleTypeID("10000000-0000-0000-0001-000000000104")
	RoleOrganizationUnitCategory    = MustRoleTypeID("10000000-0000-0000-0001-000000000105")
	RoleDistributionChannelCategory = MustRoleTypeID("10000000-0000-0000-0001-000000000106")
	RoleInternalOrganization        = MustRoleTypeID("10000000-0000-0000-0001-000000000001")
	RoleSupplier                    = MustRoleTypeID("10000000-0000-0000-0001-000000000003")
	RolePartner                     = MustRoleTypeID("10000000-0000-0000-0001-000000000004")
	RoleCompetitor                  = MustRoleTypeID("10000000-0000-0000-0001-000000000005")
	RoleRegulatory                  = MustRoleTypeID("10000000-0000-0000-0001-000000000006")
	RoleCorporation                 = MustRoleTypeID("10000000-0000-0000-0001-000000000032")
	RoleAssociation                 = MustRoleTypeID("10000000-0000-0000-0001-000000000034")
	RoleCarrier                     = MustRoleTypeID("10000000-0000-0000-0001-000000000024")
	RoleFinancialInstitution        = MustRoleTypeID("10000000-0000-0000-0001-000000000035")
	RoleDivision                    = MustRoleTypeID("10000000-0000-0000-0001-000000000008")
	RoleDepartment                  = MustRoleTypeID("10000000-0000-0000-0001-000000000009")
	RoleSubsidiary                  = MustRoleTypeID("10000000-0000-0000-0001-000000000033")
	RoleParentOrganization          = MustRoleTypeID("10000000-0000-0000-0001-000000000036")
	RoleTeam                        = MustRoleTypeID("10000000-0000-0000-0001-000000000037")
	RoleClub                        = MustRoleTypeID("10000000-0000-0000-0001-000000000038")
	RoleGroup                       = MustRoleTypeID("10000000-0000-0000-0001-000000000039")
	RoleAgent                       = MustRoleTypeID("10000000-0000-0000-0001-000000000022")
	RoleDistributor                 = MustRoleTypeID("10000000-0000-0000-0001-000000000023")
	RoleEmployee                    = MustRoleTypeID("10000000-0000-0000-0001-000000000010")
	RoleContractor                  = MustRoleTypeID("10000000-0000-0000-0001-000000000011")
	RoleFamilyMember                = MustRoleTypeID("10000000-0000-0000-0001-000000000012")
	RoleContact                     = MustRoleTypeID("10000000-0000-0000-0001-000000000014")
	RoleSelfEmployed                = MustRoleTypeID("10000000-0000-0000-0001-000000000030")
	RoleAuthorizedRepresentative    = MustRoleTypeID("10000000-0000-0000-0001-000000000031")
	RoleCustomer                    = MustRoleTypeID("10000000-0000-0000-0001-000000000002")
	RoleProspect                    = MustRoleTypeID("10000000-0000-0000-0001-000000000007")
	RoleShareholder                 = MustRoleTypeID("10000000-0000-0000-0001-000000000015")
	RoleBillToCustomer              = MustRoleTypeID("10000000-0000-0000-0001-000000000020")
	RoleShipToCustomer              = MustRoleTypeID("10000000-0000-0000-0001-000000000021")
	RoleEndUserCustomer             = MustRoleTypeID("10000000-0000-0000-0001-000000000025")
	RoleCollaborator                = MustRoleTypeID("10000000-0000-0000-0001-000000000060")
	RoleSalesperson                 = MustRoleTypeID("10000000-0000-0000-0001-0000000000C3")
)

// Tipos de relación conocidos.
var (
	RelEmployment              = MustRelationshipTypeID("10000000-0000-0000-0002-000000000001")
	RelCustomer                = MustRelationshipTypeID("10000000-0000-0000-0002-000000000002")
	RelSupplier                = MustRelationshipTypeID("10000000-0000-0000-0002-000000000003")
	RelPartnership             = MustRelationshipTypeID("10000000-0000-0000-0002-000000000004")
	RelOrganizationRollup      = MustRelationshipTypeID("10000000-0000-0000-0002-000000000005")
	RelFamily                  = MustRelationshipTypeID("10000000-0000-0000-0002-000000000006")
	RelOwnership               = MustRelationshipTypeID("10000000-0000-0000-0002-000000000007")
	RelAgent                   = MustRelationshipTypeID("10000000-0000-0000-0002-000000000008")
	RelContact                 = MustRelationshipTypeID("10000000-0000-0000-0002-000000000009")
	RelSubsidiary              = MustRelationshipTypeID("10000000-0000-0000-0002-000000000010")
	RelLegalRepresentation     = MustRelationshipTypeID("10000000-0000-0000-0002-000000000011")
	RelDepartmentAssignment    = MustRelationshipTypeID("10000000-0000-0000-0002-000000000012")
	RelCollaboratorContact     = MustRelationshipTypeID("10000000-0000-0000-0002-000000000013")
	RelCollaborator            = MustRelationshipTypeID("10000000-0000-0000-0002-000000000014")
	RelCollaboratorOrigination = MustRelationshipTypeID("10000000-0000-0000-0002-000000000015")
	RelDuplicateOf             = MustRelationshipTypeID("10000000-0000-0000-0002-000000000016")
	RelProspect                = MustRelationshipTypeID("10000000-0000-0000-0002-000000000017")
)

// Códigos de los tipos de relación cuyas relaciones llevan detalles propios.
const (
	CodeProspect     = "prospect"
	CodeOwnership    = "ownership"
	CodeCollaborator = "collaborator"
	CodeCustomer     = "customer"
)

func parent(id RoleTypeID) *RoleTypeID { return &id }

// WellKnownRoleTypes devuelve la semilla del catálogo de tipos de rol. El código de cada uno es su
// nombre en minúsculas y con guiones ("internal-organization").
func WellKnownRoleTypes() []RoleType {
	types := wellKnownRoleTypes()
	for i := range types {
		types[i].Code = CodeOf(types[i].Name.String())
	}
	return types
}

// IsWellKnownRoleType dice si un tipo de rol viene con el programa (y de él cuelgan reglas del
// código) en lugar de haberse añadido al catálogo.
func IsWellKnownRoleType(id RoleTypeID) bool {
	for _, t := range wellKnownRoleTypes() {
		if t.ID == id {
			return true
		}
	}
	return false
}

func wellKnownRoleTypes() []RoleType {
	return []RoleType{
		{ID: RolePartyRoot, Name: vocab.MustName("Party"), Description: "Root of the role type hierarchy", Category: true, Parent: nil},
		{ID: RolePersonCategory, Name: vocab.MustName("Person"), Description: "Roles exclusive to persons", Category: true, Parent: parent(RolePartyRoot)},
		{ID: RoleOrganizationCategory, Name: vocab.MustName("Organization"), Description: "Roles exclusive to organizations", Category: true, Parent: parent(RolePartyRoot)},
		{ID: RoleLegalCategory, Name: vocab.MustName("Legal"), Description: "Legal organization types", Category: true, Parent: parent(RoleOrganizationCategory)},
		{ID: RoleInformalCategory, Name: vocab.MustName("Informal"), Description: "Informal organization types (groups without legal status)", Category: true, Parent: parent(RoleOrganizationCategory)},
		{ID: RoleOrganizationUnitCategory, Name: vocab.MustName("Organization Unit"), Description: "Organization structural units", Category: true, Parent: parent(RoleOrganizationCategory)},
		{ID: RoleDistributionChannelCategory, Name: vocab.MustName("Distribution Channel"), Description: "Distribution channel types", Category: true, Parent: parent(RoleLegalCategory)},
		{ID: RoleInternalOrganization, Name: vocab.MustName("Internal Organization"), Description: "A party that is part of the enterprise", Category: false, Parent: parent(RolePartyRoot)},
		{ID: RoleSupplier, Name: vocab.MustName("Supplier"), Description: "A party that provides goods or services", Category: false, Parent: parent(RolePartyRoot)},
		{ID: RolePartner, Name: vocab.MustName("Partner"), Description: "A party with a business partnership relationship", Category: false, Parent: parent(RoleLegalCategory)},
		{ID: RoleCompetitor, Name: vocab.MustName("Competitor"), Description: "A party that competes in the same market", Category: false, Parent: parent(RoleLegalCategory)},
		{ID: RoleRegulatory, Name: vocab.MustName("Regulatory Agency"), Description: "A regulatory body or government agency", Category: false, Parent: parent(RoleLegalCategory)},
		{ID: RoleCorporation, Name: vocab.MustName("Corporation"), Description: "A corporation entity", Category: false, Parent: parent(RoleLegalCategory)},
		{ID: RoleAssociation, Name: vocab.MustName("Association"), Description: "A professional or trade association", Category: false, Parent: parent(RoleLegalCategory)},
		{ID: RoleCarrier, Name: vocab.MustName("Carrier"), Description: "A party that transports goods", Category: false, Parent: parent(RoleLegalCategory)},
		{ID: RoleFinancialInstitution, Name: vocab.MustName("Financial Institution"), Description: "A bank or financial services organization", Category: false, Parent: parent(RoleCorporation)},
		{ID: RoleDivision, Name: vocab.MustName("Division"), Description: "A division within an organization", Category: false, Parent: parent(RoleOrganizationUnitCategory)},
		{ID: RoleDepartment, Name: vocab.MustName("Department"), Description: "A department within an organization", Category: false, Parent: parent(RoleOrganizationUnitCategory)},
		{ID: RoleSubsidiary, Name: vocab.MustName("Subsidiary"), Description: "A subsidiary organization", Category: false, Parent: parent(RoleOrganizationUnitCategory)},
		{ID: RoleParentOrganization, Name: vocab.MustName("Parent Organization"), Description: "An organization that contains other organization units", Category: false, Parent: parent(RoleOrganizationUnitCategory)},
		{ID: RoleTeam, Name: vocab.MustName("Team"), Description: "A team organized for a specific project or function", Category: false, Parent: parent(RoleInformalCategory)},
		{ID: RoleClub, Name: vocab.MustName("Club"), Description: "A club or social organization", Category: false, Parent: parent(RoleInformalCategory)},
		{ID: RoleGroup, Name: vocab.MustName("Group"), Description: "A generic informal group", Category: false, Parent: parent(RoleInformalCategory)},
		{ID: RoleAgent, Name: vocab.MustName("Agent"), Description: "A party acting on behalf of another", Category: false, Parent: parent(RoleDistributionChannelCategory)},
		{ID: RoleDistributor, Name: vocab.MustName("Distributor"), Description: "A party that distributes products", Category: false, Parent: parent(RoleDistributionChannelCategory)},
		{ID: RoleEmployee, Name: vocab.MustName("Employee"), Description: "A person employed by an organization", Category: false, Parent: parent(RolePersonCategory)},
		{ID: RoleContractor, Name: vocab.MustName("Contractor"), Description: "A person providing contract services", Category: false, Parent: parent(RolePersonCategory)},
		{ID: RoleFamilyMember, Name: vocab.MustName("Family Member"), Description: "A member of a family unit", Category: false, Parent: parent(RolePersonCategory)},
		{ID: RoleContact, Name: vocab.MustName("Contact"), Description: "A contact person for an organization", Category: false, Parent: parent(RolePersonCategory)},
		{ID: RoleSelfEmployed, Name: vocab.MustName("Self Employed"), Description: "A self-employed person", Category: false, Parent: parent(RolePersonCategory)},
		{ID: RoleAuthorizedRepresentative, Name: vocab.MustName("Authorized Representative"), Description: "A person authorized to represent another party", Category: false, Parent: parent(RolePersonCategory)},
		{ID: RoleCustomer, Name: vocab.MustName("Customer"), Description: "A party that purchases goods or services", Category: false, Parent: parent(RolePartyRoot)},
		{ID: RoleProspect, Name: vocab.MustName("Prospect"), Description: "A potential customer", Category: false, Parent: parent(RolePartyRoot)},
		{ID: RoleShareholder, Name: vocab.MustName("Shareholder"), Description: "A party that owns shares in an organization", Category: false, Parent: parent(RolePartyRoot)},
		{ID: RoleBillToCustomer, Name: vocab.MustName("Bill-To Customer"), Description: "The party responsible for payment", Category: false, Parent: parent(RoleCustomer)},
		{ID: RoleShipToCustomer, Name: vocab.MustName("Ship-To Customer"), Description: "The party receiving shipments", Category: false, Parent: parent(RoleCustomer)},
		{ID: RoleEndUserCustomer, Name: vocab.MustName("End-User Customer"), Description: "The end user of a product or service", Category: false, Parent: parent(RoleCustomer)},
		{ID: RoleCollaborator, Name: vocab.MustName("Collaborator"), Description: "A party that refers business for an internal organization", Category: false, Parent: parent(RolePartyRoot)},
		{ID: RoleSalesperson, Name: vocab.MustName("Salesperson"), Description: "A person who sells on behalf of an internal organization", Category: false, Parent: parent(RolePersonCategory)},
	}
}

// WellKnownRoleCatalog construye el catálogo de la semilla.
func WellKnownRoleCatalog() *Catalog {
	c, err := NewRoleCatalog(WellKnownRoleTypes(), RolePersonCategory, RoleOrganizationCategory)
	if err != nil {
		panic(err)
	}
	return c
}

// IsWellKnownRelationshipType dice si un tipo de relación viene con el programa en lugar de
// haberse añadido al catálogo.
func IsWellKnownRelationshipType(id RelationshipTypeID) bool {
	for _, t := range WellKnownRelationshipTypes() {
		if t.ID == id {
			return true
		}
	}
	return false
}

// WellKnownRelationshipTypes devuelve la semilla del catálogo de tipos de relación.
func WellKnownRelationshipTypes() []RelationshipType {
	return []RelationshipType{
		{ID: RelEmployment, Code: "employment", Name: vocab.MustName("Employment"), Description: "Employee works for an internal organization", FromRole: RoleEmployee, ToRole: RoleInternalOrganization},
		{ID: RelCustomer, Code: CodeCustomer, Name: vocab.MustName("Customer Relationship"), Description: "Customer buys from an internal organization", FromRole: RoleCustomer, ToRole: RoleInternalOrganization},
		{ID: RelSupplier, Code: "supplier", Name: vocab.MustName("Supplier Relationship"), Description: "Internal organization buys from a supplier", FromRole: RoleInternalOrganization, ToRole: RoleSupplier},
		{ID: RelPartnership, Code: "partnership", Name: vocab.MustName("Partnership"), Description: "Symmetric partnership between organizations", FromRole: RolePartner, ToRole: RolePartner},
		{ID: RelOrganizationRollup, Code: "organization-rollup", Name: vocab.MustName("Organization Rollup"), Description: "An organization unit belongs to its parent organization",
			FromRole: RoleOrganizationUnitCategory, ToRole: RoleOrganizationCategory, Hierarchical: true},
		{ID: RelFamily, Code: "family", Name: vocab.MustName("Family Relationship"), Description: "Symmetric family relationship between persons", FromRole: RoleFamilyMember, ToRole: RoleFamilyMember},
		{ID: RelOwnership, Code: CodeOwnership, Name: vocab.MustName("Ownership"), Description: "Shareholder owns shares in an internal organization", FromRole: RoleShareholder, ToRole: RoleInternalOrganization},
		{ID: RelAgent, Code: "agent", Name: vocab.MustName("Agent Relationship"), Description: "Agent acts on behalf of a customer", FromRole: RoleAgent, ToRole: RoleCustomer},
		{ID: RelContact, Code: "contact", Name: vocab.MustName("Contact Relationship"), Description: "Contact person for a customer", FromRole: RoleContact, ToRole: RoleCustomer},
		{ID: RelCollaborator, Code: "collaborator", Name: vocab.MustName("Collaborator Relationship"), Description: "Collaborator refers business for an internal organization", FromRole: RoleCollaborator, ToRole: RoleInternalOrganization},
		{ID: RelCollaboratorOrigination, Code: "collaborator-origination", Name: vocab.MustName("Collaborator Origination"), Description: "Salesperson who originated the collaborator", FromRole: RoleSalesperson, ToRole: RoleCollaborator},
		// Los cuatro que tiene la semilla de Trench y que le faltaban a este catálogo (docs/DECISIONES-COMUNES.md, D-62).
		{ID: RelSubsidiary, Code: "subsidiary", Name: vocab.MustName("Subsidiary"), Description: "Internal organization owns a subsidiary", FromRole: RoleInternalOrganization, ToRole: RoleSubsidiary},
		{ID: RelLegalRepresentation, Code: "legal-representation", Name: vocab.MustName("Legal Representation"), Description: "Authorized representative acts for an internal organization",
			FromRole: RoleAuthorizedRepresentative, ToRole: RoleInternalOrganization},
		{ID: RelDepartmentAssignment, Code: "department-assignment", Name: vocab.MustName("Department Assignment"), Description: "Employee is assigned to a department", FromRole: RoleEmployee, ToRole: RoleDepartment},
		{ID: RelCollaboratorContact, Code: "collaborator-contact", Name: vocab.MustName("Collaborator Contact"), Description: "Contact person for a collaborator", FromRole: RoleContact, ToRole: RoleCollaborator},
		{ID: RelDuplicateOf, Code: "duplicate-of", Name: vocab.MustName("Duplicate Of"), Description: "Duplicate record merged into the survivor",
			FromRole: RolePartyRoot, ToRole: RolePartyRoot},
		{ID: RelProspect, Code: CodeProspect, Name: vocab.MustName("Prospect Relationship"), Description: "Prospect may become a customer of an internal organization", FromRole: RoleProspect, ToRole: RoleInternalOrganization},
	}
}
