package application

import (
	"context"
	"slices"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
)

// SearchPairs busca las parejas empresa–tercero de un tipo de relación: cada relación de cliente
// (o de proveedor) entre una empresa del grupo y un tercero es una fila
// (docs/DECISIONES-COMUNES.md, D-51 y D-73). Quien llama sólo ve las de sus empresas.
type SearchPairs struct {
	// Code es el código del tipo de relación: customer o supplier.
	Code string
	// Party filtra al tercero, como la búsqueda de parties; su paginación no cuenta.
	Party SearchParties
	// Organizations deja sólo las parejas de esas empresas del grupo.
	Organizations []string
	// IncludeEnded trae también las relaciones terminadas; si no, sólo las que no han terminado.
	IncludeEnded bool
	// ID, si no está vacío, busca una sola pareja: la de esa relación.
	ID string
}

// PairDTO es una pareja empresa–tercero tal como la conoce Parties. El anfitrión le añade lo que
// saben de ella los demás contextos (límite, bloqueos, perfil de proveedor).
type PairDTO struct {
	ID               string     `json:"id"` // la relación
	Party            PartyDTO   `json:"party"`
	Organization     string     `json:"internalOrganizationId"`
	OrganizationName string     `json:"internalOrganizationName"`
	CustomerNumber   string     `json:"customerNumber,omitempty"`
	Since            time.Time  `json:"since"`
	Until            *time.Time `json:"until,omitempty"`
}

// pairSide dice, para un tipo de relación de pareja, en qué lado está la empresa del grupo.
// Cliente: el cliente compra a la empresa (el cliente es el origen). Proveedor: la empresa compra
// al proveedor (la empresa es el origen).
func pairSide(code string) (domain.RelationshipTypeID, bool, error) {
	switch code {
	case domain.CodeCustomer:
		return domain.RelCustomer, false, nil
	case "supplier":
		return domain.RelSupplier, true, nil
	}
	var v fw.Validation
	v.Add("code", "enum", "a pair is of customers or of suppliers")
	return domain.RelationshipTypeID{}, false, v.Err()
}

// maxIDsPerQuery limita los identificadores de una consulta «IN»: Oracle no admite más de 1000.
const maxIDsPerQuery = 500

func (s service) pairs(ctx context.Context, q SearchPairs) ([]PairDTO, error) {
	typ, orgFirst, err := pairSide(q.Code)
	if err != nil {
		return nil, err
	}
	n, err := s.names(ctx)
	if err != nil {
		return nil, err
	}
	now := fw.Now()
	where := domain.RelFieldType.Eq(typ).And(s.scope(ctx).relationships())
	if q.ID != "" {
		id, err := domain.ParseRelationshipID(q.ID)
		if err != nil {
			var v fw.Validation
			v.Add("id", "format", "id must be a relationship id")
			return nil, v.Err()
		}
		where = where.And(domain.RelFieldID.Eq(id))
	} else if !q.IncludeEnded {
		where = where.And(domain.NotEndedAt(now))
	}
	if len(q.Organizations) > 0 {
		orgs := make([]domain.PartyID, 0, len(q.Organizations))
		for _, o := range q.Organizations {
			id, err := domain.ParsePartyID(o)
			if err != nil {
				var v fw.Validation
				v.Add("internalOrganizationIds", "format", "every organization must be a party id")
				return nil, v.Err()
			}
			orgs = append(orgs, id)
		}
		if orgFirst {
			where = where.And(domain.RelFieldFrom.In(orgs...))
		} else {
			where = where.And(domain.RelFieldTo.In(orgs...))
		}
	}
	rels, err := s.Relationships.Find(ctx, where, domain.RelFieldSince.Asc())
	if err != nil {
		return nil, err
	}
	// El tercero y la empresa de cada relación, leídos por lotes.
	sides := func(r *domain.Relationship) (org, party domain.PartyID) {
		if orgFirst {
			return r.From(), r.To()
		}
		return r.To(), r.From()
	}
	var partyIDs, orgIDs []domain.PartyID
	for _, r := range rels {
		org, party := sides(r)
		if !slices.Contains(partyIDs, party) {
			partyIDs = append(partyIDs, party)
		}
		if !slices.Contains(orgIDs, org) {
			orgIDs = append(orgIDs, org)
		}
	}
	filter, err := s.partyFilter(ctx, n, q.Party)
	if err != nil {
		return nil, err
	}
	parties, err := s.partiesByID(ctx, partyIDs, filter)
	if err != nil {
		return nil, err
	}
	orgs, err := s.partiesByID(ctx, orgIDs, spec.All[*domain.Party]())
	if err != nil {
		return nil, err
	}
	out := []PairDTO{}
	for _, r := range rels {
		org, party := sides(r)
		p, ok := parties[party]
		if !ok {
			continue // el tercero no pasa el filtro
		}
		row := PairDTO{ID: r.ID().String(), Party: n.dto(p), Organization: org.String(), Since: r.Since(), Until: r.Until()}
		if o, ok := orgs[org]; ok {
			row.OrganizationName = o.Name()
		}
		row.CustomerNumber = r.Details().Customer.Number
		out = append(out, row)
	}
	return out, nil
}

// partiesByID lee las parties de esos identificadores que cumplen el filtro, por lotes.
func (s service) partiesByID(ctx context.Context, ids []domain.PartyID, filter spec.Spec[*domain.Party]) (map[domain.PartyID]*domain.Party, error) {
	out := make(map[domain.PartyID]*domain.Party, len(ids))
	for start := 0; start < len(ids); start += maxIDsPerQuery {
		batch := ids[start:min(start+maxIDsPerQuery, len(ids))]
		ps, err := s.Parties.Find(ctx, spec.And(domain.WithIDs(batch...), filter))
		if err != nil {
			return nil, err
		}
		for _, p := range ps {
			out[p.ID()] = p
		}
	}
	return out, nil
}
