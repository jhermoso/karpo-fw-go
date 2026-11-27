package domain_test

import (
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/shipments/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

func TestCarriersWithIDs_SelectsOnlyTheGivenIDs(t *testing.T) {
	a, b := domain.NewCarrierID(), domain.NewCarrierID()
	x, err := domain.ReconstituteCarrier(a, domain.CarrierState{Company: domain.OrganizationID{UUID: fw.NewUUID()}, Code: "seur", Name: "SEUR"})
	if err != nil {
		t.Fatal(err)
	}
	if !domain.CarriersWithIDs(b, a).IsSatisfiedBy(x) {
		t.Fatal("the given id is not selected")
	}
	if domain.CarriersWithIDs(b).IsSatisfiedBy(x) {
		t.Fatal("another id is selected")
	}
	if domain.CarriersWithIDs().IsSatisfiedBy(x) {
		t.Fatal("an empty list selects something")
	}
}
