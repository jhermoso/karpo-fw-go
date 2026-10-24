package treasury_test

import (
	"context"
	"testing"

	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/treasury"
	tapp "github.com/jhermoso/karpo-fw-go/contexts/treasury/application"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// mergedParties hace de Parties: dice qué parties están fusionadas en otra.
type mergedParties struct{ ids []string }

func (m *mergedParties) MergedParties(context.Context) ([]string, error) { return m.ids, nil }

// El listado de mandatos deja fuera por defecto los de un deudor fusionado en otra party y los
// trae con includeMergedParties (docs/DECISIONES-COMUNES.md, D-64 y D-100).
func TestMandatesLeaveOutMergedParties(t *testing.T) {
	merged := &mergedParties{}
	h := compose(t, treasury.WithMergedParties(merged))
	ctx := h.adminCtx
	acme, err := h.parties.Service.RegisterOrganization.Handle(ctx, papp.RegisterOrganization{LegalName: "Acme fusiones",
		Roles: []string{pdomain.RoleInternalOrganization.String()}})
	h.ok(err)
	h.grant("clerk", acme.ID)
	ana, bea := fw.NewUUID().String(), fw.NewUUID().String()
	merged.ids = []string{bea}

	for ref, debtor := range map[string]string{"MAND-ANA": ana, "MAND-BEA": bea} {
		h.must(h.do("POST", "/api/treasury/mandates", "clerk", map[string]any{"creditor": acme.ID, "debtor": debtor, "iban": "ES6621000418401234567891",
			"reference": ref, "scheme": "CORE", "signed": "2026-01-10"}, nil), 201, "mandate "+ref)
	}

	var mandates []tapp.MandateDTO
	path := "/api/treasury/mandates?creditor=" + acme.ID
	h.must(h.do("GET", path, "clerk", nil, &mandates), 200, "mandates")
	if len(mandates) != 1 || mandates[0].Debtor != ana {
		t.Fatalf("mandates: %+v, want only %s", mandates, ana)
	}
	h.must(h.do("GET", path+"&includeMergedParties=true", "clerk", nil, &mandates), 200, "mandates")
	if len(mandates) != 2 {
		t.Fatalf("mandates with includeMergedParties: %+v", mandates)
	}
	for include, want := range map[bool]int{false: 1, true: 2} {
		ms, err := h.treasury.Service.SearchMandates.Handle(ctx, tapp.SearchMandates{Creditor: acme.ID, IncludeMergedParties: include})
		h.ok(err)
		if len(ms) != want {
			t.Fatalf("mandates (includeMergedParties %v): %+v", include, ms)
		}
	}
}
