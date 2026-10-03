package vocab

import (
	"slices"
	"strings"
)

// RegionCode is an ISO 3166-2 subdivision code ("ES-MD", "ES-CT", "PT-11"): the country code,
// a dash and 1 to 3 letters or digits. The country part must be a valid CountryCode.
type RegionCode struct{ code string }

// NewRegionCode validates an ISO 3166-2 code (case-insensitive).
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

// MustRegionCode is like NewRegionCode but panics on error.
func MustRegionCode(s string) RegionCode {
	r, err := NewRegionCode(s)
	if err != nil {
		panic(err)
	}
	return r
}

// Country returns the country of the region.
func (r RegionCode) Country() CountryCode {
	c, _, _ := strings.Cut(r.code, "-")
	return CountryCode{c}
}

// String returns the code.
func (r RegionCode) String() string { return r.code }

// IsZero reports whether the code is absent.
func (r RegionCode) IsZero() bool { return r.code == "" }

// MarshalText implements encoding.TextMarshaler.
func (r RegionCode) MarshalText() ([]byte, error) { return []byte(r.code), nil }

// UnmarshalText implements encoding.TextUnmarshaler.
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

// RegulationSource is the origin of a regulated rule (a law, a regulator's circular, an internal
// policy) and its territorial scope. It replaces the C# IRegulationSource + ICountryDelimited +
// IRegionDelimited interfaces with a single value.
type RegulationSource struct {
	Name           Name          `json:"name"`
	Description    Description   `json:"description,omitzero"`
	Countries      []CountryCode `json:"countries,omitempty"`
	Regions        []RegionCode  `json:"regions,omitempty"`
	InternalPolicy bool          `json:"internalPolicy,omitempty"`
	Justification  Description   `json:"justification,omitzero"`
}

// NewRegulationSource validates a source: it needs a name, and an internal policy must be
// justified. Regions must belong to the listed countries when countries are given.
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

// AppliesIn reports whether the source applies in a country (no countries = everywhere).
func (s RegulationSource) AppliesIn(c CountryCode) bool {
	return len(s.Countries) == 0 || slices.Contains(s.Countries, c)
}

// AppliesInRegion reports whether the source applies in a region: listed explicitly, or no
// regions listed and the region's country is covered.
func (s RegulationSource) AppliesInRegion(r RegionCode) bool {
	if len(s.Regions) > 0 {
		return slices.Contains(s.Regions, r)
	}
	return s.AppliesIn(r.Country())
}
