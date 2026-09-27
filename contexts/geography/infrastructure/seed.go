// Package infrastructure stores the Geography and reference data context: SQL mappings, the
// versioned schema of the five engines, the embedded seed (the Karpo catalogs, same GUIDs,
// extracted from 040-Data/schema/sqlserver/02-semillas.sql) and the hot-swap factories.
package infrastructure

import (
	"compress/gzip"
	"embed"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"sync"

	"github.com/jhermoso/karpo-fw-go/contexts/geography/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

//go:embed seed/*.csv.gz
var seedFS embed.FS

// record is one CSV row addressed by column name.
type record map[string]string

func (r record) uuid(col string) fw.UUID {
	if r[col] == "" {
		return fw.UUID{}
	}
	return fw.MustParseUUID(r[col])
}
func (r record) bool(col string) bool { return r[col] == "1" }
func (r record) int(col string) int   { n, _ := strconv.Atoi(r[col]); return n }

// readSeed reads a seed file.
func readSeed(name string) ([]record, error) {
	f, err := seedFS.Open("seed/" + name + ".csv.gz")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	r := csv.NewReader(gz)
	header, err := r.Read()
	if err != nil {
		return nil, err
	}
	var out []record
	for {
		row, err := r.Read()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("seed %s: %w", name, err)
		}
		rec := make(record, len(header))
		for i, h := range header {
			rec[h] = row[i]
		}
		out = append(out, rec)
	}
}

// Seed is the decoded seed data.
type Seed struct {
	BoundaryTypes []domain.BoundaryType
	Boundaries    []*domain.Boundary
	PostalCodes   []*domain.PostalCode
	Countries     []*domain.Country
	Currencies    []domain.Currency
	Languages     []domain.Language
	TimeZones     []domain.TimeZone
	StreetTypes   []domain.StreetType
}

var (
	seedOnce sync.Once
	seedData *Seed
	seedErr  error
)

// LoadSeed decodes the embedded seed once.
func LoadSeed() (*Seed, error) {
	seedOnce.Do(func() { seedData, seedErr = loadSeed() })
	return seedData, seedErr
}

func loadSeed() (*Seed, error) {
	files := map[string][]record{}
	for _, name := range []string{"boundary_types", "boundaries", "associations", "postal_codes", "countries", "currencies",
		"languages", "time_zones", "street_types", "country_currencies", "country_languages", "country_time_zones"} {
		rs, err := readSeed(name)
		if err != nil {
			return nil, err
		}
		files[name] = rs
	}
	s := &Seed{}
	for _, r := range files["boundary_types"] {
		s.BoundaryTypes = append(s.BoundaryTypes, domain.BoundaryType{ID: domain.BoundaryTypeID{UUID: r.uuid("id")}, Name: r["name"],
			Description: r["description"], Active: r.bool("active")})
	}
	links := map[fw.UUID][]domain.Link{}
	for _, r := range files["associations"] {
		links[r.uuid("child")] = append(links[r.uuid("child")], domain.Link{ID: r.uuid("id"),
			Parent: domain.BoundaryID{UUID: r.uuid("parent")}, Kind: domain.LinkKind(r["kind"])})
	}
	for _, r := range files["boundaries"] {
		b, err := domain.ReconstituteBoundary(domain.BoundaryID{UUID: r.uuid("id")}, r["name"], domain.BoundaryTypeID{UUID: r.uuid("type")},
			domain.Codes{Geo: r["geo_code"], Abbreviation: r["abbreviation"], INE: r["ine_code"], NUTS: r["nuts_code"], LAU: r["lau_code"]},
			r.bool("active"), links[r.uuid("id")])
		if err != nil {
			return nil, fmt.Errorf("seed boundary %s: %w", r["id"], err)
		}
		s.Boundaries = append(s.Boundaries, b)
	}
	for _, r := range files["postal_codes"] {
		p, err := domain.ReconstitutePostalCode(domain.PostalCodeID{UUID: r.uuid("id")}, r["code"], domain.BoundaryID{UUID: r.uuid("boundary")})
		if err != nil {
			return nil, err
		}
		s.PostalCodes = append(s.PostalCodes, p)
	}
	for _, r := range files["currencies"] {
		s.Currencies = append(s.Currencies, domain.Currency{ID: r.uuid("id"), Code: r["code"], Name: r["name"], Symbol: r["symbol"],
			MinorUnits: r.int("minor_units"), Crypto: r.bool("crypto"), Active: r.bool("active")})
	}
	for _, r := range files["languages"] {
		s.Languages = append(s.Languages, domain.Language{ID: r.uuid("id"), Code: r["code"], Name: r["name"],
			NativeName: r["native_name"], Active: r.bool("active")})
	}
	for _, r := range files["time_zones"] {
		s.TimeZones = append(s.TimeZones, domain.TimeZone{ID: r.uuid("id"), Code: r["code"], Name: r["name"],
			UTCOffsetMinutes: r.int("utc_offset_minutes"), ObservesDST: r.bool("observes_dst"), Active: r.bool("active")})
	}
	for _, r := range files["street_types"] {
		c, err := vocab.NewCountryCode(r["country"])
		if err != nil {
			return nil, err
		}
		s.StreetTypes = append(s.StreetTypes, domain.StreetType{ID: r.uuid("id"), Country: c, Code: r["code"], Name: r["name"],
			Abbreviation: r["abbreviation"], Active: r.bool("active")})
	}
	byCountry := map[fw.UUID]*domain.CountryProfile{}
	var profiles []*domain.CountryProfile
	for _, r := range files["countries"] {
		alpha2, err := vocab.NewCountryCode(r["alpha2"])
		if err != nil {
			return nil, fmt.Errorf("seed country %s: %w", r["alpha2"], err)
		}
		p := &domain.CountryProfile{ID: domain.CountryID{UUID: r.uuid("id")}, Boundary: domain.BoundaryID{UUID: r.uuid("boundary")},
			Alpha2: alpha2, Alpha3: r["alpha3"], Numeric: r.int("numeric"), CallingCode: r["calling_code"], TrunkPrefix: r["trunk_prefix"],
			DefaultLocale: r["default_locale"], EU: r.bool("eu"), EEA: r.bool("eea"), Eurozone: r.bool("eurozone"), SEPA: r.bool("sepa"),
			IBANLength: r.int("iban_length"), IBANPattern: r["iban_regex"], BBANFormat: r["bban_format"],
			PostalCodePattern: r["postal_regex"], PostalCodeRequired: r.bool("postal_required"),
			RequiresSubdivision: r.bool("requires_subdivision"), Active: r.bool("active")}
		byCountry[p.Boundary.UUID] = p
		profiles = append(profiles, p)
	}
	for _, r := range files["country_currencies"] {
		if p := byCountry[r.uuid("country")]; p != nil {
			p.Currencies = append(p.Currencies, domain.CountryCurrency{ID: r.uuid("id"), Currency: r.uuid("currency"),
				Primary: r.bool("primary"), LegalTender: r.bool("legal_tender"), Order: r.int("display_order")})
		}
	}
	for _, r := range files["country_languages"] {
		if p := byCountry[r.uuid("country")]; p != nil {
			p.Languages = append(p.Languages, domain.CountryLanguage{ID: r.uuid("id"), Language: r.uuid("language"),
				Official: r.bool("official"), Default: r.bool("is_default"), Order: r.int("display_order")})
		}
	}
	for _, r := range files["country_time_zones"] {
		if p := byCountry[r.uuid("country")]; p != nil {
			p.TimeZones = append(p.TimeZones, domain.CountryTimeZone{ID: r.uuid("id"), TimeZone: r.uuid("time_zone"), Primary: r.bool("primary")})
		}
	}
	for _, p := range profiles {
		c, err := domain.ReconstituteCountry(*p)
		if err != nil {
			return nil, fmt.Errorf("seed country %s: %w", p.Alpha2, err)
		}
		s.Countries = append(s.Countries, c)
	}
	return s, nil
}
