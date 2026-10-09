package domain_test

import (
	"slices"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/imports/domain"
)

func TestHolidays(t *testing.T) {
	records, messages, err := domain.Holidays{}.Read([]domain.File{
		{Role: domain.RoleCalendar, Name: "2026.csv", Content: string(rune(0xFEFF)) + "Fecha;Festivo;Ámbito\n" +
			"25/12/2026;  Natividad   del Señor ;es\n" +
			"2026-05-02;Fiesta de la Comunidad de Madrid;ES-md\n" +
			"2026-11-09;Nuestra Señora de la Almudena;28 079\n" +
			"2026-12-25;Navidad;ES\n" +
			"2026-12-25;Navidad;ES-MD\n" +
			"mañana;Día raro;ES\n" +
			";Sin día;ES\n" +
			"2026-01-06;Epifanía;\n" +
			"2026-01-01;;ES\n"},
		{Role: domain.RoleCalendar, Name: "local.csv", Content: "date,name,place\n2026-05-15,\"San Isidro, labrador\",28079\n"},
		{Role: "other", Name: "x.csv", Content: "nothing"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, r := range records {
		if r.Kind != domain.KindHoliday || r.Fields["place"] != r.Scope || r.Fields["date"] != r.Key {
			t.Fatalf("record: %+v", r)
		}
		got = append(got, r.Scope+" "+r.Key+" "+r.Fields["name"])
	}
	if want := []string{"ES 2026-12-25 Natividad del Señor", "ES-MD 2026-05-02 Fiesta de la Comunidad de Madrid",
		"28079 2026-11-09 Nuestra Señora de la Almudena", "ES-MD 2026-12-25 Navidad", "28079 2026-05-15 San Isidro, labrador"}; !slices.Equal(got, want) {
		t.Fatalf("records:\n%v\nwant\n%v", got, want)
	}
	if said, want := said(messages), []string{"holiday:2026-01-01:holidays.name", "holiday:2026-01-06:holidays.place",
		"holiday:2026-12-25:holidays.duplicate", "holiday::holidays.date", "holiday:mañana:holidays.date"}; !slices.Equal(said, want) {
		t.Fatalf("messages: %v", said)
	}
	if _, _, err := (domain.Holidays{}).Read([]domain.File{{Role: domain.RoleCalendar, Name: "empty.csv", Content: ""}}); err == nil {
		t.Fatal("a file without a header is not a calendar")
	}
}
