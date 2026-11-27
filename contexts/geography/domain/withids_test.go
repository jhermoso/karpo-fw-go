package domain_test

import (
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/geography/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

func TestHolidaysWithIDs_SelectsOnlyTheGivenIDs(t *testing.T) {
	a, b := domain.NewHolidayID(), domain.NewHolidayID()
	x, err := domain.ReconstituteHoliday(a, domain.HolidayState{Boundary: domain.BoundaryID{UUID: fw.NewUUID()}, Date: vocab.MustDate(2026, time.January, 1), Name: "New year"})
	if err != nil {
		t.Fatal(err)
	}
	if !domain.HolidaysWithIDs(b, a).IsSatisfiedBy(x) {
		t.Fatal("the given id is not selected")
	}
	if domain.HolidaysWithIDs(b).IsSatisfiedBy(x) {
		t.Fatal("another id is selected")
	}
	if domain.HolidaysWithIDs().IsSatisfiedBy(x) {
		t.Fatal("an empty list selects something")
	}
}
