// Package contracts is what other bounded contexts may depend on: the Open Host Service of the
// Geography and reference data context. Boundaries, postal codes and countries are referenced by
// identity (the same GUIDs as Karpo) or by ISO code.
package contracts

import "context"

// BoundaryRef is what other contexts need to show a boundary.
type BoundaryRef struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Active bool   `json:"active"`
}

// MaxBatch is the maximum number of ids per call.
const MaxBatch = 900

// Gazetteer resolves and walks boundaries.
type Gazetteer interface {
	// Resolve returns the boundaries that exist among ids.
	Resolve(ctx context.Context, ids []string) (map[string]BoundaryRef, error)
	// Ancestors returns the containing boundaries of id, nearest first (municipality ->
	// province -> autonomous community -> country -> subcontinent -> continent).
	Ancestors(ctx context.Context, id string) ([]BoundaryRef, error)
	// Descendants returns ids and every boundary contained in them (a search "in Madrid"
	// becomes "in any of these municipalities").
	Descendants(ctx context.Context, ids []string) ([]string, error)
	// CountryOf returns the ISO 3166 alpha-2 code of the country containing each boundary.
	CountryOf(ctx context.Context, ids []string) (map[string]string, error)
}

// PostalAddressQuery is the geographic part of a postal address to check.
type PostalAddressQuery struct {
	Country    string // ISO 3166 alpha-2
	PostalCode string
	Boundary   string // optional: the municipality (or smallest boundary) chosen by the user
}

// PostalAddressResult is the checked address: normalized code and its postal code entry.
type PostalAddressResult struct {
	PostalCode   string `json:"postalCode"`
	PostalCodeID string `json:"postalCodeId,omitempty"` // entry of the code in the boundary, when known
	Boundary     string `json:"boundary,omitempty"`
}

// AddressChecker validates the geographic part of postal addresses (the C#
// ContactMechanismApplicationService checked that the code belonged to the municipality).
// Errors match domain.ErrValidation.
type AddressChecker interface {
	CheckPostalAddress(ctx context.Context, q PostalAddressQuery) (PostalAddressResult, error)
}

// CountryInfo is the reference data other contexts use most.
type CountryInfo struct {
	Alpha2        string   `json:"alpha2"`
	Alpha3        string   `json:"alpha3"`
	Name          string   `json:"name"`
	Boundary      string   `json:"boundary"`
	CallingCode   string   `json:"callingCode,omitempty"`
	DefaultLocale string   `json:"defaultLocale,omitempty"`
	EU            bool     `json:"eu"`
	Eurozone      bool     `json:"eurozone"`
	SEPA          bool     `json:"sepa"`
	Currencies    []string `json:"currencies"` // ISO 4217, primary first
	Languages     []string `json:"languages"`  // ISO 639, default first
	TimeZones     []string `json:"timeZones"`  // IANA, primary first
}

// Reference answers country questions.
type Reference interface {
	Country(ctx context.Context, alpha2 string) (CountryInfo, error)
	// CheckIBAN validates an IBAN (check digits and the country's length and format).
	CheckIBAN(ctx context.Context, iban string) error
}
