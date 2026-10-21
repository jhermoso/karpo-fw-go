package application

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
)

// Fusionar fichas y sus relaciones de negocio (docs/DECISIONES-COMUNES.md, D-93; docs/PARTIES.md
// §17). Fusionar es marcar la ficha que cede como «Duplicate Of» de la superviviente; en la misma
// unidad de trabajo, cada relación vigente de cliente o de proveedor de la que cede con una empresa
// pasa a la superviviente si ésta no la tiene ya con esa empresa y quien fusiona puede escribir en
// la empresa: se termina la de la que cede y se establece una igual (mismo tipo, misma empresa,
// mismo número de cliente, mismo fin) que la continúa. Las demás se quedan en la que cede,
// bloqueadas con ella, para revisarlas caso por caso. Deshacer la fusión devuelve las que pasaron.
//
// La ficha que cede sigue afiliada a la empresa mientras dure la fusión, aunque su relación haya
// terminado: así la empresa la sigue encontrando (D-64) y quien la fusionó puede deshacerlo (D-96).
// Al deshacer, esa afiliación termina y la abre la relación devuelta.

// businessTypes son los tipos de relación de negocio que pasan a la superviviente.
var businessTypes = []domain.RelationshipTypeID{domain.RelCustomer, domain.RelSupplier}

// mergeClockSkew es el margen con el que se acepta una fecha de fusión, o de deshacerla, distinta
// del reloj del servidor.
const mergeClockSkew = time.Minute

// maxTransferChain limita el recorrido de la cadena de relaciones que se continúan unas a otras.
// La cadena no tiene ciclos (cada eslabón apunta a una relación anterior); el límite sólo protege
// de datos corruptos.
const maxTransferChain = 1000

// mergeMoment comprueba la fecha de una fusión, o de deshacerla, y devuelve el instante en que se
// mueven las relaciones. Se fusiona y se deshace ahora, con un margen para el reloj en los dos
// sentidos: una fecha pasada movería o resucitaría relaciones que cambiaron después, y una futura
// movería relaciones que pueden cambiar antes. Dentro del margen, las relaciones se buscan y se
// mueven en el más tardío de los dos instantes: no pasa una que terminó hace unos segundos, ni se
// olvida una creada en ese minuto; at sigue siendo la fecha de la marca.
func mergeMoment(at time.Time) (time.Time, error) {
	now := fw.Now()
	if d := at.Sub(now); d < -mergeClockSkew || d > mergeClockSkew {
		return time.Time{}, fw.Violation("parties.merge_not_now", "a merge is made, or undone, now")
	}
	if at.After(now) {
		return at, nil
	}
	return now, nil
}

// counterpart devuelve la empresa de una relación de negocio r de tipo rt y los lados que tendría
// la misma relación con other en lugar de p. ok es falso cuando p no ocupa el lado del tercero (es
// la empresa): esa relación no pasa a nadie.
func counterpart(rt domain.RelationshipType, r *domain.Relationship, p, other domain.PartyID) (org, from, to domain.PartyID, ok bool) {
	if rt.FromRole == domain.RoleInternalOrganization { // proveedor: la empresa compra al tercero
		return r.From(), r.From(), other, r.To() == p
	}
	return r.To(), other, r.To(), r.From() == p // cliente: el tercero compra a la empresa
}

// otherSide devuelve la party de r que no es p.
func otherSide(r *domain.Relationship, p domain.PartyID) domain.PartyID {
	if r.From() == p {
		return r.To()
	}
	return r.From()
}

// blockedOrg dice si la empresa de una relación está marcada como duplicado: con ella no se
// establece ninguna relación (D-64), así que no pasa a nadie.
func (s service) blockedOrg(ctx context.Context, org domain.PartyID) (bool, error) {
	p, err := s.Parties.Get(ctx, org)
	if err != nil {
		return false, err
	}
	return p.IsDuplicate(), nil
}

// transferBusiness pasa a survivor las relaciones de negocio de dup vigentes en at que puede pasar.
func (s service) transferBusiness(ctx context.Context, dup, survivor domain.PartyID, at time.Time) error {
	types, err := s.relationshipTypes(ctx)
	if err != nil {
		return err
	}
	rs, err := s.Relationships.Find(ctx, spec.And(domain.RelFieldType.In(businessTypes...), domain.Involving(dup), domain.ActiveAt(at)))
	if err != nil {
		return err
	}
	sc := s.scope(ctx)
	for _, old := range rs {
		rt, known := types[old.Type()]
		if !known {
			continue
		}
		org, from, to, ok := counterpart(rt, old, dup, survivor)
		// Se queda en la que cede: si es la empresa de la relación, si la empresa es la propia
		// superviviente, si quien fusiona no puede escribir en la empresa (decisión de esta
		// sesión, pendiente de Javier: docs/PARTIES.md §17) o si la empresa es a su vez un
		// duplicado.
		if !ok || org == survivor || !sc.canWriteOrg(org) {
			continue
		}
		if blocked, err := s.blockedOrg(ctx, org); err != nil {
			return err
		} else if blocked {
			continue
		}
		if has, err := s.Relationships.Exists(ctx, domain.SameRelationship(rt, from, to, at)); err != nil {
			return err
		} else if has {
			continue // ya la tiene: se queda en la que cede, para revisarla caso por caso
		}
		until := old.Until()
		if _, err := s.relationships.Update(ctx, old.ID(), func(_ context.Context, r *domain.Relationship) error { return r.Terminate(at) }); err != nil {
			return err
		}
		if _, err := s.continueRelationship(ctx, rt, old, from, to, at, until, false); err != nil {
			return err
		}
	}
	return nil
}

// returnBusiness devuelve a dup las relaciones de negocio que pasaron a survivor y que la
// superviviente aún tiene en at: termina la de la superviviente y establece para dup una igual que
// la continúa. Quien deshace tiene que poder escribir en la empresa de cada una que vuelve (403,
// como D-96 pide poder cambiar las dos fichas). Se quedan en la superviviente, y se listan en Kept,
// las que dup ya tiene otra vez con esa empresa (una renovación que empezó después) y las de
// empresas marcadas como duplicado. Las que la superviviente terminó durante la fusión no vuelven.
// Los perfiles que los otros contextos copiaron a la superviviente se quedan en ella, y en la
// vuelta no se copia nada (docs/PARTIES.md §17).
func (s service) returnBusiness(ctx context.Context, dup, survivor domain.PartyID, at time.Time) (*MergeDTO, error) {
	out := &MergeDTO{Transferred: []MergedRelationshipDTO{}, Kept: []MergedRelationshipDTO{}}
	mine, err := s.Relationships.Find(ctx, spec.And(domain.RelFieldType.In(businessTypes...), domain.Involving(dup)))
	if err != nil || len(mine) == 0 {
		return out, err
	}
	ids := make([]domain.RelationshipID, len(mine))
	for i, r := range mine {
		ids[i] = r.ID()
	}
	successors, err := s.Relationships.Find(ctx, spec.And(domain.RelFieldTransferredFrom.In(ids...), domain.Involving(survivor)))
	if err != nil {
		return nil, err
	}
	types, err := s.relationshipTypes(ctx)
	if err != nil {
		return nil, err
	}
	type pending struct {
		r             *domain.Relationship
		rt            domain.RelationshipType
		org, from, to domain.PartyID
	}
	var current []pending
	sc := s.scope(ctx)
	for _, r := range successors {
		if end := r.Until(); end != nil && !end.After(at) {
			continue // la superviviente la terminó: no vuelve
		}
		rt, known := types[r.Type()]
		if !known {
			continue
		}
		org, from, to, ok := counterpart(rt, r, survivor, dup)
		if !ok {
			continue
		}
		if !sc.canWriteOrg(org) {
			return nil, fmt.Errorf("%w: undoing the merge gives back a relationship with a company you cannot change", fw.ErrForbidden)
		}
		current = append(current, pending{r, rt, org, from, to})
	}
	// Ninguna relación de dup que tenga sucesora en la superviviente sigue afiliándola: termine la
	// sucesora como termine, la afiliación que dup conservó durante la fusión se cierra ahora.
	if err := s.endKeptAffiliations(ctx, dup, successors, at); err != nil {
		return nil, err
	}
	for _, c := range current {
		item := MergedRelationshipDTO{Relationship: c.r.ID().String(), Type: c.r.Type().String(), Counterparty: c.org.String()}
		blocked, err := s.blockedOrg(ctx, c.org)
		if err != nil {
			return nil, err
		}
		has, err := s.Relationships.Exists(ctx, domain.SameRelationship(c.rt, c.from, c.to, at))
		if err != nil {
			return nil, err
		}
		if blocked || has {
			out.Kept = append(out.Kept, item)
			continue
		}
		until := c.r.Until()
		if _, err := s.relationships.Update(ctx, c.r.ID(), func(_ context.Context, r *domain.Relationship) error { return r.Terminate(at) }); err != nil {
			return nil, err
		}
		if err := s.endAffiliations(ctx, c.r.ID(), at); err != nil {
			return nil, err
		}
		back, err := s.continueRelationship(ctx, c.rt, c.r, c.from, c.to, at, until, true)
		if err != nil {
			return nil, err
		}
		item.Relationship, item.Previous = back.ID().String(), c.r.ID().String()
		out.Transferred = append(out.Transferred, item)
	}
	return out, nil
}

// endKeptAffiliations cierra en at las afiliaciones de dup que vienen de relaciones con sucesora y
// que siguen abiertas en at (sin fin, o con un fin posterior).
func (s service) endKeptAffiliations(ctx context.Context, dup domain.PartyID, successors []*domain.Relationship, at time.Time) error {
	p, err := s.Parties.Get(ctx, dup)
	if err != nil {
		return err
	}
	var open []domain.RelationshipID
	for _, a := range p.Affiliations() {
		end, closed := a.Period.To()
		if (!closed || end.After(at)) && slices.ContainsFunc(successors, func(r *domain.Relationship) bool { return r.TransferredFrom() == a.Relationship }) {
			open = append(open, a.Relationship)
		}
	}
	if len(open) == 0 {
		return nil // nada que cerrar: no se sube la versión de una party que no cambió
	}
	_, err = s.parties.Update(ctx, dup, func(_ context.Context, p *domain.Party) error {
		for _, id := range open {
			if err := p.EndAffiliation(id, at); err != nil {
				return err
			}
		}
		return nil
	})
	return err
}

// continueRelationship establece entre from y to, desde at y hasta until (nil: sin fin), la
// relación que continúa a prev; back dice si vuelve al deshacer una fusión.
func (s service) continueRelationship(ctx context.Context, rt domain.RelationshipType, prev *domain.Relationship, from, to domain.PartyID,
	at time.Time, until *time.Time, back bool) (*domain.Relationship, error) {
	f, err := s.Parties.Get(ctx, from)
	if err != nil {
		return nil, err
	}
	t, err := s.Parties.Get(ctx, to)
	if err != nil {
		return nil, err
	}
	r, err := s.establish(ctx, rt, f, t, at, prev.Remark(), RelationshipDetailsInput{continues: prev, returning: back})
	if err != nil || until == nil {
		return r, err
	}
	// Termina cuando terminaba la que continúa, y con ella la afiliación que abrió.
	if r, err = s.relationships.Update(ctx, r.ID(), func(_ context.Context, r *domain.Relationship) error { return r.Terminate(*until) }); err != nil {
		return nil, err
	}
	return r, s.endAffiliations(ctx, r.ID(), *until)
}

// endAffiliations termina las afiliaciones que produjo una relación.
func (s service) endAffiliations(ctx context.Context, rel domain.RelationshipID, at time.Time) error {
	r, err := s.Relationships.Get(ctx, rel)
	if err != nil {
		return err
	}
	for _, id := range []domain.PartyID{r.From(), r.To()} {
		p, err := s.Parties.Get(ctx, id)
		if err != nil {
			return err
		}
		if !slices.ContainsFunc(p.Affiliations(), func(a domain.Affiliation) bool { return a.Relationship == rel }) {
			continue // nada que terminar: no se sube la versión de una party que no cambió
		}
		if _, err := s.parties.Update(ctx, id, func(_ context.Context, p *domain.Party) error { return p.EndAffiliation(rel, at) }); err != nil {
			return err
		}
	}
	return nil
}

// mergeOf dice qué fue, en at, de las relaciones de negocio de dup: las que pasaron a survivor y las
// que se quedaron en dup sin terminar (las que survivor ya tenía, las que empiezan después de la
// fusión, las de empresas en las que quien fusiona no escribe y aquellas en las que dup es la
// empresa).
func (s service) mergeOf(ctx context.Context, dup, survivor domain.PartyID, at time.Time) (*MergeDTO, error) {
	out := &MergeDTO{Transferred: []MergedRelationshipDTO{}, Kept: []MergedRelationshipDTO{}}
	mine, err := s.Relationships.Find(ctx, spec.And(domain.RelFieldType.In(businessTypes...), domain.Involving(dup)))
	if err != nil {
		return nil, err
	}
	var ids []domain.RelationshipID
	for _, r := range mine {
		ids = append(ids, r.ID())
		if end := r.Until(); end == nil || end.After(at) {
			out.Kept = append(out.Kept, MergedRelationshipDTO{Relationship: r.ID().String(), Type: r.Type().String(), Counterparty: otherSide(r, dup).String()})
		}
	}
	if len(ids) == 0 {
		return out, nil
	}
	moved, err := s.Relationships.Find(ctx, spec.And(domain.RelFieldTransferredFrom.In(ids...), domain.Involving(survivor), domain.ActiveAt(at)))
	if err != nil {
		return nil, err
	}
	for _, r := range moved {
		out.Transferred = append(out.Transferred, MergedRelationshipDTO{Relationship: r.ID().String(), Previous: r.TransferredFrom().String(),
			Type: r.Type().String(), Counterparty: otherSide(r, survivor).String()})
	}
	return out, nil
}
