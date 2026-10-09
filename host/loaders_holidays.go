package host

import (
	"context"
	"errors"

	"github.com/jhermoso/karpo-fw-go/contexts/geography"
	geoapp "github.com/jhermoso/karpo-fw-go/contexts/geography/application"
	geodomain "github.com/jhermoso/karpo-fw-go/contexts/geography/domain"
	impdomain "github.com/jhermoso/karpo-fw-go/contexts/imports/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// HolidayCalendar loads the holidays of a calendar into Geography, each in the boundary its code
// names. As who imports: keeping the calendar asks for its permission. A day the place already
// has is left as it is, so the calendar of a year can be loaded again when the local days are
// published.
type HolidayCalendar struct{ Geography *geography.Module }

// Kind implements imports' Loader.
func (HolidayCalendar) Kind() string { return impdomain.KindHoliday }

// EntityType implements imports' Loader.
func (HolidayCalendar) EntityType() string { return geodomain.HolidayKind }

// Find looks for the holiday of that day in that place.
func (l HolidayCalendar) Find(ctx context.Context, r impdomain.Record, _ impdomain.Refs) (string, error) {
	place, err := l.Geography.Holidays.Locate(ctx, r.Fields["place"])
	date, derr := vocab.ParseDate(r.Fields["date"])
	if err != nil || derr != nil {
		var rule *fw.RuleViolationError
		if err == nil || errors.As(err, &rule) {
			return "", nil // Apply will say what is wrong with the place
		}
		return "", err
	}
	year, err := l.Geography.Holidays.Search(ctx, geoapp.SearchHolidays{Boundary: place.ID, Year: date.Year()})
	if err != nil {
		return "", err
	}
	for _, h := range year {
		if h.Date == r.Fields["date"] {
			return h.ID, nil
		}
	}
	return "", nil
}

// Apply declares the holiday.
func (l HolidayCalendar) Apply(ctx context.Context, r impdomain.Record, existing string, _ impdomain.Refs) (string, impdomain.Outcome, error) {
	if existing != "" {
		return existing, impdomain.Unchanged, nil
	}
	place, err := l.Geography.Holidays.Locate(ctx, r.Fields["place"])
	if err != nil {
		return "", "", err
	}
	date, err := vocab.ParseDate(r.Fields["date"])
	if err != nil {
		return "", "", fw.Violation("imports.holiday_date", "the day cannot be read")
	}
	made, err := l.Geography.Holidays.Declare(ctx, geoapp.DeclareHolidays{Boundary: place.ID, Days: []geoapp.DayInput{{Date: date, Name: r.Fields["name"]}}})
	if err != nil {
		return "", "", err
	}
	if len(made) != 1 {
		return "", "", fw.Violation("imports.holiday_exists", "somebody declared the day while it was being loaded: load the file again")
	}
	return made[0].ID, impdomain.Created, nil
}

var _ impdomain.Loader = HolidayCalendar{}
