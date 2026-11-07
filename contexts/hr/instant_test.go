package hr_test

import (
	"context"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/hr"
	happ "github.com/jhermoso/karpo-fw-go/contexts/hr/application"
	hdomain "github.com/jhermoso/karpo-fw-go/contexts/hr/domain"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// zones hace de anfitrión: la zona de cada empresa; la que no está, UTC.
type zones map[string]*time.Location

func (z zones) DateOf(_ context.Context, company fw.UUID, at time.Time) (vocab.Date, error) {
	if loc := z[company.String()]; loc != nil {
		return vocab.DateOf(at.In(loc)), nil
	}
	return vocab.DateOf(at.UTC()), nil
}

// Ocupar un puesto desde las 23:30 UTC del 12 de marzo mira el empleo el día de la empresa: en
// Kiritimati (UTC+14) ya es el 13, el día del alta; sin zona aún es el 12, y la persona no es
// empleada todavía.
func TestHR_FillPositionFromAnInstantIsOnTheCompanysDay(t *testing.T) {
	kiritimati, err := time.LoadLocation("Pacific/Kiritimati")
	if err != nil {
		t.Fatal(err)
	}
	z := zones{}
	h := compose(t, hr.WithToday(z))
	from := time.Date(2026, 3, 12, 23, 30, 0, 0, time.UTC)
	var types []hdomain.PositionType
	for name, want := range map[string]int{"Kiritimati": 200, "UTC": 422} {
		org, err := h.parties.Service.RegisterOrganization.Handle(h.adminCtx, papp.RegisterOrganization{LegalName: "Acme " + name,
			Roles: []string{pdomain.RoleInternalOrganization.String()}})
		if err != nil {
			t.Fatal(err)
		}
		if name == "Kiritimati" {
			z[org.ID] = kiritimati
		}
		h.grant("clerk", authz.Full, org.ID)
		var ana papp.PartyDTO
		h.must(h.do("POST", "/api/persons", "clerk", map[string]any{"givenName": "Ana", "firstSurname": name,
			"affiliation": map[string]any{"organization": org.ID, "relationshipType": pdomain.RelEmployment.String()}}, &ana), 201, "ana")
		h.must(h.do("POST", "/api/hr/employments", "clerk", map[string]any{"person": ana.ID, "employer": org.ID, "hired": "2026-03-13"}, nil), 201, "hire")
		if types == nil {
			h.must(h.do("GET", "/api/hr/catalogs/position-types?active=true", "clerk", nil, &types), 200, "types")
		}
		var p happ.PositionDTO
		h.must(h.do("POST", "/api/hr/positions", "clerk", map[string]any{"unit": org.ID, "type": types[0].ID.String(),
			"plannedFrom": from.AddDate(0, 0, -10), "fullTime": true}, &p), 201, "position")
		h.must(h.do("POST", "/api/hr/positions/"+p.ID+"/fill", "clerk", map[string]any{"person": ana.ID, "from": from}, nil), want, "fill in "+name)
	}
}
