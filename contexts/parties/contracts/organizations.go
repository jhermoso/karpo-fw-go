package contracts

import (
	"context"
	"time"
)

// PartyAffiliatedV1 se publica cuando una party queda afiliada a una organización interna (una
// relación activa con ella): la pertenencia por la que los demás contextos acotan sus datos.
type PartyAffiliatedV1 struct {
	PartyID        string    `json:"partyId"`
	Organization   string    `json:"organization"`
	RelationshipID string    `json:"relationshipId"`
	From           time.Time `json:"from"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (PartyAffiliatedV1) IntegrationEventType() string { return "parties.party-affiliated.v1" }

// PartyAffiliationEndedV1 se publica cuando termina una afiliación.
type PartyAffiliationEndedV1 struct {
	PartyID        string    `json:"partyId"`
	Organization   string    `json:"organization"`
	RelationshipID string    `json:"relationshipId"`
	At             time.Time `json:"at"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (PartyAffiliationEndedV1) IntegrationEventType() string {
	return "parties.party-affiliation-ended.v1"
}

// Membership responde a qué organizaciones internas pertenece cada party (el
// IPartyOrganizationMembership de C#): cada id pedido es una clave, con una lista vacía cuando la
// party no pertenece a ninguna.
type Membership interface {
	InternalOrganizations(ctx context.Context, partyIDs []string) (map[string][]string, error)
}

// OrganizationHierarchy recorre la jerarquía de organizaciones (el IOrganizationHierarchy de C#).
type OrganizationHierarchy interface {
	// Descendants devuelve las organizaciones y todas las unidades que cuelgan de ellas.
	Descendants(ctx context.Context, organizationIDs []string) ([]string, error)
	// InternalOrganizationOf devuelve, para cada unidad, el antecesor más cercano (o ella misma)
	// que es una organización interna; las unidades sin ninguno no aparecen.
	InternalOrganizationOf(ctx context.Context, unitIDs []string) (map[string]PartyRef, error)
}

// InternalOrganizationCatalog lista las organizaciones internas (el IInternalOrganizationCatalog
// de C#).
type InternalOrganizationCatalog interface {
	All(ctx context.Context) ([]PartyRef, error)
}

// CompanyTimeZones dice la zona horaria que una empresa interna guarda en su perfil: un código
// IANA, o "" si no guarda ninguna (entonces vale la zona principal del país de su documento
// fiscal, y si no, UTC: docs/DECISIONES-COMUNES.md, D-133). Sirve a otros contextos, sin los
// permisos del usuario.
type CompanyTimeZones interface {
	TimeZone(ctx context.Context, organization string) (string, error)
}

// PartyFacilityRoleAssignedV1 se publica cuando una party empieza a desempeñar un rol en una
// instalación (RRHH: el centro de trabajo de un empleado).
type PartyFacilityRoleAssignedV1 struct {
	PartyID  string    `json:"partyId"`
	RoleID   string    `json:"roleId"`
	Facility string    `json:"facility"`
	RoleType string    `json:"roleType"`
	From     time.Time `json:"from"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (PartyFacilityRoleAssignedV1) IntegrationEventType() string {
	return "parties.party-facility-role-assigned.v1"
}

// PartyFacilityRoleEndedV1 se publica cuando termina un rol en una instalación.
type PartyFacilityRoleEndedV1 struct {
	PartyID  string    `json:"partyId"`
	RoleID   string    `json:"roleId"`
	Facility string    `json:"facility"`
	At       time.Time `json:"at"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (PartyFacilityRoleEndedV1) IntegrationEventType() string {
	return "parties.party-facility-role-ended.v1"
}

// TaxIdentity es la identificación fiscal de una party: su número fiscal (NIF, o el número de
// identidad nacional o de extranjero de una persona) y la provincia de su domicilio fiscal (las
// dos primeras cifras del código postal español), tal como los necesitan los modelos tributarios.
type TaxIdentity struct {
	PartyID      string `json:"partyId"`
	Name         string `json:"name"`
	Country      string `json:"country,omitempty"`
	Number       string `json:"number,omitempty"`
	DocumentType string `json:"documentType,omitempty"` // TXID | NIDN | ARNU
	Province     string `json:"province,omitempty"`
	// Blocked señala una party que no interviene más que en consultas: el duplicado de otra
	// (docs/DECISIONES-COMUNES.md, D-64 y D-70). No se le factura ni se le vende.
	Blocked bool `json:"blocked,omitempty"`
	// Address es el domicilio fiscal: el contacto postal de todos con uso de facturación, si no el
	// de uso por defecto, si no cualquiera. Nil si la party no tiene ninguno.
	Address *FiscalAddress `json:"address,omitempty"`
}

// FiscalAddress es un domicilio tal como va en una factura.
type FiscalAddress struct {
	Street     string `json:"street"` // tipo de vía, vía, número y resto, en una línea
	PostalCode string `json:"postalCode,omitempty"`
	Locality   string `json:"locality,omitempty"`
	Region     string `json:"region,omitempty"`
	Country    string `json:"country,omitempty"`
}

// TaxIdentities resuelve identidades fiscales por lotes (como mucho MaxDirectoryBatch ids). Las
// parties que no existen no aparecen; una party sin documento fiscal tiene Number vacío.
type TaxIdentities interface {
	TaxIdentities(ctx context.Context, partyIDs []string) (map[string]TaxIdentity, error)
}

// MergedParties dice qué parties están ahora fusionadas en otra (marcadas como «Duplicate Of»),
// para que los listados de los demás contextos dejen fuera sus registros salvo que se pidan con
// includeMergedParties (docs/DECISIONES-COMUNES.md, D-100). Son pocas: se piden todas de una vez,
// sin los permisos del usuario (D-103), y cada listado añade «party NOT IN (…)» a su consulta.
type MergedParties interface {
	MergedParties(ctx context.Context) ([]string, error)
}
