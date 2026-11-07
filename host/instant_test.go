package host_test

import (
	"testing"
	"time"

	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	"github.com/jhermoso/karpo-fw-go/host"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Un instante pasa a fecha en la zona de la empresa: las 23:30 UTC del 31 de diciembre ya son el 1
// de enero en Madrid; una empresa sin zona ni país, UTC. Hoy es lo mismo con el reloj.
func TestCompanyZones_DateOfAnInstant(t *testing.T) {
	th := newTradeHost(t, memorySwitch(t))
	ps := th.h.Parties.Service
	madrid, err := ps.RegisterOrganization.Handle(th.actx, papp.RegisterOrganization{LegalName: "Madrid SL",
		Roles: []string{pdomain.RoleInternalOrganization.String()}})
	if err != nil {
		t.Fatal(err)
	}
	id, _ := pdomain.ParsePartyID(madrid.ID)
	if _, err := ps.SetInternalOrganizationProfile.Handle(th.actx, papp.SetInternalOrganizationProfile{ID: id, TimeZone: "Europe/Madrid"}); err != nil {
		t.Fatal(err)
	}
	z := &host.CompanyZones{Geography: th.h.Geography, Profiles: th.h.Parties.CompanyTimeZones, Identities: th.h.Parties.TaxIdentities}
	at := time.Date(2026, 12, 31, 23, 30, 0, 0, time.UTC)
	for company, want := range map[fw.UUID]string{id.UUID: "2027-01-01", fw.NewUUID(): "2026-12-31"} {
		got, err := application.DateOf(th.actx, z, company, at)
		if err != nil || got.String() != want {
			t.Fatalf("%s: %s %v, want %s", company, got, err, want)
		}
	}
	got, err := application.DateOf(th.actx, nil, id.UUID, at)
	if err != nil || got.String() != "2026-12-31" {
		t.Fatalf("without zones: %s %v, want UTC", got, err)
	}
}
