package application

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
)

// MaxHierarchyDepth limita todo recorrido de una jerarquía de organizaciones (los 10 niveles de C#).
const MaxHierarchyDepth = 10

// NewAffiliation relaciona una party que se está dando de alta con una organización interna, para
// que nazca dentro del ámbito del llamante (el alta rápida de cliente de C# hacía lo mismo con la
// relación de cliente). La party recibe el rol que exige su lado del tipo de relación.
type NewAffiliation struct {
	Organization     string `json:"organization"`
	RelationshipType string `json:"relationshipType"`
	RelationshipDetailsInput
}

// canRelate: los administradores globales relacionan cualquier cosa; si no, un lado tiene que ser
// una organización en la que el llamante pueda escribir, o el llamante tiene que poder escribir
// en las dos parties.
func (s scope) canRelate(from, to *domain.Party) bool {
	now := fw.Now()
	return s.global || s.canWriteOrg(from.ID()) || s.canWriteOrg(to.ID()) || (s.canWrite(from, now) && s.canWrite(to, now))
}

// register crea una party, le da sus roles y, si se pide, su primera relación. Un llamante que no
// es administrador global tiene que dar de alta la party dentro de su ámbito.
func (s service) register(ctx context.Context, build func(domain.PartyID) (*domain.Party, error), roles []string, aff *NewAffiliation) (PartyDTO, error) {
	// La party y su primera relación son una sola operación en el registro de auditoría: así se
	// sabe después que la organización la dio de alta (D-74). Una llamada sin correlación propia
	// recibe una.
	if app.CorrelationID(ctx) == "" {
		ctx = app.WithCorrelationID(ctx, fw.NewUUID().String())
	}
	sc := s.scope(ctx)
	if aff == nil && !sc.global {
		var v fw.Validation
		v.Add("affiliation", "required", "register the party with an internal organization of your scope")
		return PartyDTO{}, v.Err()
	}
	n, err := s.names(ctx)
	if err != nil {
		return PartyDTO{}, err
	}
	p, err := build(domain.NewPartyID())
	if err != nil {
		return PartyDTO{}, err
	}
	now := fw.Now()
	for _, r := range roles {
		id, err := parseRoleType(r)
		if err != nil {
			return PartyDTO{}, err
		}
		if err := mayGive(sc, n.Roles, id); err != nil {
			return PartyDTO{}, err
		}
		if _, err := p.AssignRole(n.Roles, id, now); err != nil {
			return PartyDTO{}, err
		}
	}

	var org *domain.Party
	var rt domain.RelationshipType
	partyIsFrom := true
	if aff != nil {
		var v fw.Validation
		orgID, err := domain.ParsePartyID(aff.Organization)
		v.Require(err == nil, "affiliation.organization", "format", "organization must be a party id")
		typeID, err := domain.ParseRelationshipTypeID(aff.RelationshipType)
		v.Require(err == nil, "affiliation.relationshipType", "format", "relationship type must be an id")
		if err := v.Err(); err != nil {
			return PartyDTO{}, err
		}
		if org, err = sc.visible(s.Parties.Get(ctx, orgID)); err != nil {
			return PartyDTO{}, err
		}
		if !sc.canWriteOrg(orgID) {
			return PartyDTO{}, fw.ErrForbidden
		}
		types, err := s.relationshipTypes(ctx)
		if err != nil {
			return PartyDTO{}, err
		}
		var ok bool
		if rt, ok = types[typeID]; !ok {
			v.Add("affiliation.relationshipType", "unknown", "unknown relationship type")
			return PartyDTO{}, v.Err()
		}
		if rt.ID == domain.RelFamily || rt.ID == domain.RelDuplicateOf {
			v.Add("affiliation.relationshipType", "not_an_affiliation", "this relationship type does not affiliate a party with an organization")
			return PartyDTO{}, v.Err()
		}
		// La organización ocupa el lado que desempeña; la party nueva, el otro.
		switch {
		case org.Meets(n.Roles, rt.ToRole, now):
			partyIsFrom = true
		case org.Meets(n.Roles, rt.FromRole, now):
			partyIsFrom = false
		default:
			return PartyDTO{}, fw.Violation("parties.relationship_role_missing", "the organization does not play a role of "+rt.Name.String())
		}
		role := rt.FromRole
		if !partyIsFrom {
			role = rt.ToRole
		}
		if !p.Meets(n.Roles, role, now) {
			if _, err := p.AssignRole(n.Roles, role, now); err != nil {
				return PartyDTO{}, err
			}
		}
	}
	if err := s.parties.Create(ctx, p); err != nil {
		return PartyDTO{}, err
	}
	if org != nil {
		from, to := p, org
		if !partyIsFrom {
			from, to = org, p
		}
		if _, err := s.establish(ctx, rt, from, to, now, "", aff.RelationshipDetailsInput); err != nil {
			return PartyDTO{}, err
		}
		if p, err = s.Parties.Get(ctx, p.ID()); err != nil {
			return PartyDTO{}, err
		}
	}
	return n.dto(p), nil
}

// establish crea una relación y la proyecta en las parties: al lado al que le falta el rol del
// tipo se le da, y una party relacionada con una organización interna queda afiliada a ella
// (visibilidad, pertenencia). Los tipos jerárquicos mantienen un padre por hijo y ningún ciclo.
// Todo ocurre en la unidad de trabajo del llamante.
func (s service) establish(ctx context.Context, rt domain.RelationshipType, from, to *domain.Party, since time.Time, remark string,
	details RelationshipDetailsInput) (*domain.Relationship, error) {
	cat, err := s.roleCatalog(ctx)
	if err != nil {
		return nil, err
	}
	if rt.Hierarchical {
		if err := s.checkHierarchy(ctx, rt, from.ID(), to.ID(), since); err != nil {
			return nil, err
		}
	}
	for _, p := range []*domain.Party{from, to} {
		if err := operable(p); err != nil {
			return nil, err
		}
	}
	var moment time.Time // cuándo se mueven las relaciones de negocio de una fusión
	if rt.ID == domain.RelDuplicateOf {
		if err := canMarkDuplicate(ctx, s.scope(ctx), from, to); err != nil {
			return nil, err
		}
		if moment, err = mergeMoment(since); err != nil {
			return nil, err
		}
		// No se encadenan fusiones (D-97): la superviviente de otra no se fusiona en nadie.
		if survives, err := s.Relationships.Exists(ctx, spec.And(domain.RelFieldType.Eq(domain.RelDuplicateOf), domain.RelFieldTo.Eq(from.ID()),
			domain.NotEndedAt(since))); err != nil {
			return nil, err
		} else if survives {
			return nil, fw.Violation("parties.merge_chain", "the party is the survivor of another merge: merge that one directly into the new survivor")
		}
	}
	if dup, err := s.Relationships.Exists(ctx, domain.SameRelationship(rt, from.ID(), to.ID(), since)); err != nil {
		return nil, err
	} else if dup {
		return nil, fw.Violation("parties.duplicate_relationship", "the parties already have this relationship in that period")
	}
	if from, err = s.playing(ctx, cat, from, rt.FromRole, since); err != nil {
		return nil, err
	}
	if to, err = s.playing(ctx, cat, to, rt.ToRole, since); err != nil {
		return nil, err
	}
	r, err := domain.Establish(domain.NewRelationshipID(), rt, from, to, cat, since, remark)
	if err != nil {
		return nil, err
	}
	// Una prueba que llega con la relación sigue siendo una prueba que se concede.
	if details.Prospect != nil && details.Prospect.TrialUntil != nil {
		if err := authz.Require(ctx, PermRelationshipSetTrial); err != nil {
			return nil, err
		}
	}
	if err := details.apply(r, rt); err != nil {
		return nil, err
	}
	if err := s.relationships.Create(ctx, r); err != nil {
		return nil, err
	}
	if rt.ID == domain.RelDuplicateOf { // el duplicado se marca, no se afilia a nadie y cede sus relaciones de negocio (D-93)
		if _, err := s.parties.Update(ctx, from.ID(), func(_ context.Context, p *domain.Party) error { return p.MarkDuplicate() }); err != nil {
			return nil, err
		}
		if err := s.transferBusiness(ctx, from.ID(), to.ID(), moment); err != nil {
			return nil, err
		}
		return r, nil
	}
	if rt.ID == domain.RelFamily { // no es una relación de negocio: no se la enseña a nadie (D-67)
		return r, nil
	}
	for _, side := range [][2]*domain.Party{{from, to}, {to, from}} {
		member, org := side[0], side[1]
		if !org.PlaysAt(cat, domain.RoleInternalOrganization, since) {
			continue
		}
		if _, err := s.parties.Update(ctx, member.ID(), func(_ context.Context, p *domain.Party) error {
			return p.Affiliate(org.ID(), r.ID(), since)
		}); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// playing devuelve la party que desempeña el rol que una relación le pide, y le da el rol si le
// falta (docs/DECISIONES-COMUNES.md, D-49). La relación se rechaza como antes cuando el rol no es
// de los que se dan así: una categoría, que no se asigna a nadie; Internal Organization o un rol
// por debajo, que decide quién ve y escribe qué y sólo se da a propósito; y cualquier rol de una
// party a la que el llamante no podría dárselo a mano (una party compartida, una que sólo lee).
func (s service) playing(ctx context.Context, cat *domain.Catalog, p *domain.Party, role domain.RoleTypeID, since time.Time) (*domain.Party, error) {
	if p.Meets(cat, role, since) || cat.IsA(role, domain.RoleInternalOrganization) {
		return p, nil
	}
	if t, ok := cat.RoleType(role); !ok || t.Category {
		return p, nil
	}
	if !s.scope(ctx).canWrite(p, fw.Now()) {
		return p, nil
	}
	return s.parties.Update(ctx, p.ID(), func(_ context.Context, p *domain.Party) error {
		_, err := p.AssignRole(cat, role, since)
		return err
	})
}

// checkHierarchy rechaza un segundo padre vigente y los ciclos (entre los antepasados del padre no
// puede estar el hijo).
func (s service) checkHierarchy(ctx context.Context, rt domain.RelationshipType, child, parent domain.PartyID, since time.Time) error {
	current := domain.RelFieldType.Eq(rt.ID).And(domain.RelFieldUntil.IsNull().Or(domain.RelFieldUntil.After(since)))
	seen := []domain.PartyID{parent}
	for level, node := 0, parent; ; level++ {
		if node == child {
			return fw.Violation("parties.hierarchy_cycle", "the parent is already below the child in the hierarchy")
		}
		if level >= MaxHierarchyDepth {
			return fw.Violation("parties.hierarchy_too_deep", "the hierarchy would be deeper than the allowed levels")
		}
		ups, err := s.Relationships.Find(ctx, current.And(domain.RelFieldFrom.Eq(node)))
		if err != nil {
			return err
		}
		if len(ups) == 0 {
			break
		}
		node = ups[0].To()
		if slices.Contains(seen, node) {
			break // un ciclo que ya existe no es culpa de este cambio; se deja de recorrer
		}
		seen = append(seen, node)
	}
	if has, err := s.Relationships.Exists(ctx, spec.And(current, domain.RelFieldFrom.Eq(child))); err != nil {
		return err
	} else if has {
		return fw.Violation("parties.hierarchy_one_parent", "the child already has a parent: end that relationship first")
	}
	return nil
}

// writableRelationship carga una relación que el llamante va a cambiar, con sus parties: 404 si
// está fuera del ámbito, 403 si el llamante no puede relacionar sus parties.
func (s service) writableRelationship(ctx context.Context, id domain.RelationshipID) (r *domain.Relationship, from, to *domain.Party, err error) {
	sc := s.scope(ctx)
	if r, err = s.Relationships.Get(ctx, id); err != nil {
		return nil, nil, nil, err
	}
	if from, err = s.Parties.Get(ctx, r.From()); err != nil {
		return nil, nil, nil, err
	}
	if to, err = s.Parties.Get(ctx, r.To()); err != nil {
		return nil, nil, nil, err
	}
	if r.Type() == domain.RelDuplicateOf { // quien puede escribir el duplicado llega a su marca
		if !sc.sees(from, fw.Now()) {
			return nil, nil, nil, fw.NotFound(domain.RelationshipKind, id)
		}
		if err := canMarkDuplicate(ctx, sc, from, to); err != nil {
			return nil, nil, nil, err
		}
		return r, from, to, nil
	}
	if !sc.relationships().IsSatisfiedBy(r) {
		return nil, nil, nil, fw.NotFound(domain.RelationshipKind, id)
	}
	if !sc.canRelate(from, to) {
		return nil, nil, nil, fw.ErrForbidden
	}
	return r, from, to, nil
}

// operable rechaza cualquier cambio a una party marcada como duplicado de otra: se encuentra y se
// lee, y nada más (docs/DECISIONES-COMUNES.md, D-64). Terminar sus relaciones sigue permitido,
// incluida la Duplicate Of, que es como se deshace la marca.
func operable(p *domain.Party) error {
	if p.IsDuplicate() {
		return fw.Violation("parties.duplicate", "the party is the duplicate of another: it takes part in nothing but queries")
	}
	return nil
}

// canMarkDuplicate dice si el llamante puede marcar la party p como duplicado de survivor, o
// quitarle la marca. Fusionar es una acción propia (D-95); y, como cambia las dos fichas (las
// relaciones de negocio de la que cede pasan a la superviviente, D-93), hay que poder escribir en
// las dos (D-96). Una organización interna sólo la bloquea el administrador global.
func canMarkDuplicate(ctx context.Context, sc scope, p, survivor *domain.Party) error {
	if err := authz.Require(ctx, PermPartyMerge); err != nil {
		return err
	}
	if sc.global {
		return nil
	}
	now := fw.Now()
	if !sc.canWrite(p, now) {
		return fmt.Errorf("%w: marking a party as a duplicate needs to be able to change it", fw.ErrForbidden)
	}
	if !sc.canWrite(survivor, now) {
		return fmt.Errorf("%w: merging into a party needs to be able to change the survivor too", fw.ErrForbidden)
	}
	if slices.ContainsFunc(p.Roles(), func(r domain.PartyRole) bool { return r.RoleType == domain.RoleInternalOrganization && !r.Annulled }) {
		return fmt.Errorf("%w: only the global administrator marks an internal organization as a duplicate", fw.ErrForbidden)
	}
	return nil
}

// operableRelationship es writableRelationship para un cambio en la propia relación (sus datos):
// ninguna de sus parties puede ser un duplicado.
func (s service) operableRelationship(ctx context.Context, id domain.RelationshipID) (*domain.Relationship, *domain.Party, *domain.Party, error) {
	r, from, to, err := s.writableRelationship(ctx, id)
	if err != nil {
		return nil, nil, nil, err
	}
	for _, p := range []*domain.Party{from, to} {
		if err := operable(p); err != nil {
			return nil, nil, nil, err
		}
	}
	return r, from, to, nil
}

// terminate termina una relación y las afiliaciones que produjo. El llamante necesita ver la
// relación y tener permiso para relacionar sus parties. Terminar una «Duplicate Of» es deshacer la
// fusión: sólo una vez, desde ahora, y devuelve lo que hizo con las relaciones de negocio (D-93).
func (s service) terminate(ctx context.Context, id domain.RelationshipID, at time.Time) (*domain.Relationship, *MergeDTO, error) {
	r, from, to, err := s.writableRelationship(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if r.Type() == domain.RelDuplicateOf {
		if r.Until() != nil {
			return nil, nil, fw.Violation("parties.merge_undone", "the merge has already been undone")
		}
	}
	moment := at
	if r.Type() == domain.RelDuplicateOf {
		if moment, err = mergeMoment(at); err != nil {
			return nil, nil, err
		}
	}
	if r, err = s.relationships.Update(ctx, id, func(_ context.Context, r *domain.Relationship) error { return r.Terminate(at) }); err != nil {
		return nil, nil, err
	}
	var merge *MergeDTO
	if r.Type() == domain.RelDuplicateOf { // deshacer la marca es terminar la relación, y vuelven las relaciones que pasaron (D-93)
		if _, err := s.parties.Update(ctx, r.From(), func(_ context.Context, p *domain.Party) error { p.ClearDuplicate(); return nil }); err != nil {
			return nil, nil, err
		}
		if merge, err = s.returnBusiness(ctx, from.ID(), to.ID(), moment); err != nil {
			return nil, nil, err
		}
	}
	if err := s.endAffiliations(ctx, id, at); err != nil {
		return nil, nil, err
	}
	return r, merge, nil
}
