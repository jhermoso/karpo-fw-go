// Code generated from the C# WellKnownCatalog (RoleTypes, RelationshipTypes). The GUIDs are
// the same as in the C# catalog, so data migrated from Karpo keeps its references. Differences
// with the C# catalog (see docs/PARTIES.md):
//   - order roles (…0001-0000000000C0..C8) are not party roles: they belong to Orders;
//   - Salesperson and Collaborator, used by relationship types but absent from the C#
//     hierarchy, hang from Person and from the root;
//   - DepartmentAssignment (…0002-000000000012) is declared in C# without a catalog row.
//   - Organization Rollup relates any organization unit to any organization and is hierarchical
//     (one parent, no cycles). The C# row said Department -> Division, yet the C#
//     IOrganizationHierarchy walked rollups down from the legal organizations.

package domain

import "github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"

// Well-known role types.
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
	RoleOtherOrganizationUnit       = MustRoleTypeID("10000000-0000-0000-0001-000000000037")
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

// Well-known relationship types.
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
	RelCollaborator            = MustRelationshipTypeID("10000000-0000-0000-0002-000000000014")
	RelCollaboratorOrigination = MustRelationshipTypeID("10000000-0000-0000-0002-000000000015")
)

func parent(id RoleTypeID) *RoleTypeID { return &id }

// WellKnownRoleTypes returns the seed of the role type catalog.
func WellKnownRoleTypes() []RoleType {
	return []RoleType{
		{ID: RolePartyRoot, Name: vocab.MustName("Party"), Description: "Root of the role type hierarchy", Category: true, Parent: nil},
		{ID: RolePersonCategory, Name: vocab.MustName("Person"), Description: "Roles exclusive to persons", Category: true, Parent: parent(RolePartyRoot)},
		{ID: RoleOrganizationCategory, Name: vocab.MustName("Organization"), Description: "Roles exclusive to organizations", Category: true, Parent: parent(RolePartyRoot)},
		{ID: RoleLegalCategory, Name: vocab.MustName("Legal"), Description: "Legal organization types", Category: true, Parent: parent(RoleOrganizationCategory)},
		{ID: RoleInformalCategory, Name: vocab.MustName("Informal"), Description: "Informal organization types (groups without legal status)", Category: true, Parent: parent(RoleOrganizationCategory)},
		{ID: RoleOrganizationUnitCategory, Name: vocab.MustName("Organization Unit"), Description: "Organization structural units", Category: true, Parent: parent(RoleOrganizationCategory)},
		{ID: RoleDistributionChannelCategory, Name: vocab.MustName("Distribution Channel"), Description: "Distribution channel types", Category: true, Parent: parent(RoleLegalCategory)},
		{ID: RoleInternalOrganization, Name: vocab.MustName("Internal Organization"), Description: "An organization that is part of the enterprise", Category: false, Parent: parent(RoleLegalCategory)},
		{ID: RoleSupplier, Name: vocab.MustName("Supplier"), Description: "A party that provides goods or services", Category: false, Parent: parent(RoleLegalCategory)},
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
		{ID: RoleOtherOrganizationUnit, Name: vocab.MustName("Other Organization Unit"), Description: "An organization unit that does not fit standard categories", Category: false, Parent: parent(RoleOrganizationUnitCategory)},
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

// WellKnownRoleCatalog builds the catalog of the seed.
func WellKnownRoleCatalog() *Catalog {
	c, err := NewRoleCatalog(WellKnownRoleTypes(), RolePersonCategory, RoleOrganizationCategory)
	if err != nil {
		panic(err)
	}
	return c
}

// WellKnownRelationshipTypes returns the seed of the relationship type catalog.
func WellKnownRelationshipTypes() []RelationshipType {
	return []RelationshipType{
		{ID: RelEmployment, Name: vocab.MustName("Employment"), Description: "Employee works for an internal organization", FromRole: RoleEmployee, ToRole: RoleInternalOrganization},
		{ID: RelCustomer, Name: vocab.MustName("Customer Relationship"), Description: "Customer buys from an internal organization", FromRole: RoleCustomer, ToRole: RoleInternalOrganization},
		{ID: RelSupplier, Name: vocab.MustName("Supplier Relationship"), Description: "Internal organization buys from a supplier", FromRole: RoleInternalOrganization, ToRole: RoleSupplier},
		{ID: RelPartnership, Name: vocab.MustName("Partnership"), Description: "Symmetric partnership between organizations", FromRole: RolePartner, ToRole: RolePartner},
		{ID: RelOrganizationRollup, Name: vocab.MustName("Organization Rollup"), Description: "An organization unit belongs to its parent organization",
			FromRole: RoleOrganizationUnitCategory, ToRole: RoleOrganizationCategory, Hierarchical: true},
		{ID: RelFamily, Name: vocab.MustName("Family Relationship"), Description: "Symmetric family relationship between persons", FromRole: RoleFamilyMember, ToRole: RoleFamilyMember},
		{ID: RelOwnership, Name: vocab.MustName("Ownership"), Description: "Shareholder owns shares in an internal organization", FromRole: RoleShareholder, ToRole: RoleInternalOrganization},
		{ID: RelAgent, Name: vocab.MustName("Agent Relationship"), Description: "Agent acts on behalf of a customer", FromRole: RoleAgent, ToRole: RoleCustomer},
		{ID: RelContact, Name: vocab.MustName("Contact Relationship"), Description: "Contact person for a customer", FromRole: RoleContact, ToRole: RoleCustomer},
		{ID: RelCollaborator, Name: vocab.MustName("Collaborator Relationship"), Description: "Collaborator refers business for an internal organization", FromRole: RoleCollaborator, ToRole: RoleInternalOrganization},
		{ID: RelCollaboratorOrigination, Name: vocab.MustName("Collaborator Origination"), Description: "Salesperson who originated the collaborator", FromRole: RoleSalesperson, ToRole: RoleCollaborator},
	}
}
