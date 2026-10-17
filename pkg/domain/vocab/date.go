package vocab

import (
	"fmt"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Date es una fecha civil de calendario sin hora ni zona horaria (fechas de nacimiento, fechas
// contables, vencimientos...): la contrapartida en Go del DateOnly de C#, que Go no tiene. Es comparable con ==.
type Date struct {
	year  int
	month time.Month
	day   int
}

const dateLayout = "2006-01-02"

// NewDate valida una fecha de calendario (rechaza 2023-02-30).
func NewDate(year int, month time.Month, day int) (Date, error) {
	t := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	if t.Year() != year || t.Month() != month || t.Day() != day || year < 1 || year > 9999 {
		return Date{}, invalid("date", "calendar", fmt.Sprintf("%04d-%02d-%02d is not a valid date", year, month, day))
	}
	return Date{year, month, day}, nil
}

// MustDate es como NewDate pero entra en pánico si hay error.
func MustDate(year int, month time.Month, day int) Date {
	d, err := NewDate(year, month, day)
	if err != nil {
		panic(err)
	}
	return d
}

// ParseDate analiza "YYYY-MM-DD".
func ParseDate(s string) (Date, error) {
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		return Date{}, invalid("date", "format", "expected YYYY-MM-DD, got "+quote(s))
	}
	return DateOf(t), nil
}

// DateOf devuelve la fecha de calendario de t en la propia zona de t.
func DateOf(t time.Time) Date {
	y, m, d := t.Date()
	return Date{y, m, d}
}

// Today devuelve la fecha actual en loc según el reloj del dominio (UTC si loc es nil).
func Today(loc *time.Location) Date {
	now := domain.Now()
	if loc != nil {
		now = now.In(loc)
	}
	return DateOf(now)
}

// Year devuelve el año.
func (d Date) Year() int { return d.year }

// Month devuelve el mes.
func (d Date) Month() time.Month { return d.month }

// Day devuelve el día del mes.
func (d Date) Day() int { return d.day }

// IsZero dice si falta la fecha.
func (d Date) IsZero() bool { return d == Date{} }

// Time devuelve la medianoche de la fecha en loc (UTC si loc es nil).
func (d Date) Time(loc *time.Location) time.Time {
	if loc == nil {
		loc = time.UTC
	}
	return time.Date(d.year, d.month, d.day, 0, 0, 0, 0, loc)
}

// BaseTime devuelve la medianoche UTC de la fecha (se usa para la aritmética de fechas).
func (d Date) BaseTime() time.Time { return d.Time(time.UTC) }

// BaseDate implementa domain.DateBacked.
func (d Date) BaseDate() (int, time.Month, int) { return d.year, d.month, d.day }

// AddDays devuelve la fecha n días después (n puede ser negativo).
func (d Date) AddDays(n int) Date { return DateOf(d.BaseTime().AddDate(0, 0, n)) }

// AddMonths devuelve la fecha n meses después, ajustando el día al final del mes de destino
// (2024-01-31 + 1 mes = 2024-02-29), como esperan las reglas contables.
func (d Date) AddMonths(n int) Date {
	first := time.Date(d.year, d.month+time.Month(n), 1, 0, 0, 0, 0, time.UTC)
	last := first.AddDate(0, 1, -1).Day()
	return Date{first.Year(), first.Month(), min(d.day, last)}
}

// Weekday devuelve el día de la semana.
func (d Date) Weekday() time.Weekday { return d.BaseTime().Weekday() }

// DaysUntil devuelve el número de días de d a o (negativo si o es anterior).
func (d Date) DaysUntil(o Date) int {
	return int(o.BaseTime().Sub(d.BaseTime()).Hours() / 24)
}

// Compare devuelve -1, 0 o 1 (para ordenar y para spec.OrderedBy).
func (d Date) Compare(o Date) int { return d.BaseTime().Compare(o.BaseTime()) }

// Before dice si d es anterior a o.
func (d Date) Before(o Date) bool { return d.Compare(o) < 0 }

// After dice si d es posterior a o.
func (d Date) After(o Date) bool { return d.Compare(o) > 0 }

// CompareDates compara dos fechas (para spec.OrderedBy).
func CompareDates(a, b Date) int { return a.Compare(b) }

// String devuelve "YYYY-MM-DD" ("" si falta).
func (d Date) String() string {
	if d.IsZero() {
		return ""
	}
	return fmt.Sprintf("%04d-%02d-%02d", d.year, d.month, d.day)
}

// MarshalText escribe "YYYY-MM-DD".
func (d Date) MarshalText() ([]byte, error) { return []byte(d.String()), nil }

// UnmarshalText analiza "YYYY-MM-DD" (vacío significa ausente).
func (d *Date) UnmarshalText(b []byte) error {
	if len(b) == 0 {
		*d = Date{}
		return nil
	}
	v, err := ParseDate(string(b))
	if err == nil {
		*d = v
	}
	return err
}
