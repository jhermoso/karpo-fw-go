package host_test

import (
	"testing"

	geoapp "github.com/jhermoso/karpo-fw-go/contexts/geography/application"
	geodomain "github.com/jhermoso/karpo-fw-go/contexts/geography/domain"
	parapp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pardomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	recapp "github.com/jhermoso/karpo-fw-go/contexts/receivables/application"
	recdomain "github.com/jhermoso/karpo-fw-go/contexts/receivables/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
)

// calendarScenario gives two companies the same payment terms, one of them an address in Madrid,
// and looks at where the due dates of Receivables fall around a weekend and two holidays.
func calendarScenario(t *testing.T, sw *hotswap.Switch) {
	th := newTradeHost(t, sw)
	h, actx := th.h, th.actx
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	towns, err := h.Geography.Service.SearchBoundaries.Handle(actx, geoapp.SearchBoundaries{Text: "madrid", Type: geodomain.TypeMunicipality.String(), Size: 50})
	must(err)
	madrid := ""
	for _, b := range towns.Items {
		if b.Name == "Madrid" {
			madrid = b.ID
		}
	}
	es, err := h.Geography.Ports.Country(actx, "ES")
	if err != nil || madrid == "" {
		t.Fatalf("Madrid and Spain: %q %v", madrid, err)
	}
	company := func(name string) string {
		t.Helper()
		p, err := h.Parties.Service.RegisterOrganization.Handle(actx, parapp.RegisterOrganization{LegalName: name, Roles: []string{pardomain.RoleInternalOrganization.String()}})
		must(err)
		return p.ID
	}
	inMadrid, nowhere := company("Acme Madrid SL"), company("Sin Sede SL")
	pid, _ := pardomain.ParsePartyID(inMadrid)
	_, err = h.Parties.Service.AddContact.Handle(actx, parapp.AddContact{PartyID: pid, Kind: "postal", Purposes: []string{"default"},
		Address: &parapp.AddressDTO{Line1: "Mayor 1", PostalCode: "28013", Locality: "Madrid", Country: "ES", GeoBoundary: madrid}})
	must(err)
	// The Monday is a holiday of the town and the Tuesday one of the country.
	_, err = h.Geography.Holidays.Declare(actx, geoapp.DeclareHolidays{Boundary: madrid, Days: []geoapp.DayInput{{Date: vocab.MustDate(2026, 12, 7), Name: "Fiesta local"}}})
	must(err)
	_, err = h.Geography.Holidays.Declare(actx, geoapp.DeclareHolidays{Boundary: es.Boundary, Days: []geoapp.DayInput{{Date: vocab.MustDate(2026, 12, 8), Name: "Inmaculada Concepción"}}})
	must(err)

	// Thirty days from Friday 6 November is Sunday 6 December.
	due := func(seller string, backward int, controlled bool) string {
		t.Helper()
		code := "D" + string(rune('A'+backward))
		if !controlled {
			code = "LIBRE"
		}
		terms, err := h.Receivables.Service.CreateTerms.Handle(actx, recapp.CreateTerms{Seller: seller, Code: code, Description: "30 días", Installments: 1,
			DaysToFirst: 30, ControlHolidays: controlled, BackwardDays: backward})
		must(err)
		id, _ := recdomain.ParseTermsID(terms.ID)
		dues, err := h.Receivables.Service.Preview.Handle(actx, recapp.PreviewSchedule{ID: id, Issued: "2026-11-06", Amount: "121.00"})
		if err != nil || len(dues) != 1 {
			t.Fatalf("preview: %+v %v", dues, err)
		}
		return dues[0].Date
	}
	for _, c := range []struct {
		what, got, want string
	}{
		{"terms that do not mind holidays", due(inMadrid, 0, false), "2026-12-06"},
		{"in Madrid, forward: past the weekend, the local holiday and the national one", due(inMadrid, 0, true), "2026-12-09"},
		{"in Madrid, three days back: the Friday before", due(inMadrid, 3, true), "2026-12-04"},
		{"in Madrid, one day back is a Saturday: forward", due(inMadrid, 1, true), "2026-12-09"},
		{"a company nobody knows where it is: weekends only", due(nowhere, 0, true), "2026-12-07"},
	} {
		if c.got != c.want {
			t.Fatalf("%s: %s, want %s", c.what, c.got, c.want)
		}
	}
}

func TestCalendar_OnMemory(t *testing.T) { calendarScenario(t, memorySwitch(t)) }
func TestCalendar_OnSQLite(t *testing.T) { calendarScenario(t, sqliteSwitch(t)) }
