package domain_test

import (
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/assets/domain"
)

func TestAssetsWithIDs_SelectsOnlyTheGivenIDs(t *testing.T) {
	a, b := domain.NewAssetID(), domain.NewAssetID()
	x, err := domain.ReconstituteAsset(a, van())
	if err != nil {
		t.Fatal(err)
	}
	if !domain.AssetsWithIDs(b, a).IsSatisfiedBy(x) {
		t.Fatal("the given id is not selected")
	}
	if domain.AssetsWithIDs(b).IsSatisfiedBy(x) {
		t.Fatal("another id is selected")
	}
	if domain.AssetsWithIDs().IsSatisfiedBy(x) {
		t.Fatal("an empty list selects something")
	}
}
