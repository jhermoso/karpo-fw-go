package vocab

import (
	"slices"
	"strings"
)

// RegionCode es un código de subdivisión ISO 3166-2 ("ES-MD", "ES-CT", "PT-11"): el código de país,
// un guion y de 1 a 3 letras o dígitos. La parte del país debe ser un CountryCode válido.
type RegionCode struct{ code string }

// NewRegionCode valida un código ISO 3166-2 (sin distinguir mayúsculas).
func NewRegionCode(s string) (RegionCode, error) {
	c := strings.ToUpper(strings.TrimSpace(s))
	country, sub, ok := strings.Cut(c, "-")
	if !ok || len(sub) < 1 || len(sub) > 3 || !alnum(sub) {
		return RegionCode{}, invalid("region", "iso3166_2", "invalid ISO 3166-2 region "+quote(s))
	}
	if _, err := NewCountryCode(country); err != nil {
		return RegionCode{}, invalid("region", "iso3166_2", "invalid country in region "+quote(s))
	}
	return RegionCode{c}, nil
}

// MustRegionCode es como NewRegionCode pero entra en pánico si hay error.
func MustRegionCode(s string) RegionCode {
	r, err := NewRegionCode(s)
	if err != nil {
		panic(err)
	}
	return r
}

// Country devuelve el país de la región.
func (r RegionCode) Country() CountryCode {
	c, _, _ := strings.Cut(r.code, "-")
	return CountryCode{c}
}

// String devuelve el código.
func (r RegionCode) String() string { return r.code }

// IsZero dice si falta el código.
func (r RegionCode) IsZero() bool { return r.code == "" }

// MarshalText implementa encoding.TextMarshaler.
func (r RegionCode) MarshalText() ([]byte, error) { return []byte(r.code), nil }

// UnmarshalText implementa encoding.TextUnmarshaler.
func (r *RegionCode) UnmarshalText(b []byte) error {
	if len(b) == 0 {
		*r = RegionCode{}
		return nil
	}
	v, err := NewRegionCode(string(b))
	if err == nil {
		*r = v
	}
	return err
}

// RegulationSource es el origen de una regla regulada (una ley, una circular de un regulador, una
// política interna) y su ámbito territorial. Sustituye las interfaces IRegulationSource +
// ICountryDelimited + IRegionDelimited de C# por un único valor.
type RegulationSource struct {
	Name           Name          `json:"name"`
	Description    Description   `json:"description,omitzero"`
	Countries      []CountryCode `json:"countries,omitempty"`
	Regions        []RegionCode  `json:"regions,omitempty"`
	InternalPolicy bool          `json:"internalPolicy,omitempty"`
	Justification  Description   `json:"justification,omitzero"`
}

// NewRegulationSource valida un origen: necesita un nombre, y una política interna debe estar
// justificada. Si se dan países, las regiones deben pertenecer a los países indicados.
func NewRegulationSource(name Name, countries []CountryCode, regions []RegionCode, internalPolicy bool, justification Description) (RegulationSource, error) {
	if name.IsZero() {
		return RegulationSource{}, invalid("name", "required", "regulation source name is required")
	}
	if internalPolicy && justification.IsZero() {
		return RegulationSource{}, invalid("justification", "required", "an internal policy must be justified")
	}
	for _, r := range regions {
		if len(countries) > 0 && !slices.Contains(countries, r.Country()) {
			return RegulationSource{}, invalid("regions", "scope", "region "+r.String()+" is outside the listed countries")
		}
	}
	return RegulationSource{
		Name: name, Countries: slices.Clone(countries), Regions: slices.Clone(regions),
		InternalPolicy: internalPolicy, Justification: justification,
	}, nil
}

// AppliesIn dice si el origen se aplica en un país (sin países = en todas partes).
func (s RegulationSource) AppliesIn(c CountryCode) bool {
	return len(s.Countries) == 0 || slices.Contains(s.Countries, c)
}

// AppliesInRegion dice si el origen se aplica en una región: porque figura expresamente o porque
// no hay regiones indicadas y el país de la región está cubierto.
func (s RegulationSource) AppliesInRegion(r RegionCode) bool {
	if len(s.Regions) > 0 {
		return slices.Contains(s.Regions, r)
	}
	return s.AppliesIn(r.Country())
}
