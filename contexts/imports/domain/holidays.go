package domain

import (
	"strings"
)

// KindHoliday is a day nobody works in a place.
const KindHoliday = "holiday"

// RoleCalendar is the file of a calendar of holidays.
const RoleCalendar = "calendar"

// Holidays reads a calendar of holidays: the days of a year, each with the place it is a holiday
// in. No official body publishes one file for every place (the State publishes the national and
// regional days, each region its own, each town its two), so this is the neutral file they are
// copied to, separated by semicolons or commas:
//
//	date;name;place
//	2026-12-25;Natividad del Señor;ES
//	2026-05-02;Fiesta de la Comunidad de Madrid;ES-MD
//	2026-11-09;Nuestra Señora de la Almudena;28079
//
// The place is a country (ES), a region (ES-MD), a province (28) or a municipality (28079), by
// its official code. The columns may be named in Spanish too: fecha, nombre or festivo, lugar or
// ámbito.
type Holidays struct{}

// Key implements Source.
func (Holidays) Key() string { return "holidays" }

// Files implements Source.
func (Holidays) Files() []FileSpec {
	return []FileSpec{{Role: RoleCalendar, Required: true, About: "date;name;place"}}
}

// Kinds implements Source.
func (Holidays) Kinds() []string { return []string{KindHoliday} }

// Read implements Source.
func (Holidays) Read(files []File) ([]Record, []Message, error) {
	p := &run{}
	seen := map[string]bool{}
	for _, f := range files {
		if f.Role != RoleCalendar {
			continue
		}
		delimiter := ','
		if header, _, _ := strings.Cut(f.Content, "\n"); strings.Contains(header, ";") {
			delimiter = ';'
		}
		t, err := ParseTable(f, delimiter)
		if err != nil {
			return nil, nil, err
		}
		for _, r := range t.Rows {
			written := r.Get("date", "fecha", "día")
			place := strings.ToUpper(strings.Join(strings.Fields(r.Get("place", "lugar", "ámbito", "code", "código")), ""))
			name := strings.Join(strings.Fields(r.Get("name", "nombre", "festivo", "festividad")), " ")
			date, ok := Day(written)
			switch {
			case written == "" || !ok:
				p.reject(t, r, KindHoliday, written, "holidays.date", "the day cannot be read")
			case place == "":
				p.reject(t, r, KindHoliday, date, "holidays.place", "the row does not say where the day is a holiday")
			case name == "":
				p.reject(t, r, KindHoliday, date, "holidays.name", "the holiday has no name")
			case seen[place+"|"+date]:
				p.warn(t, r, KindHoliday, date, "holidays.duplicate", "the day appears twice for "+place+"; the second is ignored")
			default:
				seen[place+"|"+date] = true
				p.add(t, r, KindHoliday, place, date, map[string]string{"date": date, "name": name, "place": place})
			}
		}
	}
	return p.records, p.messages, nil
}
