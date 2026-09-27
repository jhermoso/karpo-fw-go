package vocab

import (
	"fmt"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Date is a civil calendar date without time or time zone (birth dates, accounting dates, due
// dates...): the Go counterpart of C# DateOnly, which Go lacks. It is comparable with ==.
type Date struct {
	year  int
	month time.Month
	day   int
}

const dateLayout = "2006-01-02"

// NewDate validates a calendar date (rejects 2023-02-30).
func NewDate(year int, month time.Month, day int) (Date, error) {
	t := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	if t.Year() != year || t.Month() != month || t.Day() != day || year < 1 || year > 9999 {
		return Date{}, invalid("date", "calendar", fmt.Sprintf("%04d-%02d-%02d is not a valid date", year, month, day))
	}
	return Date{year, month, day}, nil
}

// MustDate is like NewDate but panics on error.
func MustDate(year int, month time.Month, day int) Date {
	d, err := NewDate(year, month, day)
	if err != nil {
		panic(err)
	}
	return d
}

// ParseDate parses "YYYY-MM-DD".
func ParseDate(s string) (Date, error) {
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		return Date{}, invalid("date", "format", "expected YYYY-MM-DD, got "+quote(s))
	}
	return DateOf(t), nil
}

// DateOf returns the calendar date of t in t's own location.
func DateOf(t time.Time) Date {
	y, m, d := t.Date()
	return Date{y, m, d}
}

// Today returns the current date in loc according to the domain clock (UTC when loc is nil).
func Today(loc *time.Location) Date {
	now := domain.Now()
	if loc != nil {
		now = now.In(loc)
	}
	return DateOf(now)
}

// Year returns the year.
func (d Date) Year() int { return d.year }

// Month returns the month.
func (d Date) Month() time.Month { return d.month }

// Day returns the day of the month.
func (d Date) Day() int { return d.day }

// IsZero reports whether the date is absent.
func (d Date) IsZero() bool { return d == Date{} }

// Time returns midnight of the date in loc (UTC when loc is nil).
func (d Date) Time(loc *time.Location) time.Time {
	if loc == nil {
		loc = time.UTC
	}
	return time.Date(d.year, d.month, d.day, 0, 0, 0, 0, loc)
}

// BaseTime returns midnight UTC of the date (used for date arithmetic).
func (d Date) BaseTime() time.Time { return d.Time(time.UTC) }

// BaseDate implements domain.DateBacked.
func (d Date) BaseDate() (int, time.Month, int) { return d.year, d.month, d.day }

// AddDays returns the date n days later (n may be negative).
func (d Date) AddDays(n int) Date { return DateOf(d.BaseTime().AddDate(0, 0, n)) }

// AddMonths returns the date n months later, clamping the day to the end of the target month
// (2024-01-31 + 1 month = 2024-02-29), as accounting rules expect.
func (d Date) AddMonths(n int) Date {
	first := time.Date(d.year, d.month+time.Month(n), 1, 0, 0, 0, 0, time.UTC)
	last := first.AddDate(0, 1, -1).Day()
	return Date{first.Year(), first.Month(), min(d.day, last)}
}

// Weekday returns the day of the week.
func (d Date) Weekday() time.Weekday { return d.BaseTime().Weekday() }

// DaysUntil returns the number of days from d to o (negative when o is earlier).
func (d Date) DaysUntil(o Date) int {
	return int(o.BaseTime().Sub(d.BaseTime()).Hours() / 24)
}

// Compare returns -1, 0 or 1 (for sorting and spec.OrderedBy).
func (d Date) Compare(o Date) int { return d.BaseTime().Compare(o.BaseTime()) }

// Before reports whether d is earlier than o.
func (d Date) Before(o Date) bool { return d.Compare(o) < 0 }

// After reports whether d is later than o.
func (d Date) After(o Date) bool { return d.Compare(o) > 0 }

// CompareDates compares two dates (for spec.OrderedBy).
func CompareDates(a, b Date) int { return a.Compare(b) }

// String returns "YYYY-MM-DD" ("" when absent).
func (d Date) String() string {
	if d.IsZero() {
		return ""
	}
	return fmt.Sprintf("%04d-%02d-%02d", d.year, d.month, d.day)
}

// MarshalText renders "YYYY-MM-DD".
func (d Date) MarshalText() ([]byte, error) { return []byte(d.String()), nil }

// UnmarshalText parses "YYYY-MM-DD" (empty means absent).
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
