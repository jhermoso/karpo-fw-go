package domain_test

import (
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/products/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

func TestProductsWithIDs_SelectsOnlyTheGivenIDs(t *testing.T) {
	a, b := domain.NewProductID(), domain.NewProductID()
	x, err := domain.ReconstituteProduct(a, domain.ProductState{Company: domain.OrganizationID{UUID: fw.NewUUID()}, SKU: "T-1",
		Details: domain.Details{Name: "Tornillo", Kind: domain.Good, UoM: "ea"}})
	if err != nil {
		t.Fatal(err)
	}
	if !domain.ProductsWithIDs(b, a).IsSatisfiedBy(x) {
		t.Fatal("the given id is not selected")
	}
	if domain.ProductsWithIDs(b).IsSatisfiedBy(x) {
		t.Fatal("another id is selected")
	}
	if domain.ProductsWithIDs().IsSatisfiedBy(x) {
		t.Fatal("an empty list selects something")
	}
}
