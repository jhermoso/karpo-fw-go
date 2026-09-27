package domain_test

import (
	"errors"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/facilities/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

func types() map[domain.FacilityTypeID]domain.FacilityType {
	out := map[domain.FacilityTypeID]domain.FacilityType{}
	for _, t := range domain.WellKnownFacilityTypes() {
		out[t.ID] = t
	}
	return out
}

func isViolation(err error, code string) bool {
	var rv *fw.RuleViolationError
	return errors.As(err, &rv) && rv.Code == code
}

func TestFacility(t *testing.T) {
	ts := types()
	acme := domain.OrganizationID{UUID: fw.NewUUID()}
	madrid := domain.Location{Address: domain.Address{Line1: "Sol 1", PostalCode: "28013", Country: vocab.MustCountryCode("ES")}}
	if _, err := domain.Register(domain.NewFacilityID(), ts[domain.TypeOffice], domain.FacilityState{Owner: acme, Name: "Sol", Location: madrid}); !isViolation(err, "facilities.contact_required") {
		t.Fatalf("an office needs a phone: %v", err)
	}
	madrid.Phone = vocab.MustPhone("+34910000000")
	office, err := domain.Register(domain.NewFacilityID(), ts[domain.TypeOffice], domain.FacilityState{Owner: acme, Name: " Sol ", Location: madrid})
	if err != nil || office.Name() != "Sol" || len(office.PendingEvents()) != 1 {
		t.Fatal(err)
	}
	if err := office.Relocate(ts[domain.TypeOffice], domain.Location{Address: madrid.Address}); !isViolation(err, "facilities.contact_required") {
		t.Fatal("relocating keeps the contact requirement")
	}
	warehouse, _ := domain.Register(domain.NewFacilityID(), ts[domain.TypeWarehouse], domain.FacilityState{Owner: acme, Name: "Almacén"})
	other, _ := domain.Register(domain.NewFacilityID(), ts[domain.TypeBuilding], domain.FacilityState{Owner: domain.OrganizationID{UUID: fw.NewUUID()}, Name: "Otra"})
	if err := warehouse.MoveUnder(other); !isViolation(err, "facilities.other_organization") {
		t.Fatal("same organization only")
	}
	if err := warehouse.MoveUnder(warehouse); !isViolation(err, "facilities.hierarchy_cycle") {
		t.Fatal("not part of itself")
	}
	if err := warehouse.MoveUnder(office); err != nil || warehouse.PartOf() == nil || *warehouse.PartOf() != office.ID() {
		t.Fatal(err)
	}
	if !domain.PartsOf(office.ID()).IsSatisfiedBy(warehouse) || !domain.OwnedBy(acme).IsSatisfiedBy(office) {
		t.Fatal("specs")
	}
	if err := warehouse.SetArea(vocab.MustDecimal("-1")); !errors.Is(err, fw.ErrValidation) {
		t.Fatal("negative area")
	}
	office.Deactivate()
	if err := office.Rename("x"); !isViolation(err, "facilities.inactive") {
		t.Fatal("inactive facilities are not renamed")
	}
}
