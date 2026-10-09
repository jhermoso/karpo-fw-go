package host_test

import (
	"slices"
	"testing"

	geoapp "github.com/jhermoso/karpo-fw-go/contexts/geography/application"
	impapp "github.com/jhermoso/karpo-fw-go/contexts/imports/application"
	parapp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pardomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	recapp "github.com/jhermoso/karpo-fw-go/contexts/receivables/application"
	recdomain "github.com/jhermoso/karpo-fw-go/contexts/receivables/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
)

// The calendar of 2026 as the bulletins give it: the days of the country, those of the region,
// the two of the town, and two rows nobody can place.
const calendar2026 = `fecha;festivo;ámbito
2026-10-12;Fiesta Nacional de España;ES
2026-12-08;Inmaculada Concepción;ES
2026-12-25;Natividad del Señor;ES
2026-05-02;Fiesta de la Comunidad de Madrid;ES-MD
2026-12-07;Traslado de la Constitución;28
2026-05-15;San Isidro;28079
2026-11-09;Nuestra Señora de la Almudena;28079
2026-08-15;Asunción;ES-ZZ
2026-08-16;San Roque;99999
`

// holidayImportScenario loads a calendar of holidays through the host and looks at the calendar
// Geography keeps and at where a due date of Receivables falls.
func holidayImportScenario(t *testing.T, sw *hotswap.Switch) {
	th := newTradeHost(t, sw)
	h, actx := th.h, th.actx
	files := []impapp.FileDTO{{Role: "calendar", Name: "2026.csv", Content: calendar2026}}

	pre, err := h.Imports.Service.Preview.Handle(actx, impapp.RunImport{Source: "holidays", Files: files})
	if err != nil {
		t.Fatal(err)
	}
	th.expect("preview", pre.Counts, map[string]impapp.CountDTO{"holiday": {Read: 9, Created: 9}})

	run := th.run("holidays", files)
	th.expect("first run", run.Counts, map[string]impapp.CountDTO{"holiday": {Read: 9, Created: 7, Failed: 2}})
	if got, want := th.codes(run), []string{"holiday:2026-08-15:geography.unknown_place", "holiday:2026-08-16:geography.unknown_place"}; !slices.Equal(got, want) {
		t.Fatalf("what the run said: %v", th.said)
	}
	madrid, err := h.Geography.Holidays.Locate(actx, "28079")
	if err != nil || madrid.Name != "Madrid" {
		t.Fatalf("Madrid: %+v %v", madrid, err)
	}
	year, err := h.Geography.Holidays.Search(actx, geoapp.SearchHolidays{Boundary: madrid.ID, Year: 2026, Inherited: true})
	if err != nil {
		t.Fatal(err)
	}
	days := []string{}
	for _, d := range year {
		days = append(days, d.Date+" "+d.BoundaryName)
	}
	if want := []string{"2026-05-02 Comunidad de Madrid", "2026-05-15 ", "2026-10-12 Spain", "2026-11-09 ", "2026-12-07 Madrid", "2026-12-08 Spain",
		"2026-12-25 Spain"}; !slices.Equal(days, want) {
		t.Fatalf("the calendar of Madrid: %v", days)
	}

	// The same file again, with the day of a town that was published later.
	later := []impapp.FileDTO{{Role: "calendar", Name: "2026.csv", Content: calendar2026 + "2026-08-24;San Bartolomé;28005\n"}}
	again := th.run("holidays", later)
	th.expect("second run", again.Counts, map[string]impapp.CountDTO{"holiday": {Read: 10, Created: 1, Unchanged: 7, Failed: 2}})
	alcala, err := h.Geography.Holidays.Locate(actx, "28005")
	if err != nil {
		t.Fatal(err)
	}
	if own, err := h.Geography.Holidays.Search(actx, geoapp.SearchHolidays{Boundary: alcala.ID, Year: 2026}); err != nil || len(own) != 1 || own[0].Name != "San Bartolomé" {
		t.Fatalf("the day of Alcalá: %+v %v", own, err)
	}

	// And a company in Madrid collects past the Sunday, the Monday of the province and the Tuesday
	// of the country.
	p, err := h.Parties.Service.RegisterOrganization.Handle(actx, parapp.RegisterOrganization{LegalName: "Acme Madrid SL", Roles: []string{pardomain.RoleInternalOrganization.String()}})
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := pardomain.ParsePartyID(p.ID)
	if _, err := h.Parties.Service.AddContact.Handle(actx, parapp.AddContact{PartyID: pid, Kind: "postal", Purposes: []string{"default"},
		Address: &parapp.AddressDTO{Line1: "Mayor 1", PostalCode: "28013", Locality: "Madrid", Country: "ES", GeoBoundary: madrid.ID}}); err != nil {
		t.Fatal(err)
	}
	terms, err := h.Receivables.Service.CreateTerms.Handle(actx, recapp.CreateTerms{Seller: p.ID, Code: "30D", Description: "30 días", Installments: 1,
		DaysToFirst: 30, ControlHolidays: true})
	if err != nil {
		t.Fatal(err)
	}
	tid, _ := recdomain.ParseTermsID(terms.ID)
	dues, err := h.Receivables.Service.Preview.Handle(actx, recapp.PreviewSchedule{ID: tid, Issued: "2026-11-06", Amount: "121.00"})
	if err != nil || len(dues) != 1 || dues[0].Date != "2026-12-09" {
		t.Fatalf("the due date: %+v %v", dues, err)
	}
}

func TestHolidayImport_OnMemory(t *testing.T) { holidayImportScenario(t, memorySwitch(t)) }
func TestHolidayImport_OnSQLite(t *testing.T) { holidayImportScenario(t, sqliteSwitch(t)) }
