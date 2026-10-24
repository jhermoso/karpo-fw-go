package hr_test

import (
	"context"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/hr"
	happ "github.com/jhermoso/karpo-fw-go/contexts/hr/application"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// mergedParties hace de Parties: dice qué parties están fusionadas en otra.
type mergedParties struct{ ids []string }

func (m *mergedParties) MergedParties(context.Context) ([]string, error) { return m.ids, nil }

// El listado de relaciones laborales deja fuera por defecto las de una persona fusionada en otra
// party, y las trae con includeMergedParties; la relación pedida por su identidad se sigue
// encontrando (docs/DECISIONES-COMUNES.md, D-100).
func TestEmploymentsLeaveOutMergedParties(t *testing.T) {
	merged := &mergedParties{}
	h := compose(t, hr.WithMergedParties(merged))
	acme, err := h.parties.Service.RegisterOrganization.Handle(h.adminCtx, papp.RegisterOrganization{LegalName: "Acme fusiones",
		Roles: []string{pdomain.RoleInternalOrganization.String()}})
	if err != nil {
		t.Fatal(err)
	}
	h.grant("clerk", authz.Full, acme.ID)
	h.grant("reader", authz.ReadOnly, acme.ID)
	person := func(name string) papp.PartyDTO {
		var p papp.PartyDTO
		h.must(h.do("POST", "/api/persons", "clerk", map[string]any{"givenName": name, "firstSurname": "Fusiones",
			"affiliation": map[string]any{"organization": acme.ID, "relationshipType": pdomain.RelEmployment.String()}}, &p), 201, name)
		return p
	}
	ana, bea := person("Ana"), person("Bea")
	merged.ids = []string{bea.ID}

	hired := vocab.DateOf(fw.Now()).AddDays(-30)
	ids := map[string]string{}
	for _, p := range []papp.PartyDTO{ana, bea} {
		var e happ.EmploymentDTO
		h.must(h.do("POST", "/api/hr/employments", "clerk", map[string]any{"person": p.ID, "employer": acme.ID, "hired": hired.String()}, &e), 201, "hire")
		ids[p.ID] = e.ID
	}

	persons := func(query string) []string {
		t.Helper()
		var page fw.Page[happ.EmploymentDTO]
		h.must(h.do("GET", "/api/hr/employments?employer="+acme.ID+query, "reader", nil, &page), 200, "employments"+query)
		out := []string{}
		for _, it := range page.Items {
			out = append(out, it.Person)
		}
		return out
	}
	if got := persons(""); len(got) != 1 || got[0] != ana.ID {
		t.Fatalf("employments: %v, want only %s", got, ana.ID)
	}
	if got := persons("&includeMergedParties=true"); len(got) != 2 {
		t.Fatalf("employments with includeMergedParties: %v", got)
	}
	h.must(h.do("GET", "/api/hr/employments/"+ids[bea.ID], "reader", nil, nil), 200, "the employment of a merged party, by id")
}
