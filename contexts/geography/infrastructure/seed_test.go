package infrastructure

import (
	"context"
	"strings"
	"testing"

	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
)

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

// mojibake are the traces of UTF-8 text read as Latin-1 or Windows-1252 and encoded again
// ("Ã±" for "ñ", "â€™" for an apostrophe), and of a failed decoding.
var mojibake = []string{"Ã", "Â", "â€", "\uFFFD"}

// Every text of every seed file, whether or not the domain reads the column.
func TestSeed_NoTextHasABrokenEncoding(t *testing.T) {
	for _, name := range []string{"boundary_types", "boundaries", "associations", "postal_codes", "countries", "currencies",
		"languages", "time_zones", "street_types", "country_currencies", "country_languages", "country_time_zones"} {
		rows, err := readSeed(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rows {
			for col, v := range r {
				for _, m := range mojibake {
					if strings.Contains(v, m) {
						t.Errorf("%s %s: %s %q contains %q", name, r["id"], col, v, m)
					}
				}
			}
		}
	}
	s, err := LoadSeed()
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range s.Boundaries {
		if b.ID().String() == boundaryNameFixes[0].id && b.Name() != "Coruña, A" {
			t.Errorf("province 15: %q", b.Name())
		}
	}
}

// The seed is decoded once per process and loaded into as many stores as needed.
func TestLoadMemory_LeavesTheSeedUntouched(t *testing.T) {
	ctx := context.Background()
	for range 2 {
		if err := LoadMemory(ctx, memory.NewStore("memory")); err != nil {
			t.Fatal(err)
		}
	}
	s, _ := LoadSeed()
	if s.Boundaries[0].Version() != 0 || s.PostalCodes[0].Version() != 0 || s.Countries[0].Version() != 0 {
		t.Fatalf("loading must not mark the shared seed as persisted: %d %d %d", s.Boundaries[0].Version(), s.PostalCodes[0].Version(), s.Countries[0].Version())
	}
}
