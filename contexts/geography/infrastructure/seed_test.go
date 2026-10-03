package infrastructure

import "testing"

func TestSeed(t *testing.T) {
	s, err := LoadSeed()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Boundaries) != 8497 || len(s.PostalCodes) != 14608 || len(s.Countries) != 248 || len(s.BoundaryTypes) != 27 ||
		len(s.Currencies) != 182 || len(s.Languages) != 183 || len(s.TimeZones) != 5 || len(s.StreetTypes) != 40 {
		t.Fatalf("seed sizes: %d %d %d %d", len(s.Boundaries), len(s.PostalCodes), len(s.Countries), len(s.BoundaryTypes))
	}
	links := 0
	for _, b := range s.Boundaries {
		links += len(b.Links())
	}
	if links != 8870 {
		t.Fatalf("links: %d", links)
	}
}
