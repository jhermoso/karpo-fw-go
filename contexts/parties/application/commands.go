// Package application reúne los casos de uso de Parties: órdenes y consultas, sus manejadores
// decorados con la cadena del framework (permisos, validación, idempotencia, reintentos), los DTO,
// la traducción al Published Language y el directorio de parties dentro del proceso.
package application

import (
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Permisos del contexto ({Subdomain}.{Resource}.{Action}, como en los PermissionCodes de C#).
var (
	PermPartyRead   = authz.MustPermission("Parties.Party.Read")
	PermPartyCreate = authz.MustPermission("Parties.Party.Create")
	PermPartyUpdate = authz.MustPermission("Parties.Party.Update")
	// PermPartyUpdateRelated deja al llamante cambiar una party con la que su organización sólo
	// se relaciona: una que no dio de alta y que no es organización interna de nadie. Una acción
	// propia: el administrador de la organización la tiene, un usuario estándar no, y el
	// administrador global puede quitarla (docs/DECISIONES-COMUNES.md, D-66).
	PermPartyUpdateRelated = authz.MustPermission("Parties.Party.UpdateRelated")
	PermRoleAssign         = authz.MustPermission("Parties.PartyRole.Assign")
	// PermPartyMerge marca una party como duplicado de otra (la fusiona en ella) o deshace la
	// marca. Una acción propia, distinta de cambiar una party (docs/DECISIONES-COMUNES.md, D-95):
	// la regla estándar de Security no se la da al usuario estándar.
	PermPartyMerge = authz.MustPermission("Parties.Party.Merge")
	// PermRoleAnnul retira un rol dado por error, y lo borra cuando nada depende de él: una
	// acción propia, que la regla estándar de Security no reparte.
	PermRoleAnnul          = authz.MustPermission("Parties.PartyRole.Annul")
	PermRelationshipRead   = authz.MustPermission("Parties.Relationship.Read")
	PermRelationshipCreate = authz.MustPermission("Parties.Relationship.Create")
	PermRelationshipEnd    = authz.MustPermission("Parties.Relationship.Terminate")
	// PermRelationshipUpdate cambia los datos que una relación lleva por su tipo.
	PermRelationshipUpdate = authz.MustPermission("Parties.Relationship.Update")
	// PermRelationshipSetTrial concede, amplía o retira la prueba gratuita de un prospecto. Es una
	// acción propia, no una actualización, para que la regla estándar del catálogo de Security
	// (leer, crear, actualizar) no se la dé a todo usuario estándar: regalar tiempo es una
	// decisión comercial.
	PermRelationshipSetTrial = authz.MustPermission("Parties.Relationship.SetTrial")
	// Añadir un tipo al catálogo de roles lo cambia para toda la instalación: una acción propia
	// (la regla estándar de Security repartiría un «Create»), y sólo para un administrador
	// global.
	PermRoleTypeDefine         = authz.MustPermission("Parties.RoleType.Define")
	PermRelationshipTypeDefine = authz.MustPermission("Parties.RelationshipType.Define")
)

// Permissions devuelve los permisos que este contexto declara al catálogo de Security.
func Permissions() []authz.Permission {
	return []authz.Permission{PermPartyRead, PermPartyCreate, PermPartyUpdate, PermRoleAssign, PermRelationshipRead,
		PermRelationshipCreate, PermRelationshipEnd, PermRelationshipUpdate, PermRelationshipSetTrial, PermRoleTypeDefine,
		PermRelationshipTypeDefine, PermRoleAnnul, PermPartyUpdateRelated, PermPartyMerge}
}

// RegisterPerson da de alta una persona, si se quiere con roles iniciales (el CreatePartyWithRole de C#).
type RegisterPerson struct {
	GivenName     string          `json:"givenName"`
	FirstSurname  string          `json:"firstSurname"`
	SecondSurname string          `json:"secondSurname,omitempty"`
	Gender        string          `json:"gender,omitempty"`
	BirthDate     string          `json:"birthDate,omitempty"` // YYYY-MM-DD
	MaritalStatus string          `json:"maritalStatus,omitempty"`
	Roles         []string        `json:"roles,omitempty"`
	Affiliation   *NewAffiliation `json:"affiliation,omitempty"` // obligatoria salvo para el administrador global
	RequestID     string          `json:"-"`                     // Idempotency-Key
}

// IdempotencyKey implementa application.IdempotencyKeyed.
func (c RegisterPerson) IdempotencyKey() string { return c.RequestID }

func (c RegisterPerson) details() (domain.PersonDetails, error) {
	var v fw.Validation
	name, err := domain.NewPersonalName(c.GivenName, c.FirstSurname, c.SecondSurname)
	v.Merge("", err)
	g, err := domain.ParseGender(c.Gender)
	v.Merge("", err)
	m, err := domain.ParseMaritalStatus(c.MaritalStatus)
	v.Merge("", err)
	var birth vocab.Date
	if c.BirthDate != "" {
		if birth, err = vocab.ParseDate(c.BirthDate); err != nil {
			v.Add("birthDate", "format", "birth date must be YYYY-MM-DD")
		}
	}
	return domain.PersonDetails{Name: name, Gender: g, BirthDate: birth, MaritalStatus: m}, v.Err()
}

// RegisterOrganization da de alta una organización, si se quiere con roles iniciales.
type RegisterOrganization struct {
	LegalName   string          `json:"legalName"`
	TradeName   string          `json:"tradeName,omitempty"`
	LegalForm   string          `json:"legalForm,omitempty"`
	Roles       []string        `json:"roles,omitempty"`
	Affiliation *NewAffiliation `json:"affiliation,omitempty"`
	RequestID   string          `json:"-"`
}

// SetLegalForm cambia la forma jurídica de una organización.
type SetLegalForm struct {
	ID        domain.PartyID `json:"-"`
	LegalForm string         `json:"legalForm"`
}

// SetShared hace de una party una entrada compartida del catálogo, visible para todas las
// organizaciones (sólo administradores globales).
type SetShared struct {
	ID     domain.PartyID `json:"-"`
	Shared bool           `json:"shared"`
}

// FlagDocumentIssue marca una party cuyo documento de identidad se rechazó: el número tal como
// llegó y por qué. Una importación lo hace con lo que carga; una persona también puede hacerlo.
type FlagDocumentIssue struct {
	ID     domain.PartyID `json:"-"`
	Number string         `json:"number,omitempty"`
	Reason string         `json:"reason"`
}

// ClearDocumentIssue quita la marca de documento de identidad rechazado.
type ClearDocumentIssue struct {
	ID domain.PartyID `json:"-"`
}

// ListInternalOrganizations lista las organizaciones internas del ámbito del llamante.
type ListInternalOrganizations struct{}

// IdempotencyKey implementa application.IdempotencyKeyed.
func (c RegisterOrganization) IdempotencyKey() string { return c.RequestID }

// RenameParty cambia el nombre de una persona (nombre y apellidos) o de una organización
// (razón social y nombre comercial); los campos de la otra clase se ignoran.
type RenameParty struct {
	ID            domain.PartyID `json:"-"`
	GivenName     string         `json:"givenName,omitempty"`
	FirstSurname  string         `json:"firstSurname,omitempty"`
	SecondSurname string         `json:"secondSurname,omitempty"`
	LegalName     string         `json:"legalName,omitempty"`
	TradeName     string         `json:"tradeName,omitempty"`
}

// UpdatePerson sustituye el género, la fecha de nacimiento y el estado civil de una persona
// (vacío: desconocido). El nombre cambia con RenameParty.
type UpdatePerson struct {
	ID            domain.PartyID `json:"-"`
	Gender        string         `json:"gender,omitempty"`
	BirthDate     string         `json:"birthDate,omitempty"` // YYYY-MM-DD
	MaritalStatus string         `json:"maritalStatus,omitempty"`
}

// SetPartyActive activa o desactiva una party (el conmutador de C#).
type SetPartyActive struct {
	ID     domain.PartyID `json:"-"`
	Active bool           `json:"active"`
}

// AssignRole hace que una party desempeñe un rol desde un momento (ahora si From es nil).
type AssignRole struct {
	PartyID  domain.PartyID `json:"-"`
	RoleType string         `json:"roleType"`
	From     *time.Time     `json:"from,omitempty"`
}

// EndRole termina un rol en un momento (ahora si At es nil).
type EndRole struct {
	PartyID domain.PartyID     `json:"-"`
	RoleID  domain.PartyRoleID `json:"-"`
	At      *time.Time         `json:"at,omitempty"`
}

// AnnulRole retira un rol dado a una party por error.
type AnnulRole struct {
	PartyID domain.PartyID     `json:"-"`
	RoleID  domain.PartyRoleID `json:"-"`
}

// SetSelfEmployedDetails fija el perfil de actividad de un rol Self Employed
// (docs/DECISIONES-COMUNES.md, D-85). Sustituye al que hubiera: un campo vacío borra ese dato y
// todos vacíos dejan el perfil vacío. RegisteredOn es una fecha YYYY-MM-DD.
type SetSelfEmployedDetails struct {
	PartyID      domain.PartyID     `json:"-"`
	RoleID       domain.PartyRoleID `json:"-"`
	TradeName    string             `json:"tradeName,omitempty"`
	ActivityCode string             `json:"activityCode,omitempty"`
	RegisteredOn string             `json:"registeredOn,omitempty"`
}

// EstablishRelationship relaciona dos parties (los roles salen del tipo de relación). Los datos
// del tipo, si tiene, pueden venir con ella.
type EstablishRelationship struct {
	Type   string     `json:"type"`
	From   string     `json:"fromParty"`
	To     string     `json:"toParty"`
	Since  *time.Time `json:"since,omitempty"`
	Remark string     `json:"remark,omitempty"`
	RelationshipDetailsInput
}

// RelationshipDetailsInput son los datos que una relación puede llevar por su tipo; cada uno sólo
// lo aceptan los tipos de relación que lo tienen.
type RelationshipDetailsInput struct {
	Prospect  *ProspectInput  `json:"prospect,omitempty"`  // relaciones de prospecto
	Ownership *OwnershipInput `json:"ownership,omitempty"` // relaciones de propiedad
	// continues es la relación de otra party a la que continúa la nueva, con sus datos: la que pasa
	// a la superviviente al fusionar o vuelve al deshacer la fusión (D-93). No llega por HTTP.
	continues *domain.Relationship
	// returning dice que continues vuelve al deshacer la fusión: el traslado se publica marcado
	// como vuelta y los otros contextos no copian perfiles (docs/PARTIES.md §17).
	returning bool
}

// apply pone los datos en una relación de tipo rt.
func (d RelationshipDetailsInput) apply(r *domain.Relationship, rt domain.RelationshipType) error {
	if d.continues != nil {
		if err := r.Continue(rt, d.continues, d.returning); err != nil {
			return err
		}
	}
	if d.Prospect != nil {
		if err := r.SetTrial(rt, d.Prospect.TrialUntil); err != nil {
			return err
		}
	}
	if d.Ownership != nil {
		share, err := parseShare(d.Ownership.Share)
		if err != nil {
			return err
		}
		return r.SetOwnershipShare(rt, share)
	}
	return nil
}

// ProspectInput son los datos de una relación de prospecto.
type ProspectInput struct {
	TrialUntil *time.Time `json:"trialUntil"` // null: sin prueba
}

// OwnershipInput son los datos de una relación de propiedad.
type OwnershipInput struct {
	Share *string `json:"share"` // puntos con dos decimales como mucho ("30"); null: desconocida
}

func parseShare(s *string) (*vocab.Percentage, error) {
	if s == nil {
		return nil, nil
	}
	d, err := vocab.ParseDecimal(*s)
	if err != nil {
		var v fw.Validation
		v.Add("share", "format", "share must be a decimal number")
		return nil, v.Err()
	}
	p := vocab.NewPercentage(d)
	return &p, nil
}

// SetOwnershipShare anota, corrige o (con una participación null) borra la participación del
// accionista en una relación de propiedad.
type SetOwnershipShare struct {
	ID    domain.RelationshipID `json:"-"`
	Share *string               `json:"share"`
}

// SetCustomerNumber da a un cliente su número en la organización, lo cambia o (con un número
// vacío) se lo quita.
type SetCustomerNumber struct {
	ID             domain.RelationshipID `json:"-"`
	CustomerNumber string                `json:"customerNumber"`
}

// SetPromotionCode da a un colaborador su código de promoción, lo cambia o (con un código vacío)
// se lo quita.
type SetPromotionCode struct {
	ID            domain.RelationshipID `json:"-"`
	PromotionCode string                `json:"promotionCode"`
}

// SetProspectTrial concede, amplía, acorta o (con un fin null) retira la prueba de una relación
// de prospecto.
type SetProspectTrial struct {
	ID         domain.RelationshipID `json:"-"`
	TrialUntil *time.Time            `json:"trialUntil"`
}

// TerminateRelationship termina una relación (ahora si At es nil).
type TerminateRelationship struct {
	ID domain.RelationshipID `json:"-"`
	At *time.Time            `json:"at,omitempty"`
}

// GetParty carga una party.
type GetParty struct{ ID domain.PartyID }

// SearchParties busca parties; cada criterio pasa a formar parte de una única especificación que
// ejecuta el almacén.
type SearchParties struct {
	Text string
	Kind string
	Role string // id del tipo de rol: parties que lo desempeñan (o un rol por debajo) ahora
	// IncludeEndedRoles hace que Role case también con los roles que no están en vigor: los
	// terminados y los que no han empezado (docs/DECISIONES-COMUNES.md, D-48).
	IncludeEndedRoles bool
	Document          string // número de identificación
	Organization      string // id de la organización interna: parties afiliadas a ella ahora
	Facility          string // id de la instalación: parties con un rol en ella ahora
	Classification    string // id del tipo de clasificación: parties clasificadas así ahora
	ActiveOnly        bool
	DocumentIssue     bool // sólo las parties marcadas con un documento de identidad rechazado
	// IncludeMergedParties trae también las parties marcadas como duplicado de otra, que una
	// búsqueda deja fuera por omisión (docs/DECISIONES-COMUNES.md, D-64).
	IncludeMergedParties bool
	Page, Size           int
}

// PartyRelationships lista las relaciones de una party.
type PartyRelationships struct {
	PartyID    domain.PartyID
	ActiveOnly bool
}

// ListRoleTypes lista el catálogo de tipos de rol.
type ListRoleTypes struct{}

// DefineRelationshipType añade un tipo de relación al catálogo: qué rol desempeña cada lado. No
// lleva datos propios.
type DefineRelationshipType struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Code        string `json:"code,omitempty"` // opcional; único
	FromRole    string `json:"fromRole"`       // id del tipo de rol
	ToRole      string `json:"toRole"`         // id del tipo de rol
}

// DefineRoleType añade un tipo de rol al catálogo: un rol que se puede asignar y no lleva datos
// propios, colocado en la jerarquía bajo su padre.
type DefineRoleType struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Code        string `json:"code,omitempty"` // opcional; único
	Parent      string `json:"parent"`         // id del tipo de rol
}

// ListRelationshipTypes lista el catálogo de tipos de relación.
type ListRelationshipTypes struct{}

func nowOr(t *time.Time) time.Time {
	if t == nil {
		return fw.Now()
	}
	return t.UTC()
}
