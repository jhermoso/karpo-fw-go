package parties_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/geography"
	gapp "github.com/jhermoso/karpo-fw-go/contexts/geography/application"
	gdomain "github.com/jhermoso/karpo-fw-go/contexts/geography/domain"
	ginfra "github.com/jhermoso/karpo-fw-go/contexts/geography/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/parties"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	pinfra "github.com/jhermoso/karpo-fw-go/contexts/parties/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
)

// TestParties_AddressesCheckedByGeography composes two contexts on one backend: Parties
// validates postal addresses through its own port, implemented by an adapter over the
// Geography contracts (decision of the context map: Parties keeps only Geography ids).
func TestParties_AddressesCheckedByGeography(t *testing.T) {
	ctx := context.Background()
	store := memory.NewStore("host")
	if err := ginfra.LoadMemory(ctx, store); err != nil {
		t.Fatal(err)
	}
	sw := hotswap.New(store)
	geo := geography.Compose(sw)
	mod := parties.Compose(sw, nil, parties.WithAddressChecker(pinfra.GeographyAddresses{Checker: geo.Ports}))

	ac, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "admin", Kind: authz.Service,
		Permissions: []authz.Permission{authz.Wildcard, gapp.PermBoundaryRead}})
	ac.GlobalAdmin = true
	ctx = authz.WithContext(ctx, ac)

	towns, err := geo.Service.SearchBoundaries.Handle(ctx, gapp.SearchBoundaries{Text: "madrid", Type: gdomain.TypeMunicipality.String()})
	if err != nil || towns.Total == 0 {
		t.Fatal(err)
	}
	var madrid string
	for _, b := range towns.Items {
		if b.Name == "Madrid" {
			madrid = b.ID
		}
	}
	ana, err := mod.Service.RegisterPerson.Handle(ctx, papp.RegisterPerson{GivenName: "Ana", FirstSurname: "García"})
	if err != nil {
		t.Fatal(err)
	}
	id, _ := domain.ParsePartyID(ana.ID)
	add := func(code, boundary string) (papp.PartyDTO, error) {
		return mod.Service.AddContact.Handle(ctx, papp.AddContact{PartyID: id, Kind: "postal", Purposes: []string{"home"},
			Address: &papp.AddressDTO{Line1: "Mayor 1", PostalCode: code, Locality: "Madrid", Country: "ES", GeoBoundary: boundary}})
	}
	if _, err := add("08001", madrid); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("08001 is Barcelona: %v", err)
	}
	if _, err := add("2801", ""); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("Spanish postal codes have five digits: %v", err)
	}
	got, err := add("28013", madrid)
	if err != nil {
		t.Fatal(err)
	}
	a := got.Contacts[0].Address
	if a.GeoBoundary != madrid || a.GeoPostalCode == "" {
		t.Fatalf("the address keeps the Geography ids: %+v", a)
	}
}
