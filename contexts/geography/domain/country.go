package domain

import (
	"regexp"
	"slices"
	"strings"
	"sync"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// CountryID identifies a country profile.
type CountryID struct{ fw.UUID }

// CountryCurrency is a currency used in a country.
type CountryCurrency struct {
	ID          fw.UUID
	Currency    fw.UUID
	Primary     bool
	LegalTender bool
	Order       int
}

// CountryLanguage is a language spoken in a country.
type CountryLanguage struct {
	ID       fw.UUID
	Language fw.UUID
	Official bool
	Default  bool
	Order    int
}

// CountryTimeZone is a time zone of a country.
type CountryTimeZone struct {
	ID       fw.UUID
	TimeZone fw.UUID
	Primary  bool
}

// CountryProfile is the reference data of a country (ISO 3166 codes, telephone and banking
// formats, postal code format, EU membership...), with its currencies, languages and time zones.
type CountryProfile struct {
	ID                  CountryID
	Boundary            BoundaryID
	Alpha2              vocab.CountryCode
	Alpha3              string
	Numeric             int
	CallingCode         string
	TrunkPrefix         string
	DefaultLocale       string
	EU, EEA, Eurozone   bool
	SEPA                bool
	IBANLength          int
	IBANPattern         string
	BBANFormat          string
	PostalCodePattern   string
	PostalCodeRequired  bool
	RequiresSubdivision bool
	Active              bool
	Currencies          []CountryCurrency
	Languages           []CountryLanguage
	TimeZones           []CountryTimeZone
}

// Country is the aggregate of a country profile.
type Country struct {
	fw.BaseAggregateRoot[CountryID]
	p CountryProfile
}

var patterns sync.Map // pattern -> *regexp.Regexp

func compiled(p string) (*regexp.Regexp, error) {
	if re, ok := patterns.Load(p); ok {
		return re.(*regexp.Regexp), nil
	}
	re, err := regexp.Compile(p)
	if err != nil {
		return nil, err
	}
	patterns.Store(p, re)
	return re, nil
}

// ReconstituteCountry rebuilds a country from stored data, compiling its formats.
func ReconstituteCountry(p CountryProfile) (*Country, error) {
	base, err := fw.NewBaseAggregateRoot(CountryKind, p.ID)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(!p.Alpha2.IsZero(), "alpha2", "required", "a country needs its ISO 3166 alpha-2 code")
	for field, pat := range map[string]string{"ibanPattern": p.IBANPattern, "postalCodePattern": p.PostalCodePattern} {
		if pat != "" {
			if _, err := compiled(pat); err != nil {
				v.Add(field, "format", err.Error())
			}
		}
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	p.Currencies, p.Languages, p.TimeZones = slices.Clone(p.Currencies), slices.Clone(p.Languages), slices.Clone(p.TimeZones)
	return &Country{BaseAggregateRoot: base, p: p}, nil
}

// Profile returns a copy of the profile.
func (c *Country) Profile() CountryProfile {
	p := c.p
	p.Currencies, p.Languages, p.TimeZones = slices.Clone(p.Currencies), slices.Clone(p.Languages), slices.Clone(p.TimeZones)
	return p
}

// Alpha2 returns the ISO 3166 alpha-2 code.
func (c *Country) Alpha2() string { return c.p.Alpha2.String() }

// Boundary returns the boundary of the country.
func (c *Country) Boundary() BoundaryID { return c.p.Boundary }

// CheckPostalCode validates a postal code with the country format (none: any non-empty code).
func (c *Country) CheckPostalCode(code string) (string, error) {
	code = NormalizePostalCode(code)
	var v fw.Validation
	if code == "" {
		v.Require(!c.p.PostalCodeRequired, "postalCode", "required", "a postal code is required in "+c.Alpha2())
		return "", v.Err()
	}
	if c.p.PostalCodePattern != "" {
		re, _ := compiled(c.p.PostalCodePattern)
		v.Require(re.MatchString(code), "postalCode", "format", "postal code does not have the format of "+c.Alpha2())
	}
	return code, v.Err()
}

// CheckIBAN validates an IBAN (ISO 13616 check digits) against the country: its country code,
// length and national format.
func (c *Country) CheckIBAN(i vocab.IBAN) error {
	var v fw.Validation
	v.Require(i.Country() == c.Alpha2(), "iban", "country", "the IBAN is not from "+c.Alpha2())
	v.Require(c.p.IBANLength == 0 || len(i.String()) == c.p.IBANLength, "iban", "length", "wrong IBAN length for "+c.Alpha2())
	if c.p.IBANPattern != "" {
		re, _ := compiled(c.p.IBANPattern)
		v.Require(re.MatchString(i.String()), "iban", "format", "the IBAN does not have the format of "+c.Alpha2())
	}
	return v.Err()
}

// Country fields.
var (
	FieldAlpha2          = spec.Text("alpha2", (*Country).Alpha2)
	FieldCountryBoundary = spec.Comparable("boundary", (*Country).Boundary)
)

// CountryAlpha2 matches a country by ISO code (case-insensitive input).
func CountryAlpha2(code string) spec.Spec[*Country] {
	return FieldAlpha2.Eq(strings.ToUpper(strings.TrimSpace(code)))
}
