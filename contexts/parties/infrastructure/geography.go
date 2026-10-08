package infrastructure

import (
	"context"

	"github.com/jhermoso/karpo-fw-go/contexts/geography/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// GeographyAddresses adapts the Geography context's AddressChecker to the Parties port: it is
// the anti-corruption layer between the two contexts (Parties keeps only ids of Geography).
type GeographyAddresses struct{ Checker contracts.AddressChecker }

// CheckAddress validates the postal code for the country and, when the address names a
// municipality, that the code belongs to it; it completes the postal code reference.
func (g GeographyAddresses) CheckAddress(ctx context.Context, a domain.PostalAddress) (domain.PostalAddress, error) {
	q := contracts.PostalAddressQuery{Country: a.Country.String(), PostalCode: a.PostalCode}
	if !a.Geo.Boundary.IsZero() {
		q.Boundary = a.Geo.Boundary.String()
	}
	res, err := g.Checker.CheckPostalAddress(ctx, q)
	if err != nil {
		return domain.PostalAddress{}, err
	}
	a.PostalCode = res.PostalCode
	if res.PostalCodeID != "" {
		a.Geo.PostalCode = fw.MustParseUUID(res.PostalCodeID)
	}
	if a.Geo.Boundary.IsZero() && res.Boundary != "" {
		a.Geo.Boundary = fw.MustParseUUID(res.Boundary)
	}
	return a, nil
}
