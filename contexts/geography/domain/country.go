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

// CountryID identifica un perfil de país.
type CountryID struct{ fw.UUID }

// CountryCurrency es una moneda que se usa en un país.
type CountryCurrency struct {
	ID          fw.UUID
	Currency    fw.UUID
	Primary     bool
	LegalTender bool
	Order       int
}

// CountryLanguage es un idioma que se habla en un país.
type CountryLanguage struct {
	ID       fw.UUID
	Language fw.UUID
	Official bool
	Default  bool
	Order    int
}

// CountryTimeZone es un huso horario de un país.
type CountryTimeZone struct {
	ID       fw.UUID
	TimeZone fw.UUID
	Primary  bool
}

// CountryProfile son los datos de referencia de un país (códigos ISO 3166, formatos telefónicos y
// bancarios, formato del código postal, pertenencia a la UE...), con sus monedas, idiomas y husos
// horarios.
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

// Country es el agregado de un perfil de país.
type Country struct {
	fw.BaseAggregateRoot[CountryID]
	p CountryProfile
}

var patterns sync.Map // patrón -> *regexp.Regexp

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

// ReconstituteCountry reconstruye un país a partir de los datos guardados, compilando sus
// formatos.
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

// Profile devuelve una copia del perfil.
func (c *Country) Profile() CountryProfile {
	p := c.p
	p.Currencies, p.Languages, p.TimeZones = slices.Clone(p.Currencies), slices.Clone(p.Languages), slices.Clone(p.TimeZones)
	return p
}

// Alpha2 devuelve el código ISO 3166 alfa-2.
func (c *Country) Alpha2() string { return c.p.Alpha2.String() }

// Boundary devuelve el límite del país.
func (c *Country) Boundary() BoundaryID { return c.p.Boundary }

// CheckPostalCode valida un código postal con el formato del país (si no tiene: cualquier código
// no vacío).
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

// CheckIBAN valida un IBAN (dígitos de control ISO 13616) contra el país: su código de país, su
// longitud y su formato nacional.
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

// Campos de los países.
var (
	FieldAlpha2          = spec.Text("alpha2", (*Country).Alpha2)
	FieldCountryBoundary = spec.Comparable("boundary", (*Country).Boundary)
)

// CountryAlpha2 selecciona un país por su código ISO (la entrada no distingue mayúsculas).
func CountryAlpha2(code string) spec.Spec[*Country] {
	return FieldAlpha2.Eq(strings.ToUpper(strings.TrimSpace(code)))
}
