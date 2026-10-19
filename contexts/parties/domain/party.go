package domain

import (
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// PartyKind es el nombre estable del tipo de agregado de Party.
const PartyKind = "parties.party"

// Kind distingue a las personas de las organizaciones. En C# eran subclases de Party mapeadas TPT
// (Person, Organization, LegalOrganization, Corporation); aquí la clase es un dato y los detalles
// de cada clase son objetos de valor, así que una party nunca cambia de clase para cambiar sus datos.
type Kind string

// Clases de party.
const (
	KindPerson       Kind = "person"
	KindOrganization Kind = "organization"
)

// PersonDetails son los datos de una persona.
type PersonDetails struct {
	Name          PersonalName
	Gender        Gender
	BirthDate     vocab.Date // cero si no se conoce
	MaritalStatus MaritalStatus
}

func (d PersonDetails) validate() error {
	var v fw.Validation
	v.Require(!d.Name.IsZero(), "name", "required", "a person needs a name")
	v.Require(d.BirthDate.IsZero() || !d.BirthDate.After(vocab.DateOf(fw.Now())), "birthDate", "future", "birth date cannot be in the future")
	return v.Err()
}

// OrganizationDetails son los datos de una organización.
type OrganizationDetails struct {
	Name      OrganizationName
	LegalForm LegalForm
}

// PartyRole es un rol que desempeña una party durante un periodo (entidad hija de Party).
type PartyRole struct {
	ID       PartyRoleID
	RoleType RoleTypeID
	Period   vocab.ValidPeriod
	// Annulled marca un rol dado por error que no se pudo quitar porque ya se había dicho algo
	// de la party en ese rol. Terminó cuando se anuló.
	Annulled bool
	// Details son los datos que lleva el rol por su tipo (el perfil del rol); a cero en los demás.
	Details RoleDetails
}

// IsActiveAt dice si el rol se desempeña en t.
func (r PartyRole) IsActiveAt(t time.Time) bool { return r.Period.IsActiveAt(t) }

// Party es alguien que toma parte en el negocio: una persona o una organización, con los roles que
// desempeña. Los roles son entidades hijas: sus invariantes (clase compatible, sin periodos
// solapados del mismo rol) se hacen cumplir aquí, cosa que C# se saltaba al persistir PartyRole
// directamente.
type Party struct {
	fw.BaseAggregateRoot[PartyID]
	traits.Activation
	traits.Audited
	traits.TestFlag
	kind         Kind
	person       PersonDetails
	organization OrganizationDetails
	roles        []PartyRole

	identifications []Identification
	contacts        []Contact
	classifications []Classification
	affiliations    []Affiliation
	facilityRoles   []FacilityRole
	shared          bool
	documentIssue   DocumentIssue
	duplicate       bool
}

// DocumentIssue marca una party cuyo documento de identidad se rechazó: el número tal como llegó y
// por qué no se aceptó. Así una party cargada por una importación con un documento erróneo sigue a
// la vista hasta que alguien la corrija (docs/PARTIES-MODELO-COMUN.md, D-5 y D-9). El valor cero
// es que no hay incidencia.
type DocumentIssue struct {
	Number string // tal como llegó, para quien lo corrija; no identifica a nadie
	Reason string
}

// IsZero dice si no hay incidencia.
func (d DocumentIssue) IsZero() bool { return d == DocumentIssue{} }

// Límites de una incidencia de documento.
const (
	MaxDocumentIssueNumber = 60
	MaxDocumentIssueReason = 200
)

// RegisterPerson crea una persona.
func RegisterPerson(id PartyID, d PersonDetails) (*Party, error) {
	if err := d.validate(); err != nil {
		return nil, err
	}
	p, err := newParty(id, KindPerson)
	if err != nil {
		return nil, err
	}
	p.person = d
	p.Raise(PartyRegistered{EventMeta: p.NewEventMeta(), Kind: string(KindPerson), Name: p.Name()})
	return p, nil
}

// RegisterOrganization crea una organización.
func RegisterOrganization(id PartyID, d OrganizationDetails) (*Party, error) {
	if d.Name.Legal() == "" {
		var v fw.Validation
		v.Add("name", "required", "an organization needs a legal name")
		return nil, v.Err()
	}
	p, err := newParty(id, KindOrganization)
	if err != nil {
		return nil, err
	}
	p.organization = d
	p.Raise(PartyRegistered{EventMeta: p.NewEventMeta(), Kind: string(KindOrganization), Name: p.Name()})
	return p, nil
}

func newParty(id PartyID, k Kind) (*Party, error) {
	base, err := fw.NewBaseAggregateRoot(PartyKind, id)
	if err != nil {
		return nil, err
	}
	return &Party{BaseAggregateRoot: base, kind: k}, nil
}

// PartyState es el estado persistido de una party (lo usa Reconstitute).
type PartyState struct {
	Kind         Kind
	Person       PersonDetails
	Organization OrganizationDetails
	Roles        []PartyRole
	Identities   []Identification
	Contacts     []Contact
	Classes      []Classification
	Affiliations []Affiliation
	Facilities   []FacilityRole
	Shared       bool
	Issue        DocumentIssue
	Duplicate    bool
	Active       bool
	Test         bool
	Audit        traits.AuditStamp
}

// Reconstitute reconstruye una party a partir del estado persistido, sin eventos.
func Reconstitute(id PartyID, s PartyState) (*Party, error) {
	p, err := newParty(id, s.Kind)
	if err != nil {
		return nil, err
	}
	switch s.Kind {
	case KindPerson:
		p.person = s.Person
	case KindOrganization:
		p.organization = s.Organization
	default:
		var v fw.Validation
		v.Add("kind", "enum", "kind must be person or organization")
		return nil, v.Err()
	}
	p.roles = slices.Clone(s.Roles)
	p.identifications = slices.Clone(s.Identities)
	p.contacts = slices.Clone(s.Contacts)
	p.classifications = slices.Clone(s.Classes)
	p.affiliations = slices.Clone(s.Affiliations)
	p.facilityRoles = slices.Clone(s.Facilities)
	p.shared = s.Shared
	p.documentIssue = s.Issue
	p.duplicate = s.Duplicate
	p.Activation = traits.RestoredActivation(s.Active)
	p.Audited = traits.RestoredAudit(s.Audit)
	p.TestFlag = traits.RestoredTestFlag(s.Test)
	return p, nil
}

// IsDuplicate dice si la party es un duplicado de otra: se sigue encontrando y leyendo, pero no
// toma parte en nada más (docs/DECISIONES-COMUNES.md, D-64). La relación Duplicate Of dice de qué
// party; esta marca es su proyección, para que las búsquedas puedan dejarla fuera.
func (p *Party) IsDuplicate() bool { return p.duplicate }

// MarkDuplicate marca la party como duplicado. Una party es duplicado de una sola party.
func (p *Party) MarkDuplicate() error {
	if p.duplicate {
		return fw.Violation("parties.already_duplicate", "the party is already the duplicate of another")
	}
	p.duplicate = true
	p.Raise(PartyMarkedDuplicate{EventMeta: p.NewEventMeta()})
	return nil
}

// ClearDuplicate quita la marca: la relación Duplicate Of ha terminado.
func (p *Party) ClearDuplicate() {
	if p.duplicate {
		p.duplicate = false
		p.Raise(PartyDuplicateCleared{EventMeta: p.NewEventMeta()})
	}
}

// DocumentIssue devuelve la marca de un documento de identidad rechazado (cero si no la hay).
func (p *Party) DocumentIssue() DocumentIssue { return p.documentIssue }

// HasDocumentIssue dice si la party está marcada con un documento de identidad rechazado.
func (p *Party) HasDocumentIssue() bool { return !p.documentIssue.IsZero() }

// FlagDocumentIssue marca la party: su documento de identidad se rechazó, por ese motivo. Una
// marca nueva sustituye a la que hubiera.
func (p *Party) FlagDocumentIssue(number, reason string) error {
	number, reason = strings.Join(strings.Fields(number), " "), strings.Join(strings.Fields(reason), " ")
	var v fw.Validation
	v.Require(reason != "", "reason", "required", "say why the document was refused")
	v.Require(utf8.RuneCountInString(number) <= MaxDocumentIssueNumber, "number", "length", "number must have at most 60 characters")
	v.Require(utf8.RuneCountInString(reason) <= MaxDocumentIssueReason, "reason", "length", "reason must have at most 200 characters")
	if err := v.Err(); err != nil {
		return err
	}
	p.documentIssue = DocumentIssue{Number: number, Reason: reason}
	return nil
}

// ClearDocumentIssue quita la marca: alguien la ha mirado.
func (p *Party) ClearDocumentIssue() { p.documentIssue = DocumentIssue{} }

// Kind devuelve la clase de party.
func (p *Party) Kind() Kind { return p.kind }

// Person devuelve los datos de persona (cero en una organización).
func (p *Party) Person() PersonDetails { return p.person }

// Organization devuelve los datos de organización (cero en una persona).
func (p *Party) Organization() OrganizationDetails { return p.organization }

// Name devuelve el nombre visible.
func (p *Party) Name() string {
	if p.kind == KindPerson {
		return p.person.Name.String()
	}
	return p.organization.Name.String()
}

// Roles devuelve una copia de todos los roles, actuales o pasados.
func (p *Party) Roles() []PartyRole { return slices.Clone(p.roles) }

// RoleTypes devuelve los tipos de los roles (para especificaciones sobre la colección de roles).
func (p *Party) RoleTypes() []RoleTypeID {
	out := make([]RoleTypeID, len(p.roles))
	for i, r := range p.roles {
		out[i] = r.RoleType
	}
	return out
}

func (p *Party) requireActive(action string) error {
	if !p.IsActive() {
		return fw.Violation("parties.inactive", "an inactive party cannot "+action)
	}
	return nil
}

// RenamePerson cambia el nombre de una persona.
func (p *Party) RenamePerson(n PersonalName) error {
	if p.kind != KindPerson {
		return fw.Violation("parties.kind_mismatch", "only a person has a personal name")
	}
	if n.IsZero() {
		var v fw.Validation
		v.Add("name", "required", "a person needs a name")
		return v.Err()
	}
	return p.rename(func() { p.person.Name = n }, n == p.person.Name)
}

// RenameOrganization cambia el nombre de una organización.
func (p *Party) RenameOrganization(n OrganizationName) error {
	if p.kind != KindOrganization {
		return fw.Violation("parties.kind_mismatch", "only an organization has an organization name")
	}
	if n.Legal() == "" {
		var v fw.Validation
		v.Add("name", "required", "an organization needs a legal name")
		return v.Err()
	}
	return p.rename(func() { p.organization.Name = n }, n == p.organization.Name)
}

func (p *Party) rename(apply func(), same bool) error {
	if err := p.requireActive("be renamed"); err != nil {
		return err
	}
	if same {
		return nil
	}
	old := p.Name()
	apply()
	if p.Name() != old {
		p.Raise(PartyRenamed{EventMeta: p.NewEventMeta(), From: old, To: p.Name()})
	}
	return nil
}

// UpdatePersonDetails cambia el género, la fecha de nacimiento y el estado civil de una persona.
func (p *Party) UpdatePersonDetails(g Gender, birth vocab.Date, m MaritalStatus) error {
	if p.kind != KindPerson {
		return fw.Violation("parties.kind_mismatch", "only a person has personal details")
	}
	if err := p.requireActive("change its personal details"); err != nil {
		return err
	}
	d := p.person
	d.Gender, d.BirthDate, d.MaritalStatus = g, birth, m
	if err := d.validate(); err != nil {
		return err
	}
	p.person = d
	return nil
}

// Activate reactiva la party.
func (p *Party) Activate() {
	if p.Activation.Activate() {
		p.Raise(PartyActivated{EventMeta: p.NewEventMeta()})
	}
}

// Deactivate desactiva la party. Sus roles conservan sus periodos: desactivar oculta la party, no
// reescribe su historia.
func (p *Party) Deactivate() {
	if p.Activation.Deactivate() {
		p.Raise(PartyDeactivated{EventMeta: p.NewEventMeta()})
	}
}

// AssignRole hace que la party desempeñe un rol a partir de un instante. Invariantes: el tipo de
// rol existe, no es una categoría y encaja con la clase de party; la party está activa; y no
// desempeña ya el mismo rol en un periodo solapado.
func (p *Party) AssignRole(c RoleCatalog, roleType RoleTypeID, from time.Time) (PartyRoleID, error) {
	if err := p.requireActive("be given roles"); err != nil {
		return PartyRoleID{}, err
	}
	if err := CanPlay(c, p.kind, roleType); err != nil {
		return PartyRoleID{}, err
	}
	period, err := vocab.OpenPeriodFrom(from)
	if err != nil {
		return PartyRoleID{}, err
	}
	for _, r := range p.roles {
		if !r.Annulled && r.RoleType == roleType && r.Period.Overlaps(period) {
			return PartyRoleID{}, fw.Violation("parties.role_overlap", "the party already plays this role in that period")
		}
	}
	role := PartyRole{ID: NewPartyRoleID(), RoleType: roleType, Period: period}
	p.roles = append(slices.Clone(p.roles), role) // nunca se comparte el array subyacente con una copia guardada
	p.Raise(PartyRoleAssigned{EventMeta: p.NewEventMeta(), RoleID: role.ID.String(), RoleType: roleType.String(), From: period.From()})
	return role.ID, nil
}

// AnnulRole retira un rol dado por error (docs/DECISIONES-COMUNES.md, D-8). Si nada depende de
// él, el rol se quita como si nunca se hubiera dado; si algo depende (lo dice quien llama: es
// quien puede mirar las relaciones), termina en ese instante y se queda, marcado como anulado.
// Dice si el rol se quitó.
func (p *Party) AnnulRole(id PartyRoleID, at time.Time, depended bool) (removed bool, err error) {
	i := slices.IndexFunc(p.roles, func(r PartyRole) bool { return r.ID == id })
	if i < 0 {
		return false, fw.NotFound("parties.party_role", id)
	}
	r := p.roles[i]
	if r.Annulled {
		return false, nil
	}
	p.roles = slices.Clone(p.roles)
	if at.Before(r.Period.From()) {
		at = r.Period.From()
	}
	if end, closed := r.Period.To(); closed && !at.Before(end) {
		at = end // ya había terminado: es cuando dejó de desempeñarse
	}
	if !depended {
		p.roles = slices.Delete(p.roles, i, i+1)
	} else {
		period, err := vocab.NewValidPeriod(r.Period.From(), &at)
		if err != nil {
			return false, err
		}
		p.roles[i].Period = period
		p.roles[i].Annulled = true
	}
	p.Raise(PartyRoleAnnulled{EventMeta: p.NewEventMeta(), RoleID: id.String(), RoleType: r.RoleType.String(), At: at.UTC(), Removed: !depended})
	return !depended, nil
}

// EndRole deja de desempeñar un rol en un instante (no antes de que empezara).
func (p *Party) EndRole(id PartyRoleID, at time.Time) error {
	i := slices.IndexFunc(p.roles, func(r PartyRole) bool { return r.ID == id })
	if i < 0 {
		return fw.NotFound("parties.party_role", id)
	}
	r := p.roles[i]
	if end, closed := r.Period.To(); closed && !at.Before(end) {
		return nil // ya terminó en at o antes
	}
	period, err := vocab.NewValidPeriod(r.Period.From(), &at)
	if err != nil {
		return fw.Violation("parties.role_end_before_start", "a role cannot end before it starts")
	}
	p.roles = slices.Clone(p.roles)
	p.roles[i].Period = period
	p.Raise(PartyRoleEnded{EventMeta: p.NewEventMeta(), RoleID: id.String(), RoleType: r.RoleType.String(), At: at.UTC()})
	return nil
}

// PlaysAt dice si la party desempeña roleType, o un rol por debajo de él en la jerarquía, en t.
func (p *Party) PlaysAt(c RoleCatalog, roleType RoleTypeID, t time.Time) bool {
	for _, r := range p.roles {
		if !r.Annulled && r.IsActiveAt(t) && c.IsA(r.RoleType, roleType) { // un rol anulado no cuenta para nada, en ningún momento
			return true
		}
	}
	return false
}

// AuditSnapshot implementa traits.Snapshotter.
func (p *Party) AuditSnapshot() map[string]any {
	s := map[string]any{"kind": string(p.kind), "name": p.Name(), "active": p.IsActive(), "test": p.IsTest()}
	if !p.documentIssue.IsZero() {
		s["documentIssue"] = strings.TrimPrefix(p.documentIssue.Number+": "+p.documentIssue.Reason, ": ")
	}
	docs := make([]string, 0, len(p.identifications))
	for _, i := range p.identifications {
		docs = append(docs, i.Country.String()+":"+i.Number)
	}
	contacts := make([]string, 0, len(p.contacts))
	for _, c := range p.contacts {
		if c.IsActiveAt(fw.Now()) {
			contacts = append(contacts, string(c.Kind)+":"+c.Value+c.Address.String())
		}
	}
	now := fw.Now()
	var roles, classes, selfEmployed []string
	for _, r := range p.roles {
		if !r.IsActiveAt(now) {
			continue
		}
		roles = append(roles, r.RoleType.String())
		// Cada perfil, de los mismos roles que lista roles: el id del rol y sus datos entre
		// comillas (strconv.Quote), así una coma o un signo en el nombre comercial no lo confunden.
		if d := r.Details.SelfEmployed; !d.IsZero() {
			selfEmployed = append(selfEmployed, r.ID.String()+":"+strconv.Quote(d.TradeName)+" "+strconv.Quote(d.ActivityCode)+" "+
				strconv.Quote(d.RegisteredOn.String()))
		}
	}
	if len(selfEmployed) > 0 { // sólo si hay alguno: no cambia el registro de quien no es autónomo
		s["selfEmployed"] = strings.Join(selfEmployed, ",")
	}
	for _, c := range p.classifications {
		if c.Period.IsActiveAt(now) {
			classes = append(classes, c.Type.String())
		}
	}
	var orgs []string
	for _, o := range p.OrganizationsAt(now) {
		orgs = append(orgs, o.String())
	}
	s["organizations"] = strings.Join(orgs, ",")
	s["shared"] = p.shared
	s["roles"] = strings.Join(roles, ",")
	s["classifications"] = strings.Join(classes, ",")
	s["identifications"] = strings.Join(docs, ",")
	s["contacts"] = strings.Join(contacts, ",")
	if p.kind == KindPerson {
		s["gender"] = string(p.person.Gender)
		s["birthDate"] = p.person.BirthDate.String()
		s["maritalStatus"] = string(p.person.MaritalStatus)
	} else {
		s["legalName"] = p.organization.Name.Legal()
		s["tradeName"] = p.organization.Name.Trade()
		s["legalForm"] = string(p.organization.LegalForm)
	}
	return s
}
