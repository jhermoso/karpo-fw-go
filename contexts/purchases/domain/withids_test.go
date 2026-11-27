package domain_test

import (
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/purchases/domain"
)

func TestSuppliersWithIDs_SelectsOnlyTheGivenIDs(t *testing.T) {
	a, b := domain.NewSupplierID(), domain.NewSupplierID()
	x, err := domain.ReconstituteSupplier(a, domain.SupplierState{Company: company, Supplier: supplier, Category: domain.Rent})
	if err != nil {
		t.Fatal(err)
	}
	if !domain.SuppliersWithIDs(b, a).IsSatisfiedBy(x) {
		t.Fatal("the given id is not selected")
	}
	if domain.SuppliersWithIDs(b).IsSatisfiedBy(x) {
		t.Fatal("another id is selected")
	}
	if domain.SuppliersWithIDs().IsSatisfiedBy(x) {
		t.Fatal("an empty list selects something")
	}
}
