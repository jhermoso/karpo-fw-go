package domain

import (
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// PartyKind is the stable aggregate type name of Party.
const PartyKind = "parties.party"

// Kind distinguishes people from organizations. In C# they were subclasses of Party mapped TPT
// (Person, Organization, LegalOrganization, Corporation); here the kind is data and the details
// of each kind are value objects, so a party never changes class to change its data.
type Kind string

// Party kinds.
const (
	KindPerson       Kind = "person"
	KindOrganization Kind = "organization"
)

// PersonDetails are the data of a person.
type PersonDetails struct {
	Name          PersonalName
	Gender        Gender
	BirthDate     vocab.Date // zero when unknown
	MaritalStatus MaritalStatus
}

func (d PersonDetails) validate() error {
	var v fw.Validation
	v.Require(!d.Name.IsZero(), "name", "required", "a person needs a name")
	v.Require(d.BirthDate.IsZero() || !d.BirthDate.After(vocab.DateOf(fw.Now())), "birthDate", "future", "birth date cannot be in the future")
	return v.Err()
}

// OrganizationDetails are the data of an organization.
type OrganizationDetails struct {
	Name      OrganizationName
	LegalForm LegalForm
}

// PartyRole is a role played by a party during a period (child entity of Party).
type PartyRole struct {
	ID       PartyRoleID
	RoleType RoleTypeID
	Period   vocab.ValidPeriod
	// Annulled marks a role given by mistake that could not be taken away because something
	// was already said about the party in that role. It ended when it was annulled.
	Annulled bool
}

// IsActiveAt reports whether the role is played at t.
func (r PartyRole) IsActiveAt(t time.Time) bool { return r.Period.IsActiveAt(t) }

// Party is someone taking part in the business: a person or an organization, with the roles it
// plays. Roles are child entities: their invariants (compatible kind, no overlapping periods of
// the same role) are guarded here, which the C# bypassed by persisting PartyRole directly.
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

// DocumentIssue marks a party whose identity document was refused: the number as it came and why
// it was not taken. It is how a party loaded by an import with a wrong document stays in sight
// until somebody corrects it (docs/PARTIES-MODELO-COMUN.md, D-5 and D-9). The zero value is no
// issue.
type DocumentIssue struct {
	Number string // as it came, for who corrects it; it identifies nobody
	Reason string
}

// IsZero reports whether there is no issue.
func (d DocumentIssue) IsZero() bool { return d == DocumentIssue{} }

// Limits of a document issue.
const (
	MaxDocumentIssueNumber = 60
	MaxDocumentIssueReason = 200
)

// RegisterPerson creates a person.
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

// RegisterOrganization creates an organization.
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

// PartyState is the persisted state of a party (used by Reconstitute).
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

// Reconstitute rebuilds a party from persisted state, without events.
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

// IsDuplicate reports whether the party is a duplicate of another: it is still found and read,
// but takes part in nothing else (docs/DECISIONES-COMUNES.md, D-64). The Duplicate Of
// relationship says of which party; this mark is its projection, so that searches can leave it
// out.
func (p *Party) IsDuplicate() bool { return p.duplicate }

// MarkDuplicate marks the party as a duplicate. A party is the duplicate of one party only.
func (p *Party) MarkDuplicate() error {
	if p.duplicate {
		return fw.Violation("parties.already_duplicate", "the party is already the duplicate of another")
	}
	p.duplicate = true
	p.Raise(PartyMarkedDuplicate{EventMeta: p.NewEventMeta()})
	return nil
}

// ClearDuplicate takes the mark away: the Duplicate Of relationship ended.
func (p *Party) ClearDuplicate() {
	if p.duplicate {
		p.duplicate = false
		p.Raise(PartyDuplicateCleared{EventMeta: p.NewEventMeta()})
	}
}

// DocumentIssue returns the mark of a refused identity document (zero when there is none).
func (p *Party) DocumentIssue() DocumentIssue { return p.documentIssue }

// HasDocumentIssue reports whether the party is marked with a refused identity document.
func (p *Party) HasDocumentIssue() bool { return !p.documentIssue.IsZero() }

// FlagDocumentIssue marks the party: its identity document was refused, for that reason. A new
// mark replaces the one there was.
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

// ClearDocumentIssue takes the mark away: somebody looked at it.
func (p *Party) ClearDocumentIssue() { p.documentIssue = DocumentIssue{} }

// Kind returns the party kind.
func (p *Party) Kind() Kind { return p.kind }

// Person returns the person details (zero for an organization).
func (p *Party) Person() PersonDetails { return p.person }

// Organization returns the organization details (zero for a person).
func (p *Party) Organization() OrganizationDetails { return p.organization }

// Name returns the display name.
func (p *Party) Name() string {
	if p.kind == KindPerson {
		return p.person.Name.String()
	}
	return p.organization.Name.String()
}

// Roles returns a copy of every role, current or past.
func (p *Party) Roles() []PartyRole { return slices.Clone(p.roles) }

// RoleTypes returns the role types of the roles (for specifications over the role collection).
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

// RenamePerson changes the name of a person.
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

// RenameOrganization changes the name of an organization.
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

// UpdatePersonDetails changes gender, birth date and marital status of a person.
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

// Activate reactivates the party.
func (p *Party) Activate() {
	if p.Activation.Activate() {
		p.Raise(PartyActivated{EventMeta: p.NewEventMeta()})
	}
}

// Deactivate deactivates the party. Its roles keep their periods: deactivation hides the party,
// it does not rewrite its history.
func (p *Party) Deactivate() {
	if p.Activation.Deactivate() {
		p.Raise(PartyDeactivated{EventMeta: p.NewEventMeta()})
	}
}

// AssignRole makes the party play a role from a moment on. Invariants: the role type exists, is
// not a category and suits the party kind; the party is active; and it does not already play
// the same role in an overlapping period.
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
	p.roles = append(slices.Clone(p.roles), role) // never share the backing array with a stored copy
	p.Raise(PartyRoleAssigned{EventMeta: p.NewEventMeta(), RoleID: role.ID.String(), RoleType: roleType.String(), From: period.From()})
	return role.ID, nil
}

// AnnulRole takes back a role given by mistake (docs/DECISIONES-COMUNES.md, D-8). When nothing
// depends on it, the role is removed as if it had never been given; when something does (the
// caller says so: it is the one who can look at the relationships), it ends at that moment and
// stays, marked as annulled. It reports whether the role was removed.
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
		at = end // it had ended already: that is when it stopped being played
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

// EndRole stops playing a role at a moment (not before it started).
func (p *Party) EndRole(id PartyRoleID, at time.Time) error {
	i := slices.IndexFunc(p.roles, func(r PartyRole) bool { return r.ID == id })
	if i < 0 {
		return fw.NotFound("parties.party_role", id)
	}
	r := p.roles[i]
	if end, closed := r.Period.To(); closed && !at.Before(end) {
		return nil // already ended at or before at
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

// PlaysAt reports whether the party plays roleType, or a role below it in the hierarchy, at t.
func (p *Party) PlaysAt(c RoleCatalog, roleType RoleTypeID, t time.Time) bool {
	for _, r := range p.roles {
		if !r.Annulled && r.IsActiveAt(t) && c.IsA(r.RoleType, roleType) { // an annulled role counts for nothing, whenever
			return true
		}
	}
	return false
}

// AuditSnapshot implements traits.Snapshotter.
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
	var roles, classes []string
	for _, r := range p.roles {
		if r.IsActiveAt(now) {
			roles = append(roles, r.RoleType.String())
		}
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
