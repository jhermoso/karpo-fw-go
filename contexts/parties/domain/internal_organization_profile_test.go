package domain_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// La zona de la empresa es un código IANA conocido; un nombre de Windows no vale, y vacío la
// limpia (D-133).
func TestInternalOrganizationProfile_TimeZone(t *testing.T) {
	p := domain.InternalOrganizationProfile{Organization: domain.NewPartyID()}
	for _, code := range []string{"Atlantic/Canary", " Europe/Madrid ", "America/Argentina/Buenos_Aires", "Etc/GMT+3", "America/Port-au-Prince", "UTC"} {
		if err := p.SetTimeZone(code); err != nil || p.TimeZone != strings.TrimSpace(code) {
			t.Fatalf("%q: %q %v", code, p.TimeZone, err)
		}
	}
	for _, code := range []string{"Romance Standard Time", "W. Europe Standard Time", "Mars/Olympus", "EST5EDT", "Local",
		"Europe/../Madrid", strings.Repeat("A/", 40), "europe/madrid", "Europe/madrid", "utc", "posix/Europe/Madrid",
		"right/Europe/Madrid", "/Europe/Madrid", "Europe//Madrid"} {
		err := p.SetTimeZone(code)
		var rv *fw.RuleViolationError
		if !errors.As(err, &rv) || rv.Code != domain.ErrInvalidTimeZone || p.TimeZone != "UTC" {
			t.Fatalf("%q must be refused, keeping the zone: %v %q", code, err, p.TimeZone)
		}
	}
	for _, code := range []string{"", "   "} {
		p.TimeZone = "UTC"
		if err := p.SetTimeZone(code); err != nil || p.TimeZone != "" {
			t.Fatalf("%q clears the zone: %q %v", code, p.TimeZone, err)
		}
	}
}
