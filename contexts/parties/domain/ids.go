// Package domain es el modelo del contexto delimitado Parties: quién toma parte en el negocio
// (personas y organizaciones), los roles que desempeñan y las relaciones entre ellos
// (UDM Vol. 1, cap. 2). Sólo se escribe contra los paquetes de dominio puros del framework.
package domain

import fw "github.com/jhermoso/karpo-fw-go/pkg/domain"

// PartyID identifica una party. El valor es el mismo GUID que usa IdParty en C#.
type PartyID struct{ fw.UUID }

// NewPartyID devuelve una identidad nueva.
func NewPartyID() PartyID { return PartyID{fw.NewUUID()} }

// ParsePartyID interpreta una identidad en texto.
func ParsePartyID(s string) (PartyID, error) { u, err := fw.ParseUUID(s); return PartyID{u}, err }

// PartyRoleID identifica un rol que desempeña una party (entidad hija del agregado Party).
type PartyRoleID struct{ fw.UUID }

// NewPartyRoleID devuelve una identidad nueva.
func NewPartyRoleID() PartyRoleID { return PartyRoleID{fw.NewUUID()} }

// ParsePartyRoleID interpreta una identidad en texto.
func ParsePartyRoleID(s string) (PartyRoleID, error) {
	u, err := fw.ParseUUID(s)
	return PartyRoleID{u}, err
}

// RoleTypeID identifica un tipo de rol del catálogo.
type RoleTypeID struct{ fw.UUID }

// ParseRoleTypeID interpreta una identidad en texto.
func ParseRoleTypeID(s string) (RoleTypeID, error) {
	u, err := fw.ParseUUID(s)
	return RoleTypeID{u}, err
}

// MustRoleTypeID interpreta una identidad conocida (entra en pánico con un literal mal formado).
func MustRoleTypeID(s string) RoleTypeID { return RoleTypeID{fw.MustParseUUID(s)} }

// RelationshipTypeID identifica un tipo de relación del catálogo.
type RelationshipTypeID struct{ fw.UUID }

// ParseRelationshipTypeID interpreta una identidad en texto.
func ParseRelationshipTypeID(s string) (RelationshipTypeID, error) {
	u, err := fw.ParseUUID(s)
	return RelationshipTypeID{u}, err
}

// MustRelationshipTypeID interpreta una identidad conocida (entra en pánico con un literal mal formado).
func MustRelationshipTypeID(s string) RelationshipTypeID {
	return RelationshipTypeID{fw.MustParseUUID(s)}
}

// RelationshipID identifica una relación entre dos parties.
type RelationshipID struct{ fw.UUID }

// NewRelationshipID devuelve una identidad nueva.
func NewRelationshipID() RelationshipID { return RelationshipID{fw.NewUUID()} }

// ParseRelationshipID interpreta una identidad en texto.
func ParseRelationshipID(s string) (RelationshipID, error) {
	u, err := fw.ParseUUID(s)
	return RelationshipID{u}, err
}
