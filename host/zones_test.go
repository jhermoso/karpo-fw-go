package host_test

import (
	"errors"
	"testing"
	"time"

	bilapp "github.com/jhermoso/karpo-fw-go/contexts/billing/application"
	bildomain "github.com/jhermoso/karpo-fw-go/contexts/billing/domain"
	fisapp "github.com/jhermoso/karpo-fw-go/contexts/fiscal/application"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	"github.com/jhermoso/karpo-fw-go/host"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/time/fake"
)

// La zona de una empresa es la que guarda en su perfil; si no guarda ninguna, la principal de su
// país en Geography; un país que no conoce, o ninguno, UTC (D-133).
func TestCompanyZones(t *testing.T) {
	th := newTradeHost(t, memorySwitch(t))
	ps := th.h.Parties.Service
	canary, err := ps.RegisterOrganization.Handle(th.actx, papp.RegisterOrganization{LegalName: "Canarias SL",
		Roles: []string{pdomain.RoleInternalOrganization.String()}})
	if err != nil {
		t.Fatal(err)
	}
	id, _ := pdomain.ParsePartyID(canary.ID)
	if _, err := ps.SetInternalOrganizationProfile.Handle(th.actx, papp.SetInternalOrganizationProfile{ID: id, TimeZone: "Atlantic/Canary"}); err != nil {
		t.Fatal(err)
	}
	z := &host.CompanyZones{Geography: th.h.Geography, Profiles: th.h.Parties.CompanyTimeZones}
	other := pdomain.NewPartyID().String()
	for _, c := range []struct{ company, country, want string }{
		{canary.ID, "ES", "Atlantic/Canary"}, {other, "ES", "Europe/Madrid"}, {other, "ZZ", "UTC"}, {"", "", "UTC"},
	} {
		for range 2 { // la segunda, el país de memoria
			loc, err := z.Location(th.actx, c.company, c.country)
			if err != nil || loc.String() != c.want {
				t.Fatalf("%s %s: %v %v, want %s", c.company, c.country, loc, err, c.want)
			}
		}
	}
}

// De punta a punta, con las zonas del anfitrión: una empresa española de Canarias factura sin
// fecha el 31 de diciembre a las 23:30, hora canaria (las 00:30 del 1 de enero en Madrid). Con la
// zona de Madrid la factura sería del año siguiente y la serie de 2026 la rechaza; en cuanto guarda
// Atlantic/Canary, la siguiente emisión sale con fecha del 31, sin esperar a ninguna caché.
func TestCompanyZones_IssueDateInTheCanaries(t *testing.T) {
	defer fw.SetClock(fake.New(time.Date(2026, 12, 31, 23, 30, 0, 0, time.UTC)))() // en invierno, Canarias está en UTC
	th := newTradeHost(t, memorySwitch(t))
	h, actx := th.h, th.actx
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	organization := func(name, nif string, roles []string, affiliation *papp.NewAffiliation) string {
		t.Helper()
		p, err := h.Parties.Service.RegisterOrganization.Handle(actx, papp.RegisterOrganization{LegalName: name, Roles: roles, Affiliation: affiliation})
		must(err)
		pid, _ := pdomain.ParsePartyID(p.ID)
		_, err = h.Parties.Service.AddIdentification.Handle(actx, papp.AddIdentification{PartyID: pid,
			DocumentType: "c0000000-0004-0000-0000-000000000003", Country: "ES", Number: nif, Primary: true})
		must(err)
		return p.ID
	}
	canary := organization("Canarias SL", "B12345674", []string{pdomain.RoleInternalOrganization.String()}, nil)
	customer := organization("Cliente SL", "A08015497", []string{pdomain.RoleCustomer.String()},
		&papp.NewAffiliation{Organization: canary, RelationshipType: pdomain.RelCustomer.String()})
	_, err := h.Fiscal.Service.CreateRate.Handle(actx, fisapp.CreateRate{Type: "vat", Territory: "common", Code: "G21", Description: "General",
		Rate: "21", From: vocab.MustDate(2012, 9, 1)})
	must(err)
	_, err = h.Fiscal.Service.RegisterTaxpayer.Handle(actx, fisapp.RegisterTaxpayer{Organization: canary, TermsInput: fisapp.TermsInput{Territory: "common", FiscalYearStartMonth: 1}})
	must(err)
	series, err := h.Billing.Service.OpenSeries.Handle(actx, bilapp.OpenSeries{Seller: canary, Code: "FC", Year: 2026})
	must(err)
	inv, err := h.Billing.Service.DraftInvoice.Handle(actx, bilapp.DraftInvoice{Seller: canary, Customer: customer,
		DetailsInput: bilapp.DetailsInput{Description: "Cuota"}})
	must(err)
	id, _ := bildomain.ParseInvoiceID(inv.ID)
	_, err = h.Billing.Service.AddLine.Handle(actx, bilapp.AddLine{ID: id, Description: "Cuota", Quantity: "1", UnitPrice: "10", TaxCode: "G21"})
	must(err)

	var rv *fw.RuleViolationError
	if _, err := h.Billing.Service.Issue.Handle(actx, bilapp.IssueInvoice{ID: id, Series: series.ID}); !errors.As(err, &rv) || rv.Code != "billing.series_other_year" {
		t.Fatalf("without a stored zone it is already 2027 in Madrid: %v", err)
	}
	canaryID, _ := pdomain.ParsePartyID(canary)
	_, err = h.Parties.Service.SetInternalOrganizationProfile.Handle(actx, papp.SetInternalOrganizationProfile{ID: canaryID, TimeZone: "Atlantic/Canary"})
	must(err)
	issued, err := h.Billing.Service.Issue.Handle(actx, bilapp.IssueInvoice{ID: id, Series: series.ID})
	must(err)
	if issued.IssueDate != "2026-12-31" {
		t.Fatalf("issue date: %s, want 2026-12-31", issued.IssueDate)
	}
}
