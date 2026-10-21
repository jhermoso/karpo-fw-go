package domain

import (
	"fmt"
	"strings"
	"time"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// RelationshipKind es el nombre estable del tipo de agregado de Relationship.
const RelationshipKind = "parties.relationship"

// RelationshipType es una entrada del catálogo de tipos de relación: el rol que debe desempeñar
// cada lado ("Employment" relaciona un Employee con una Internal Organization).
type RelationshipType struct {
	ID          RelationshipTypeID
	Name        vocab.Name
	Description string
	FromRole    RoleTypeID
	ToRole      RoleTypeID
	// Los tipos jerárquicos relacionan un hijo (From) con su padre (To): una party tiene un solo
	// padre vigente por tipo y la jerarquía no tiene ciclos (agregaciones de organizaciones).
	Hierarchical bool
	// Code es la clave estable y legible del tipo ("prospect"); opcional y única. Es lo que dice
	// qué detalles lleva una relación del tipo (los subtipos UDM de PARTY RELATIONSHIP): un tipo
	// sin código, o con un código que el dominio no conoce, no lleva ninguno, así que añadir un
	// tipo sin datos propios se queda en una fila del catálogo.
	Code string
}

// NewRelationshipType construye un tipo de relación que alguien añade al catálogo: qué rol
// desempeña cada lado. No lleva datos propios y no es jerárquico (de los que lo son cuelgan reglas
// del programa). Que los roles existan, y que el nombre y el código estén libres, se comprueba
// contra los catálogos (AdmitsRelationshipType).
func NewRelationshipType(id RelationshipTypeID, name, description, code string, from, to RoleTypeID) (RelationshipType, error) {
	var v fw.Validation
	n, err := vocab.NewName(name)
	v.Merge("", err)
	description = strings.Join(strings.Fields(description), " ")
	v.Require(len([]rune(description)) <= 500, "description", "length", "description must have at most 500 characters")
	code = strings.TrimSpace(code)
	v.Require(code == "" || (len(code) <= MaxTypeCode && typeCode.MatchString(code)), "code", "format",
		"code is written in lower case letters, digits and hyphens, starts with a letter and has at most 40 characters")
	v.Require(!id.IsZero(), "id", "required", "id is required")
	v.Require(!from.IsZero(), "fromRole", "required", "the role of the first side is required")
	v.Require(!to.IsZero(), "toRole", "required", "the role of the second side is required")
	if err := v.Err(); err != nil {
		return RelationshipType{}, err
	}
	return RelationshipType{ID: id, Code: code, Name: n, Description: description, FromRole: from, ToRole: to}, nil
}

// AdmitsRelationshipType comprueba que un tipo de relación nuevo encaja en los catálogos: sus
// roles existen y ningún tipo de relación tiene su nombre (sin distinguir mayúsculas) ni su código.
func AdmitsRelationshipType(roles RoleCatalog, types map[RelationshipTypeID]RelationshipType, t RelationshipType) error {
	var v fw.Validation
	for _, side := range []struct {
		field string
		role  RoleTypeID
	}{{"fromRole", t.FromRole}, {"toRole", t.ToRole}} {
		if _, ok := roles.RoleType(side.role); !ok {
			v.Add(side.field, "unknown", "unknown role type "+side.role.String())
		}
	}
	if err := v.Err(); err != nil {
		return err
	}
	if _, dup := types[t.ID]; dup {
		return fw.Violation("parties.relationship_type_exists", "the relationship type "+t.ID.String()+" already exists")
	}
	for _, other := range types {
		if strings.EqualFold(other.Name.String(), t.Name.String()) {
			return fw.Violation("parties.relationship_type_name_taken", "there is already a relationship type called "+other.Name.String())
		}
		if t.Code != "" && other.Code == t.Code {
			return fw.Violation("parties.relationship_type_code_taken", "there is already a relationship type with the code "+t.Code)
		}
	}
	return nil
}

// IndexRelationshipTypes indexa el catálogo por id y rechaza dos tipos con el mismo código.
func IndexRelationshipTypes(types []RelationshipType) (map[RelationshipTypeID]RelationshipType, error) {
	out := make(map[RelationshipTypeID]RelationshipType, len(types))
	codes := map[string]RelationshipTypeID{}
	for _, t := range types {
		if other, dup := codes[t.Code]; dup && t.Code != "" && other != t.ID {
			return nil, fmt.Errorf("%w: relationship types %s and %s share the code %q", fw.ErrValidation, other, t.ID, t.Code)
		}
		codes[t.Code] = t.ID
		out[t.ID] = t
	}
	return out, nil
}

// Relationship une durante un periodo a dos parties que desempeñan los roles que exige su tipo
// (UDM PARTY RELATIONSHIP). Es un agregado propio: una party puede tener miles de relaciones, y
// cada una cambia por su cuenta.
type Relationship struct {
	fw.BaseAggregateRoot[RelationshipID]
	traits.Audited
	relType  RelationshipTypeID
	from, to PartyID
	fromRole RoleTypeID
	toRole   RoleTypeID
	period   vocab.ValidPeriod
	remark   string
	details  RelationshipDetails
	// transferredFrom es la relación a la que ésta continúa en otra party, por una fusión o por
	// deshacerla (D-93); cero si nació por sí misma.
	transferredFrom RelationshipID
}

// RelationshipDetails son los datos que lleva una relación por su tipo (los subtipos UDM de PARTY
// RELATIONSHIP). Como con PersonDetails y OrganizationDetails en Party, el tipo es un dato y los
// detalles de cada tipo son objetos de valor; los de los otros tipos se quedan a cero.
type RelationshipDetails struct {
	Prospect     ProspectDetails     // tipos con código CodeProspect
	Ownership    OwnershipDetails    // tipos con código CodeOwnership
	Collaborator CollaboratorDetails // tipos con código CodeCollaborator
	Customer     CustomerDetails     // tipos con código CodeCustomer
}

// CustomerDetails son los datos de una relación de cliente: el número con el que la organización
// interna conoce al cliente. Cada organización numera a sus propios clientes, así que el número es
// de la pareja y no del cliente (en C# estaba en el rol Customer, uno para todos, y otra vez en el
// perfil comercial de la relación).
type CustomerDetails struct {
	// Number se escribe en mayúsculas; vacío cuando la organización no ha dado ninguno.
	Number string
}

// MaxCustomerNumber es la longitud del número de cliente más largo.
const MaxCustomerNumber = 20

// NormalizeCustomerNumber escribe un número de cliente tal como se guarda y se compara.
func NormalizeCustomerNumber(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }

// CollaboratorDetails son los datos de una relación de colaborador: el código que el colaborador
// da a quienes trae, por el que la organización interna sabe de quién es un negocio. (En C# era
// una columna de una tabla sin modelo, rellenada por una carga masiva.)
type CollaboratorDetails struct {
	// PromotionCode se escribe en mayúsculas; vacío cuando el colaborador no tiene.
	PromotionCode string
}

// MaxPromotionCode es la longitud del código de promoción más largo.
const MaxPromotionCode = 15

// NormalizePromotionCode escribe un código de promoción tal como se guarda y se compara.
func NormalizePromotionCode(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }

// OwnershipDetails son los datos de una relación de propiedad: la participación del accionista.
type OwnershipDetails struct {
	// Share es la participación directa en la organización, en puntos (30 = 30 %); nil si no se conoce.
	Share *vocab.Percentage
}

// ProspectDetails son los datos de una relación de prospect: la prueba gratuita que la
// organización interna concede al prospect.
type ProspectDetails struct {
	// TrialUntil es el momento en que termina la prueba (nil: no se ha concedido). La prueba empieza con la
	// relación; su duración por defecto es política comercial y corresponde a quien la concede.
	TrialUntil *time.Time
}

// Establish crea una relación entre dos parties y comprueba lo que C# dejaba en stubs: las
// parties son distintas, están activas y desempeñan los roles que exige el tipo (o roles por
// debajo de ellos en la jerarquía) cuando empieza la relación. Las parties se leen, nunca se
// modifican: dar a un lado el rol que le falta es cosa del caso de uso, antes de llegar aquí.
func Establish(id RelationshipID, t RelationshipType, from, to *Party, c RoleCatalog, since time.Time, remark string) (*Relationship, error) {
	if from.ID() == to.ID() {
		return nil, fw.Violation("parties.self_relationship", "a party cannot be related to itself")
	}
	for _, s := range []struct {
		p    *Party
		role RoleTypeID
		side string
	}{{from, t.FromRole, "origin"}, {to, t.ToRole, "destination"}} {
		if !s.p.IsActive() {
			return nil, fw.Violation("parties.inactive", "the "+s.side+" party is inactive")
		}
		if !s.p.Meets(c, s.role, since) {
			name := s.role.String()
			if rt, ok := c.RoleType(s.role); ok {
				name = rt.Name.String()
			}
			return nil, fw.Violation("parties.relationship_role_missing",
				"the "+s.side+" party does not play the role "+name+" required by "+t.Name.String())
		}
	}
	period, err := vocab.OpenPeriodFrom(since)
	if err != nil {
		return nil, err
	}
	r, err := ReconstituteRelationship(id, RelationshipState{
		Type: t.ID, From: from.ID(), To: to.ID(), FromRole: t.FromRole, ToRole: t.ToRole, Period: period, Remark: remark,
	})
	if err != nil {
		return nil, err
	}
	r.Raise(RelationshipEstablished{EventMeta: r.NewEventMeta(), Type: t.ID.String(), From: from.ID().String(),
		To: to.ID().String(), FromRole: t.FromRole.String(), ToRole: t.ToRole.String(), Since: period.From()})
	return r, nil
}

// Meets dice si la party es lo que un tipo de relación pide a uno de sus lados en t: desempeña el
// rol (o uno por debajo de él), o el rol es la categoría de toda una clase y la party es de esa
// clase; la raíz pide cualquier party (Duplicate Of). Un tipo que pide «una organización» pide lo
// que la party es, desempeñe los roles que desempeñe (una organización interna o un proveedor
// también pueden ser una persona: docs/DECISIONES-COMUNES.md, D-53).
func (p *Party) Meets(c RoleCatalog, role RoleTypeID, t time.Time) bool {
	return p.PlaysAt(c, role, t) || role == RolePartyRoot ||
		(role == RoleOrganizationCategory && p.kind == KindOrganization) || (role == RolePersonCategory && p.kind == KindPerson)
}

// RelationshipState es el estado persistido de una relación.
type RelationshipState struct {
	Type             RelationshipTypeID
	From, To         PartyID
	FromRole, ToRole RoleTypeID
	Period           vocab.ValidPeriod
	Remark           string
	Details          RelationshipDetails
	TransferredFrom  RelationshipID
	Audit            traits.AuditStamp
}

// ReconstituteRelationship reconstruye una relación a partir del estado persistido, sin eventos.
func ReconstituteRelationship(id RelationshipID, s RelationshipState) (*Relationship, error) {
	base, err := fw.NewBaseAggregateRoot(RelationshipKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(!s.Type.IsZero(), "type", "required", "relationship type is required")
	v.Require(!s.From.IsZero() && !s.To.IsZero(), "parties", "required", "both parties are required")
	v.Require(!s.Period.IsZero(), "since", "required", "start is required")
	v.Require(len([]rune(s.Remark)) <= 1000, "remark", "length", "at most 1000 characters")
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &Relationship{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), relType: s.Type,
		from: s.From, to: s.To, fromRole: s.FromRole, toRole: s.ToRole, period: s.Period, remark: s.Remark, details: s.Details,
		transferredFrom: s.TransferredFrom}, nil
}

// TransferredFrom devuelve la relación a la que ésta continúa en otra party (cero si ninguna).
func (r *Relationship) TransferredFrom() RelationshipID { return r.transferredFrom }

// Continue hace de una relación recién establecida la continuación de prev en otra party: mismo
// tipo, la misma organización en el mismo lado y los mismos datos (el número de cliente). Es como
// una relación de negocio pasa de la ficha fusionada a la superviviente y vuelve al deshacer la
// fusión (docs/DECISIONES-COMUNES.md, D-93); returning dice que es esto último. Terminar prev es
// cosa de quien llama.
func (r *Relationship) Continue(t RelationshipType, prev *Relationship, returning bool) error {
	if t.ID != r.relType || prev.relType != r.relType {
		return fw.Violation("parties.transfer_type_mismatch", "a relationship continues another of its same type")
	}
	var org, ceding, receiving PartyID
	switch {
	case prev.from == r.from && prev.to != r.to:
		org, ceding, receiving = r.from, prev.to, r.to
	case prev.to == r.to && prev.from != r.from:
		org, ceding, receiving = r.to, prev.from, r.from
	default:
		return fw.Violation("parties.transfer_side_mismatch", "a relationship continues another with the same organization and another party")
	}
	r.details = prev.details
	r.transferredFrom = prev.ID()
	r.Raise(RelationshipTransferred{EventMeta: r.NewEventMeta(), Previous: prev.ID().String(), Type: t.ID.String(), TypeCode: t.Code,
		Organization: org.String(), Ceding: ceding.String(), Receiving: receiving.String(), Returning: returning, At: r.Since()})
	return nil
}

// Type devuelve el tipo de relación.
func (r *Relationship) Type() RelationshipTypeID { return r.relType }

// From devuelve la party de origen.
func (r *Relationship) From() PartyID { return r.from }

// To devuelve la party de destino.
func (r *Relationship) To() PartyID { return r.to }

// FromRole devuelve el rol de la party de origen.
func (r *Relationship) FromRole() RoleTypeID { return r.fromRole }

// ToRole devuelve el rol de la party de destino.
func (r *Relationship) ToRole() RoleTypeID { return r.toRole }

// Period devuelve la vigencia de la relación.
func (r *Relationship) Period() vocab.ValidPeriod { return r.period }

// Remark devuelve el comentario libre.
func (r *Relationship) Remark() string { return r.remark }

// Since devuelve cuándo empezó la relación.
func (r *Relationship) Since() time.Time { return r.period.From() }

// Until devuelve cuándo terminó la relación (nil mientras está activa).
func (r *Relationship) Until() *time.Time {
	if t, ok := r.period.To(); ok {
		return &t
	}
	return nil
}

// Involves dice si la party es uno de los lados.
func (r *Relationship) Involves(p PartyID) bool { return r.from == p || r.to == p }

// Terminate termina la relación en un instante (no antes de que empezara). Terminar una relación
// ya terminada antes de su fin adelanta el fin; después no hace nada.
func (r *Relationship) Terminate(at time.Time) error {
	if end, closed := r.period.To(); closed && !at.Before(end) {
		return nil
	}
	p, err := vocab.NewValidPeriod(r.period.From(), &at)
	if err != nil {
		return fw.Violation("parties.relationship_end_before_start", "a relationship cannot end before it starts")
	}
	r.period = p
	r.Raise(RelationshipTerminated{EventMeta: r.NewEventMeta(), At: at.UTC()})
	return nil
}

// Details devuelve los datos que lleva la relación por su tipo.
func (r *Relationship) Details() RelationshipDetails { return r.details }

// TrialUntil devuelve cuándo termina la prueba de una relación de prospect (nil si no hay prueba).
func (r *Relationship) TrialUntil() *time.Time { return r.details.Prospect.TrialUntil }

// InTrialAt dice si la prueba está vigente en t: lo está la relación, y t es anterior al fin de
// la prueba.
func (r *Relationship) InTrialAt(t time.Time) bool {
	until := r.details.Prospect.TrialUntil
	return until != nil && r.period.IsActiveAt(t) && t.Before(*until)
}

// SetTrial concede, amplía, acorta o (con nil) retira la prueba de una relación de prospect. t es
// el tipo de la relación: sólo los tipos con código CodeProspect llevan prueba. La prueba termina
// después de que empiece la relación, y una relación terminada conserva la prueba que tenía.
func (r *Relationship) SetTrial(t RelationshipType, until *time.Time) error {
	if t.ID != r.relType || t.Code != CodeProspect {
		return fw.Violation("parties.not_a_prospect_relationship", "only a prospect relationship has a trial")
	}
	if err := r.requireOpen(); err != nil {
		return err
	}
	if until != nil {
		u := until.UTC()
		if !u.After(r.Since()) {
			var v fw.Validation
			v.Add("trialUntil", "range", "the trial must end after the relationship starts")
			return v.Err()
		}
		until = &u
	}
	if old := r.details.Prospect.TrialUntil; (old == nil && until == nil) || (old != nil && until != nil && old.Equal(*until)) {
		return nil
	}
	r.details.Prospect.TrialUntil = until
	r.Raise(ProspectTrialChanged{EventMeta: r.NewEventMeta(), Prospect: r.from.String(), Organization: r.to.String(), TrialUntil: until})
	return nil
}

// requireOpen se niega a cambiar los detalles de una relación que ha terminado.
func (r *Relationship) requireOpen() error {
	if end, closed := r.period.To(); closed && !end.After(fw.Now()) {
		return fw.Violation("parties.relationship_ended", "the details of an ended relationship cannot change")
	}
	return nil
}

// OwnershipShare devuelve la participación del accionista (nil si no se conoce o no es una propiedad).
func (r *Relationship) OwnershipShare() *vocab.Percentage { return r.details.Ownership.Share }

var maxShare = vocab.DecimalFromInt(100)

// SetOwnershipShare registra, corrige o (con nil) borra la participación directa del accionista en
// una relación de propiedad: por encima de 0 y hasta el 100 %, con dos decimales como mucho. t es
// el tipo de la relación: sólo los tipos con código CodeOwnership llevan participación.
func (r *Relationship) SetOwnershipShare(t RelationshipType, share *vocab.Percentage) error {
	if t.ID != r.relType || t.Code != CodeOwnership {
		return fw.Violation("parties.not_an_ownership_relationship", "only an ownership relationship has a share")
	}
	if err := r.requireOpen(); err != nil {
		return err
	}
	points := ""
	if share != nil {
		p := share.Points()
		if !p.IsPositive() || p.GreaterThan(maxShare) || !p.Equal(p.Round(2)) {
			var v fw.Validation
			v.Add("share", "range", "the share must be above 0 and up to 100, with two decimals at most")
			return v.Err()
		}
		points = p.StringFixed(2)
	}
	if old := r.details.Ownership.Share; (old == nil && share == nil) || (old != nil && share != nil && old.Equal(*share)) {
		return nil
	}
	r.details.Ownership.Share = share
	r.Raise(OwnershipShareChanged{EventMeta: r.NewEventMeta(), Shareholder: r.from.String(), Organization: r.to.String(), Share: points})
	return nil
}

// PromotionCode devuelve el código de promoción del colaborador ("" si no tiene o no es un colaborador).
func (r *Relationship) PromotionCode() string { return r.details.Collaborator.PromotionCode }

// SetPromotionCode da al colaborador su código de promoción, lo cambia o (con "") se lo quita:
// hasta 15 letras, dígitos, guiones y guiones bajos. t es el tipo de la relación: sólo los tipos
// con código CodeCollaborator lo llevan. Que ningún otro colaborador de la organización tenga el
// código lo comprueba quien llama.
func (r *Relationship) SetPromotionCode(t RelationshipType, code string) error {
	if t.ID != r.relType || t.Code != CodeCollaborator {
		return fw.Violation("parties.not_a_collaborator_relationship", "only a collaborator relationship has a promotion code")
	}
	if err := r.requireOpen(); err != nil {
		return err
	}
	code = NormalizePromotionCode(code)
	valid := len(code) <= MaxPromotionCode
	for _, c := range code {
		valid = valid && (c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_')
	}
	if !valid {
		var v fw.Validation
		v.Add("promotionCode", "format", "up to 15 letters, digits, dashes and underscores")
		return v.Err()
	}
	r.details.Collaborator.PromotionCode = code
	return nil
}

// CustomerNumber devuelve el número con el que la organización conoce al cliente ("" si no tiene
// o no es una relación de cliente).
func (r *Relationship) CustomerNumber() string { return r.details.Customer.Number }

// SetCustomerNumber da al cliente su número en la organización, lo cambia o (con "") se lo quita:
// hasta 20 letras, dígitos, guiones, puntos, barras y guiones bajos. t es el tipo de la relación:
// sólo los tipos con código CodeCustomer lo llevan. Que ningún otro cliente de la organización
// tenga el número lo comprueba quien llama.
func (r *Relationship) SetCustomerNumber(t RelationshipType, number string) error {
	if t.ID != r.relType || t.Code != CodeCustomer {
		return fw.Violation("parties.not_a_customer_relationship", "only a customer relationship has a customer number")
	}
	if err := r.requireOpen(); err != nil {
		return err
	}
	number = NormalizeCustomerNumber(number)
	valid := len(number) <= MaxCustomerNumber
	for _, c := range number {
		valid = valid && (c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' || c == '/')
	}
	if !valid {
		var v fw.Validation
		v.Add("customerNumber", "format", "up to 20 letters, digits, dashes, dots, slashes and underscores")
		return v.Err()
	}
	r.details.Customer.Number = number
	return nil
}

// AuditSnapshot implementa traits.Snapshotter.
func (r *Relationship) AuditSnapshot() map[string]any {
	format := func(t *time.Time) string {
		if t == nil {
			return ""
		}
		return t.Format(time.RFC3339)
	}
	return map[string]any{"type": r.relType.String(), "from": r.from.String(), "to": r.to.String(),
		"since": r.Since().Format(time.RFC3339), "until": format(r.Until()), "remark": r.remark,
		"trialUntil": format(r.TrialUntil()), "share": shareText(r.OwnershipShare()), "promotionCode": r.PromotionCode(),
		"customerNumber": r.CustomerNumber(), "transferredFrom": transferredText(r.transferredFrom)}
}

// transferredText escribe la relación de la que viene otra ("" para ninguna).
func transferredText(id RelationshipID) string {
	if id.IsZero() {
		return ""
	}
	return id.String()
}

// shareText escribe una participación con dos decimales ("" para nil).
func shareText(p *vocab.Percentage) string {
	if p == nil {
		return ""
	}
	return p.Points().StringFixed(2)
}
