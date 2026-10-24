package payroll_test

import (
	"context"
	"testing"

	fapp "github.com/jhermoso/karpo-fw-go/contexts/facilities/application"
	fdomain "github.com/jhermoso/karpo-fw-go/contexts/facilities/domain"
	happ "github.com/jhermoso/karpo-fw-go/contexts/hr/application"
	hdomain "github.com/jhermoso/karpo-fw-go/contexts/hr/domain"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/payroll"
	yapp "github.com/jhermoso/karpo-fw-go/contexts/payroll/application"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// mergedParties hace de Parties: dice qué parties están fusionadas en otra.
type mergedParties struct{ ids []string }

func (m *mergedParties) MergedParties(context.Context) ([]string, error) { return m.ids, nil }

// Los listados de nóminas y de perfiles dejan fuera por defecto los de una persona fusionada en
// otra party, y los traen con includeMergedParties; el registro pedido por su identidad se sigue
// encontrando (docs/DECISIONES-COMUNES.md, D-100).
func TestListingsLeaveOutMergedParties(t *testing.T) {
	merged := &mergedParties{}
	h := compose(t, payroll.WithMergedParties(merged))
	ctx := h.adminCtx
	ok := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	acme, err := h.parties.Service.RegisterOrganization.Handle(ctx, papp.RegisterOrganization{LegalName: "Acme fusiones",
		Roles: []string{pdomain.RoleInternalOrganization.String()}})
	ok(err)
	h.grant("clerk", acme.ID)
	tower, err := h.fac.Service.Register.Handle(ctx, fapp.RegisterFacility{Organization: acme.ID, Type: fdomain.TypeBuilding.String(), Name: "Torre fusiones"})
	ok(err)
	wc, err := h.hr.Service.OpenWorkCenter.Handle(ctx, happ.OpenWorkCenter{Employer: acme.ID, Facility: tower.ID, Code: "28/1/01", Opened: vocab.MustDate(2020, 1, 1)})
	ok(err)

	// Ana y Bea, empleadas de Acme con su perfil y la nómina del mes pasado; Bea está fusionada.
	today := vocab.DateOf(fw.Now())
	end := vocab.MustDate(today.Year(), today.Month(), 1).AddDays(-1)
	start := vocab.MustDate(end.Year(), end.Month(), 1)
	profiles, payslips := map[string]string{}, map[string]string{}
	var ana, bea string
	for i, name := range []string{"Ana", "Bea"} {
		p, err := h.parties.Service.RegisterPerson.Handle(ctx, papp.RegisterPerson{GivenName: name, FirstSurname: "Fusiones",
			Affiliation: &papp.NewAffiliation{Organization: acme.ID, RelationshipType: pdomain.RelEmployment.String()}})
		ok(err)
		job, err := h.hr.Service.Hire.Handle(ctx, happ.Hire{Person: p.ID, Employer: acme.ID, Number: "E-" + string(rune('1'+i)), Hired: today.AddDays(-200)})
		ok(err)
		jobID, _ := hdomain.ParseEmploymentID(job.ID)
		_, err = h.hr.Service.AddContract.Handle(ctx, happ.AddContract{ID: jobID, TypeCode: "100", Start: today.AddDays(-200),
			Agreement: "b3800000-0003-0000-0000-000000000005", WorkCenter: wc.ID, Primary: true})
		ok(err)
		var prof yapp.ProfileDTO
		h.must(h.do("POST", "/api/payroll/profiles", "clerk", map[string]any{"person": p.ID, "employer": acme.ID, "contributionGroup": 5,
			"incomeTaxRate": "15", "salary": map[string]any{"amount": "2000", "periodicity": "monthly", "paymentsPerYear": 14}}, &prof), 201, "profile")
		profiles[p.ID] = prof.ID
		var slip yapp.PayslipDTO
		h.must(h.do("POST", "/api/payroll/payslips", "clerk", map[string]any{"person": p.ID, "employer": acme.ID, "start": start.String(),
			"end": end.String()}, &slip), 201, "payslip")
		payslips[p.ID] = slip.ID
		if name == "Ana" {
			ana = p.ID
		} else {
			bea = p.ID
		}
	}
	merged.ids = []string{bea}

	persons := func(listing, query string) []string {
		t.Helper()
		var page fw.Page[struct {
			Person string `json:"person"`
		}]
		h.must(h.do("GET", "/api/payroll/"+listing+"?employer="+acme.ID+query, "clerk", nil, &page), 200, listing+query)
		out := []string{}
		for _, it := range page.Items {
			out = append(out, it.Person)
		}
		return out
	}
	for _, listing := range []string{"payslips", "profiles"} {
		if got := persons(listing, ""); len(got) != 1 || got[0] != ana {
			t.Fatalf("%s: %v, want only %s", listing, got, ana)
		}
		if got := persons(listing, "&includeMergedParties=true"); len(got) != 2 {
			t.Fatalf("%s with includeMergedParties: %v", listing, got)
		}
	}
	h.must(h.do("GET", "/api/payroll/payslips/"+payslips[bea], "clerk", nil, nil), 200, "the payslip of a merged party, by id")
	h.must(h.do("GET", "/api/payroll/profiles/"+profiles[bea], "clerk", nil, nil), 200, "the profile of a merged party, by id")
}
