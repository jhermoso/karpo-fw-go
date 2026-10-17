package infrastructure

import (
	"context"

	"github.com/jhermoso/karpo-fw-go/contexts/geography/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// GeographyAddresses adapta el AddressChecker del contexto Geography al puerto de Parties: es la
// capa anticorrupción entre los dos contextos (Parties sólo guarda ids de Geography).
type GeographyAddresses struct{ Checker contracts.AddressChecker }

// CheckAddress valida el código postal para el país y, cuando la dirección indica un municipio,
// que el código pertenece a él; completa la referencia al código postal.
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
