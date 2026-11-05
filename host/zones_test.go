package host_test

import (
	"testing"

	"github.com/jhermoso/karpo-fw-go/host"
)

// La zona de una empresa es la principal de su país en Geography; un país que no conoce, UTC.
func TestCountryZones(t *testing.T) {
	th := newTradeHost(t, memorySwitch(t))
	z := &host.CountryZones{Geography: th.h.Geography}
	for country, want := range map[string]string{"ES": "Europe/Madrid", "ZZ": "UTC"} {
		for range 2 { // la segunda, de memoria
			loc, err := z.Location(th.actx, country)
			if err != nil || loc.String() != want {
				t.Fatalf("%s: %v %v, want %s", country, loc, err, want)
			}
		}
	}
}
