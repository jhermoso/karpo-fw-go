package geography_test

import (
	"context"
	"errors"

	gapp "github.com/jhermoso/karpo-fw-go/contexts/geography/application"
	"github.com/jhermoso/karpo-fw-go/contexts/geography/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// holidays lleva por HTTP el calendario de España y de Madrid y pregunta por ellos al puerto; lo
// mismo en todos los backends.
func (e *env) holidays() {
	t := e.t
	ctx := context.Background()
	var page fw.Page[gapp.BoundaryDTO]
	e.must(e.do("GET", "/api/geography/boundaries?q=madrid&type="+domain.TypeMunicipality.String(), e.token, nil, &page), 200, "search")
	madrid := ""
	for _, b := range page.Items {
		if b.Name == "Madrid" {
			madrid = b.ID
		}
	}
	es, err := e.mod.Ports.Country(ctx, "ES")
	if err != nil || madrid == "" || es.Boundary == "" {
		t.Fatalf("Madrid and Spain: %q %+v %v", madrid, es, err)
	}
	spain := es.Boundary
	day := func(date, name string) map[string]string { return map[string]string{"date": date, "name": name} }
	declare := func(boundary string, days ...map[string]string) map[string]any {
		return map[string]any{"boundary": boundary, "days": days}
	}

	// Cada cosa pide su permiso.
	e.must(e.do("GET", "/api/geography/holidays?boundary="+spain+"&year=2026", e.token, nil, nil), 403, "reading without the permission")
	e.must(e.do("POST", "/api/geography/holidays", e.reader, declare(spain, day("2026-12-25", "Navidad")), nil), 403, "declaring without the permission")

	// Los festivos del país, enviados dos veces: la segunda sólo se declara lo nuevo.
	var made []gapp.HolidayDTO
	e.must(e.do("POST", "/api/geography/holidays", e.keeper, declare(spain, day("2026-12-25", "  Natividad   del Señor "), day("2026-10-12", "Fiesta Nacional"),
		day("2026-12-25", "Navidad otra vez")), &made), 201, "the holidays of Spain")
	if len(made) != 2 || made[0].Name != "Natividad del Señor" || made[0].Date != "2026-12-25" || made[0].Boundary != spain {
		t.Fatalf("declared: %+v", made)
	}
	e.must(e.do("POST", "/api/geography/holidays", e.keeper, declare(spain, day("2026-12-25", "Navidad"), day("2026-12-08", "Inmaculada Concepción")), &made), 201, "again")
	if len(made) != 1 || made[0].Date != "2026-12-08" {
		t.Fatalf("only what is new: %+v", made)
	}
	// Y los del pueblo.
	e.must(e.do("POST", "/api/geography/holidays", e.keeper, declare(madrid, day("2026-11-09", "Nuestra Señora de la Almudena"), day("2026-05-15", "San Isidro")), &made), 201, "Madrid")
	almudena := made[0].ID

	// Lo que no es un festivo que declarar.
	e.must(e.do("POST", "/api/geography/holidays", e.keeper, declare(spain), nil), 400, "no days")
	e.must(e.do("POST", "/api/geography/holidays", e.keeper, declare(spain, day("2026-01-06", " ")), nil), 400, "no name")
	e.must(e.do("POST", "/api/geography/holidays", e.keeper, declare("nowhere", day("2026-01-06", "Reyes")), nil), 400, "no boundary")
	e.must(e.do("POST", "/api/geography/holidays", e.keeper, declare(fw.NewUUID().String(), day("2026-01-06", "Reyes")), nil), 404, "a boundary nobody knows")
	e.must(e.do("GET", "/api/geography/holidays?boundary="+spain, e.keeper, nil, nil), 400, "no year")

	// El calendario del pueblo: sus propios días, y con los que hereda, en el orden del año.
	var own, all []gapp.HolidayDTO
	e.must(e.do("GET", "/api/geography/holidays?boundary="+madrid+"&year=2026", e.keeper, nil, &own), 200, "own")
	e.must(e.do("GET", "/api/geography/holidays?boundary="+madrid+"&year=2026&inherited=true", e.keeper, nil, &all), 200, "inherited")
	dates := []string{}
	for _, h := range all {
		dates = append(dates, h.Date)
	}
	if len(own) != 2 || own[0].Date != "2026-05-15" || len(all) != 5 || dates[0] != "2026-05-15" || dates[1] != "2026-10-12" || dates[2] != "2026-11-09" ||
		dates[4] != "2026-12-25" || all[1].BoundaryName == "" || all[1].Boundary != spain {
		t.Fatalf("calendar of Madrid: %+v / %+v", own, all)
	}
	var next []gapp.HolidayDTO
	e.must(e.do("GET", "/api/geography/holidays?boundary="+madrid+"&year=2027&inherited=true", e.keeper, nil, &next), 200, "another year")
	if len(next) != 0 {
		t.Fatalf("2027: %+v", next)
	}

	// Lo que preguntan otros contextos: un día es festivo donde se declara y en lo que hay dentro.
	is := func(boundary, date string) bool {
		t.Helper()
		ok, err := e.mod.Holidays.IsHoliday(ctx, boundary, date)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if !is(madrid, "2026-11-09") || !is(madrid, "2026-12-25") || !is(spain, "2026-12-25") || is(spain, "2026-11-09") || is(madrid, "2026-11-10") ||
		is(fw.NewUUID().String(), "2026-12-25") || is("", "2026-12-25") {
		t.Fatal("holidays by place")
	}
	if _, err := e.mod.Holidays.IsHoliday(ctx, madrid, "25/12/2026"); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("a day written otherwise: %v", err)
	}

	// Un lugar por el código que le dan los calendarios oficiales.
	for code, name := range map[string]string{"es": "Spain", "ES-MD": "Comunidad de Madrid", "28": "Madrid", " 28079 ": "Madrid", "ES-CT": "Cataluña"} {
		if ref, err := e.mod.Holidays.Locate(ctx, code); err != nil || ref.Name != name || ref.ID == "" {
			t.Fatalf("%q: %+v %v", code, ref, err)
		}
	}
	if town, _ := e.mod.Holidays.Locate(ctx, "28079"); town.ID != madrid {
		t.Fatalf("28079 is Madrid: %+v", town)
	}
	var rule *fw.RuleViolationError
	for _, code := range []string{"", "XX", "ES-ZZ", "FR-MD", "99", "99999", "2807", "Madrid"} {
		if _, err := e.mod.Holidays.Locate(ctx, code); !errors.As(err, &rule) || rule.Code != "geography.unknown_place" {
			t.Fatalf("%q is nowhere: %v", code, err)
		}
	}

	// Quitar uno declarado por error.
	e.must(e.do("DELETE", "/api/geography/holidays/"+almudena, e.reader, nil, nil), 403, "removing without the permission")
	e.must(e.do("DELETE", "/api/geography/holidays/"+almudena, e.keeper, nil, nil), 200, "removing")
	e.must(e.do("DELETE", "/api/geography/holidays/"+almudena, e.keeper, nil, nil), 404, "removing twice")
	e.must(e.do("DELETE", "/api/geography/holidays/x", e.keeper, nil, nil), 400, "removing nothing")
	if is(madrid, "2026-11-09") {
		t.Fatal("removed")
	}

	// Una isla tiene días propios: sus pueblos los celebran, y los de otra isla no.
	var island, towns, others fw.Page[gapp.BoundaryDTO]
	e.must(e.do("GET", "/api/geography/boundaries?q=la%20palma&level=3", e.token, nil, &island), 200, "an island")
	if island.Total != 1 || island.Items[0].NUTS != "ES707" {
		t.Fatalf("La Palma: %+v", island.Items)
	}
	e.must(e.do("GET", "/api/geography/boundaries?parent="+island.Items[0].ID+"&size=50", e.token, nil, &towns), 200, "the towns of an island")
	e.must(e.do("GET", "/api/geography/boundaries?q=valverde&level=local", e.token, nil, &others), 200, "a town of El Hierro")
	hierro := ""
	for _, b := range others.Items {
		if b.INE == "38048" {
			hierro = b.ID
		}
	}
	if towns.Total != 14 || towns.Items[0].Level != "local" || hierro == "" {
		t.Fatalf("the 14 towns of La Palma and Valverde: %d %+v", towns.Total, others.Items)
	}
	e.must(e.do("POST", "/api/geography/holidays", e.keeper, declare(island.Items[0].ID, day("2026-08-05", "Nuestra Señora de las Nieves")), &made), 201, "the day of an island")
	if !is(towns.Items[0].ID, "2026-08-05") || is(hierro, "2026-08-05") || is(madrid, "2026-08-05") {
		t.Fatal("the day of La Palma is kept in La Palma and nowhere else")
	}
}
