package infrastructure

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/jhermoso/karpo-fw-go/contexts/geography/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// Context is the name of the bounded context (migration history).
const Context = "geography"

var schemaDDL = []string{
	`CREATE TABLE geo_boundary_types (id {uuid} NOT NULL PRIMARY KEY, name {str:200} NOT NULL, description {str:500}, active {bool} NOT NULL)`,
	`CREATE TABLE geo_boundaries (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, name {str:200} NOT NULL,
	boundary_type {uuid} NOT NULL, geo_code {str:20}, abbreviation {str:20}, ine_code {str:20}, nuts_code {str:20}, lau_code {str:20},
	active {bool} NOT NULL, FOREIGN KEY (boundary_type) REFERENCES geo_boundary_types (id))`,
	`CREATE INDEX ix_geo_boundaries_name ON geo_boundaries (name)`,
	`CREATE INDEX ix_geo_boundaries_type ON geo_boundaries (boundary_type)`,
	`CREATE TABLE geo_boundary_links (boundary_id {uuid} NOT NULL, id {uuid} NOT NULL, parent {uuid} NOT NULL, kind {str:20} NOT NULL,
	PRIMARY KEY (boundary_id, id), FOREIGN KEY (boundary_id) REFERENCES geo_boundaries (id), FOREIGN KEY (parent) REFERENCES geo_boundaries (id))`,
	`CREATE INDEX ix_geo_boundary_links_parent ON geo_boundary_links (parent, kind)`,
	`CREATE TABLE geo_postal_codes (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, code {str:20} NOT NULL,
	boundary {uuid} NOT NULL, FOREIGN KEY (boundary) REFERENCES geo_boundaries (id))`,
	`CREATE UNIQUE INDEX ux_geo_postal_codes ON geo_postal_codes (code, boundary)`,
	`CREATE TABLE geo_countries (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, boundary {uuid} NOT NULL,
	alpha2 {str:2} NOT NULL, alpha3 {str:3}, numeric_code {int}, calling_code {str:10}, trunk_prefix {str:5}, default_locale {str:20},
	eu {bool} NOT NULL, eea {bool} NOT NULL, eurozone {bool} NOT NULL, sepa {bool} NOT NULL, iban_length {int}, iban_regex {str:200},
	bban_format {str:100}, postal_regex {str:200}, postal_required {bool} NOT NULL, requires_subdivision {bool} NOT NULL,
	active {bool} NOT NULL, FOREIGN KEY (boundary) REFERENCES geo_boundaries (id))`,
	`CREATE UNIQUE INDEX ux_geo_countries_alpha2 ON geo_countries (alpha2)`,
	`CREATE TABLE geo_currencies (id {uuid} NOT NULL PRIMARY KEY, code {str:10} NOT NULL, name {str:100} NOT NULL, symbol {str:10},
	minor_units {int} NOT NULL, crypto {bool} NOT NULL, active {bool} NOT NULL)`,
	`CREATE TABLE geo_languages (id {uuid} NOT NULL PRIMARY KEY, code {str:10} NOT NULL, name {str:100} NOT NULL, native_name {str:100},
	active {bool} NOT NULL)`,
	`CREATE TABLE geo_time_zones (id {uuid} NOT NULL PRIMARY KEY, code {str:64} NOT NULL, name {str:100} NOT NULL,
	utc_offset_minutes {int} NOT NULL, observes_dst {bool} NOT NULL, active {bool} NOT NULL)`,
	`CREATE TABLE geo_street_types (id {uuid} NOT NULL PRIMARY KEY, country {str:2} NOT NULL, code {str:10} NOT NULL, name {str:100} NOT NULL,
	abbreviation {str:20}, active {bool} NOT NULL)`,
	`CREATE TABLE geo_country_currencies (country_id {uuid} NOT NULL, id {uuid} NOT NULL, currency {uuid} NOT NULL, is_primary {bool} NOT NULL,
	legal_tender {bool} NOT NULL, display_order {int} NOT NULL, PRIMARY KEY (country_id, id),
	FOREIGN KEY (country_id) REFERENCES geo_countries (id), FOREIGN KEY (currency) REFERENCES geo_currencies (id))`,
	`CREATE TABLE geo_country_languages (country_id {uuid} NOT NULL, id {uuid} NOT NULL, language {uuid} NOT NULL, official {bool} NOT NULL,
	is_default {bool} NOT NULL, display_order {int} NOT NULL, PRIMARY KEY (country_id, id),
	FOREIGN KEY (country_id) REFERENCES geo_countries (id), FOREIGN KEY (language) REFERENCES geo_languages (id))`,
	`CREATE TABLE geo_country_time_zones (country_id {uuid} NOT NULL, id {uuid} NOT NULL, time_zone {uuid} NOT NULL, is_primary {bool} NOT NULL,
	PRIMARY KEY (country_id, id), FOREIGN KEY (country_id) REFERENCES geo_countries (id), FOREIGN KEY (time_zone) REFERENCES geo_time_zones (id))`,
}

// Tables of the context, children first (DropAll).
var tables = []string{"geo_country_time_zones", "geo_country_languages", "geo_country_currencies", "geo_countries", "geo_postal_codes",
	"geo_boundary_links", "geo_boundaries", "geo_boundary_types", "geo_street_types", "geo_time_zones", "geo_languages", "geo_currencies"}

// Migrations is the versioned schema of the context: the tables, then the Karpo seed.
func Migrations() sqlrepo.MigrationSet {
	return sqlrepo.MigrationSet{Context: Context, Migrations: []sqlrepo.Migration{
		{Version: 1, Name: "boundaries, postal codes and reference data", Up: sqlrepo.RenderDDLAll(schemaDDL...)},
		{Version: 2, Name: "Karpo geography and reference seed", Run: seedDatabase},
	}}
}

// Migrator returns the schema migrator of the context on db.
func Migrator(db *sqlrepo.DB) (*sqlrepo.Migrator, error) {
	return sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{Migrations()})
}

// DropAll removes every table of the context (tests only; the migration history is shared).
func DropAll(ctx context.Context, db *sqlrepo.DB) {
	for _, t := range tables {
		_, _ = db.ExecContext(ctx, "DROP TABLE "+t)
	}
}

func opt(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func optInt(n int) any {
	if n == 0 {
		return nil
	}
	return int64(n)
}

// seedDatabase loads the embedded seed with batched inserts (parents before children).
func seedDatabase(ctx context.Context, db *sqlrepo.DB) error {
	s, err := LoadSeed()
	if err != nil {
		return err
	}
	insert := func(table string, cols []string, rows [][]any) error {
		if err := db.InsertMany(ctx, table, cols, rows); err != nil {
			return fmt.Errorf("seeding %s: %w", table, err)
		}
		return nil
	}
	var rows [][]any
	for _, t := range s.BoundaryTypes {
		rows = append(rows, []any{t.ID, t.Name, opt(t.Description), t.Active})
	}
	if err := insert("geo_boundary_types", []string{"id", "name", "description", "active"}, rows); err != nil {
		return err
	}
	rows, links := nil, [][]any{}
	for _, b := range s.Boundaries {
		c := b.Codes()
		rows = append(rows, []any{b.ID(), int64(1), b.Name(), b.Type(), opt(c.Geo), opt(c.Abbreviation), opt(c.INE), opt(c.NUTS), opt(c.LAU), b.IsActive()})
		for _, l := range b.Links() {
			links = append(links, []any{b.ID(), l.ID, l.Parent, string(l.Kind)})
		}
	}
	if err := insert("geo_boundaries", []string{"id", "version", "name", "boundary_type", "geo_code", "abbreviation", "ine_code",
		"nuts_code", "lau_code", "active"}, rows); err != nil {
		return err
	}
	if err := insert("geo_boundary_links", []string{"boundary_id", "id", "parent", "kind"}, links); err != nil {
		return err
	}
	rows = nil
	for _, p := range s.PostalCodes {
		rows = append(rows, []any{p.ID(), int64(1), p.Code(), p.Boundary()})
	}
	if err := insert("geo_postal_codes", []string{"id", "version", "code", "boundary"}, rows); err != nil {
		return err
	}
	rows = nil
	for _, c := range s.Currencies {
		rows = append(rows, []any{c.ID, c.Code, c.Name, opt(c.Symbol), int64(c.MinorUnits), c.Crypto, c.Active})
	}
	if err := insert("geo_currencies", []string{"id", "code", "name", "symbol", "minor_units", "crypto", "active"}, rows); err != nil {
		return err
	}
	rows = nil
	for _, l := range s.Languages {
		rows = append(rows, []any{l.ID, l.Code, l.Name, opt(l.NativeName), l.Active})
	}
	if err := insert("geo_languages", []string{"id", "code", "name", "native_name", "active"}, rows); err != nil {
		return err
	}
	rows = nil
	for _, z := range s.TimeZones {
		rows = append(rows, []any{z.ID, z.Code, z.Name, int64(z.UTCOffsetMinutes), z.ObservesDST, z.Active})
	}
	if err := insert("geo_time_zones", []string{"id", "code", "name", "utc_offset_minutes", "observes_dst", "active"}, rows); err != nil {
		return err
	}
	rows = nil
	for _, st := range s.StreetTypes {
		rows = append(rows, []any{st.ID, st.Country.String(), st.Code, st.Name, opt(st.Abbreviation), st.Active})
	}
	if err := insert("geo_street_types", []string{"id", "country", "code", "name", "abbreviation", "active"}, rows); err != nil {
		return err
	}
	var currencies, languages, zones [][]any
	rows = nil
	for _, c := range s.Countries {
		p := c.Profile()
		rows = append(rows, []any{p.ID, int64(1), p.Boundary, p.Alpha2.String(), opt(p.Alpha3), optInt(p.Numeric), opt(p.CallingCode),
			opt(p.TrunkPrefix), opt(p.DefaultLocale), p.EU, p.EEA, p.Eurozone, p.SEPA, optInt(p.IBANLength), opt(p.IBANPattern),
			opt(p.BBANFormat), opt(p.PostalCodePattern), p.PostalCodeRequired, p.RequiresSubdivision, p.Active})
		for _, x := range p.Currencies {
			currencies = append(currencies, []any{p.ID, x.ID, x.Currency, x.Primary, x.LegalTender, int64(x.Order)})
		}
		for _, x := range p.Languages {
			languages = append(languages, []any{p.ID, x.ID, x.Language, x.Official, x.Default, int64(x.Order)})
		}
		for _, x := range p.TimeZones {
			zones = append(zones, []any{p.ID, x.ID, x.TimeZone, x.Primary})
		}
	}
	if err := insert("geo_countries", countryColumns, rows); err != nil {
		return err
	}
	if err := insert("geo_country_currencies", []string{"country_id", "id", "currency", "is_primary", "legal_tender", "display_order"}, currencies); err != nil {
		return err
	}
	if err := insert("geo_country_languages", []string{"country_id", "id", "language", "official", "is_default", "display_order"}, languages); err != nil {
		return err
	}
	return insert("geo_country_time_zones", []string{"country_id", "id", "time_zone", "is_primary"}, zones)
}

var countryColumns = []string{"id", "version", "boundary", "alpha2", "alpha3", "numeric_code", "calling_code", "trunk_prefix", "default_locale",
	"eu", "eea", "eurozone", "sepa", "iban_length", "iban_regex", "bban_format", "postal_regex", "postal_required", "requires_subdivision", "active"}

// ---------------------------------------------------------------------------------------------
// Mappings (read-only: the data changes through migrations)
// ---------------------------------------------------------------------------------------------

func readOnly(kind string) error {
	return fmt.Errorf("%w: %s is reference data, maintained by migrations", fw.ErrUnsupported, kind)
}

// BoundaryMapping maps Boundary to geo_boundaries + geo_boundary_links.
func BoundaryMapping() sqlrepo.Mapping[domain.BoundaryID, *domain.Boundary] {
	return sqlrepo.Mapping[domain.BoundaryID, *domain.Boundary]{
		Table:   "geo_boundaries",
		Columns: []string{"name", "boundary_type", "geo_code", "abbreviation", "ine_code", "nuts_code", "lau_code", "active"},
		Fields:  map[string]string{"type": "boundary_type"},
		Dehydrate: func(*domain.Boundary) (sqlrepo.Values, error) {
			return nil, readOnly(domain.BoundaryKind)
		},
		Hydrate: func(row *sqlrepo.Row, children sqlrepo.ChildRows) (*domain.Boundary, error) {
			var links []domain.Link
			for _, c := range children.Of("links") {
				links = append(links, domain.Link{ID: c.UUID("id"), Parent: domain.BoundaryID{UUID: c.UUID("parent")}, Kind: domain.LinkKind(c.String("kind"))})
			}
			if err := row.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteBoundary(domain.BoundaryID{UUID: row.UUID("id")}, row.String("name"),
				domain.BoundaryTypeID{UUID: row.UUID("boundary_type")}, domain.Codes{Geo: row.String("geo_code"),
					Abbreviation: row.String("abbreviation"), INE: row.String("ine_code"), NUTS: row.String("nuts_code"), LAU: row.String("lau_code")},
				row.Bool("active"), links)
		},
		Children: []sqlrepo.Child[*domain.Boundary]{{
			Name: "links", Table: "geo_boundary_links", ForeignKey: "boundary_id", Columns: []string{"id", "parent", "kind"},
			OrderBy:   []string{"kind", "id"},
			Dehydrate: func(*domain.Boundary) ([]sqlrepo.Values, error) { return nil, readOnly(domain.BoundaryKind) },
		}},
	}
}

// PostalCodeMapping maps PostalCode to geo_postal_codes.
func PostalCodeMapping() sqlrepo.Mapping[domain.PostalCodeID, *domain.PostalCode] {
	return sqlrepo.Mapping[domain.PostalCodeID, *domain.PostalCode]{
		Table:     "geo_postal_codes",
		Columns:   []string{"code", "boundary"},
		Dehydrate: func(*domain.PostalCode) (sqlrepo.Values, error) { return nil, readOnly(domain.PostalCodeKind) },
		Hydrate: func(row *sqlrepo.Row, _ sqlrepo.ChildRows) (*domain.PostalCode, error) {
			return domain.ReconstitutePostalCode(domain.PostalCodeID{UUID: row.UUID("id")}, row.String("code"),
				domain.BoundaryID{UUID: row.UUID("boundary")})
		},
	}
}

// CountryMapping maps Country to geo_countries and its three child tables.
func CountryMapping() sqlrepo.Mapping[domain.CountryID, *domain.Country] {
	child := func(name, table string, cols ...string) sqlrepo.Child[*domain.Country] {
		return sqlrepo.Child[*domain.Country]{Name: name, Table: table, ForeignKey: "country_id", Columns: append([]string{"id"}, cols...),
			OrderBy: []string{"id"}, Dehydrate: func(*domain.Country) ([]sqlrepo.Values, error) { return nil, readOnly(domain.CountryKind) }}
	}
	return sqlrepo.Mapping[domain.CountryID, *domain.Country]{
		Table:     "geo_countries",
		Columns:   countryColumns[2:],
		Dehydrate: func(*domain.Country) (sqlrepo.Values, error) { return nil, readOnly(domain.CountryKind) },
		Hydrate: func(r *sqlrepo.Row, children sqlrepo.ChildRows) (*domain.Country, error) {
			alpha2, err := vocab.NewCountryCode(r.String("alpha2"))
			if err != nil {
				return nil, err
			}
			p := domain.CountryProfile{ID: domain.CountryID{UUID: r.UUID("id")}, Boundary: domain.BoundaryID{UUID: r.UUID("boundary")},
				Alpha2: alpha2, Alpha3: r.String("alpha3"), Numeric: int(r.Int64("numeric_code")), CallingCode: r.String("calling_code"),
				TrunkPrefix: r.String("trunk_prefix"), DefaultLocale: r.String("default_locale"), EU: r.Bool("eu"), EEA: r.Bool("eea"),
				Eurozone: r.Bool("eurozone"), SEPA: r.Bool("sepa"), IBANLength: int(r.Int64("iban_length")), IBANPattern: r.String("iban_regex"),
				BBANFormat: r.String("bban_format"), PostalCodePattern: r.String("postal_regex"), PostalCodeRequired: r.Bool("postal_required"),
				RequiresSubdivision: r.Bool("requires_subdivision"), Active: r.Bool("active")}
			for _, c := range children.Of("currencies") {
				p.Currencies = append(p.Currencies, domain.CountryCurrency{ID: c.UUID("id"), Currency: c.UUID("currency"),
					Primary: c.Bool("is_primary"), LegalTender: c.Bool("legal_tender"), Order: int(c.Int64("display_order"))})
			}
			for _, c := range children.Of("languages") {
				p.Languages = append(p.Languages, domain.CountryLanguage{ID: c.UUID("id"), Language: c.UUID("language"),
					Official: c.Bool("official"), Default: c.Bool("is_default"), Order: int(c.Int64("display_order"))})
			}
			for _, c := range children.Of("time_zones") {
				p.TimeZones = append(p.TimeZones, domain.CountryTimeZone{ID: c.UUID("id"), TimeZone: c.UUID("time_zone"), Primary: c.Bool("is_primary")})
			}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteCountry(p)
		},
		Children: []sqlrepo.Child[*domain.Country]{
			child("currencies", "geo_country_currencies", "currency", "is_primary", "legal_tender", "display_order"),
			child("languages", "geo_country_languages", "language", "official", "is_default", "display_order"),
			child("time_zones", "geo_country_time_zones", "time_zone", "is_primary"),
		},
	}
}

// ---------------------------------------------------------------------------------------------
// Catalogs
// ---------------------------------------------------------------------------------------------

// SQLCatalogs reads the reference catalogs from the database.
type SQLCatalogs struct{ db *sqlrepo.DB }

var _ domain.Catalogs = SQLCatalogs{}

// BoundaryTypes implements domain.Catalogs.
func (c SQLCatalogs) BoundaryTypes(ctx context.Context) ([]domain.BoundaryType, error) {
	rows, err := c.db.Select(ctx, "geo_boundary_types", []string{"id", "name", "description", "active"}, "name")
	return mapRows(rows, err, func(r *sqlrepo.Row) domain.BoundaryType {
		return domain.BoundaryType{ID: domain.BoundaryTypeID{UUID: r.UUID("id")}, Name: r.String("name"), Description: r.String("description"), Active: r.Bool("active")}
	})
}

// Currencies implements domain.Catalogs.
func (c SQLCatalogs) Currencies(ctx context.Context) ([]domain.Currency, error) {
	rows, err := c.db.Select(ctx, "geo_currencies", []string{"id", "code", "name", "symbol", "minor_units", "crypto", "active"}, "code")
	return mapRows(rows, err, func(r *sqlrepo.Row) domain.Currency {
		return domain.Currency{ID: r.UUID("id"), Code: r.String("code"), Name: r.String("name"), Symbol: r.String("symbol"),
			MinorUnits: int(r.Int64("minor_units")), Crypto: r.Bool("crypto"), Active: r.Bool("active")}
	})
}

// Languages implements domain.Catalogs.
func (c SQLCatalogs) Languages(ctx context.Context) ([]domain.Language, error) {
	rows, err := c.db.Select(ctx, "geo_languages", []string{"id", "code", "name", "native_name", "active"}, "code")
	return mapRows(rows, err, func(r *sqlrepo.Row) domain.Language {
		return domain.Language{ID: r.UUID("id"), Code: r.String("code"), Name: r.String("name"), NativeName: r.String("native_name"), Active: r.Bool("active")}
	})
}

// TimeZones implements domain.Catalogs.
func (c SQLCatalogs) TimeZones(ctx context.Context) ([]domain.TimeZone, error) {
	rows, err := c.db.Select(ctx, "geo_time_zones", []string{"id", "code", "name", "utc_offset_minutes", "observes_dst", "active"}, "code")
	return mapRows(rows, err, func(r *sqlrepo.Row) domain.TimeZone {
		return domain.TimeZone{ID: r.UUID("id"), Code: r.String("code"), Name: r.String("name"),
			UTCOffsetMinutes: int(r.Int64("utc_offset_minutes")), ObservesDST: r.Bool("observes_dst"), Active: r.Bool("active")}
	})
}

// StreetTypes implements domain.Catalogs.
func (c SQLCatalogs) StreetTypes(ctx context.Context) ([]domain.StreetType, error) {
	rows, err := c.db.Select(ctx, "geo_street_types", []string{"id", "country", "code", "name", "abbreviation", "active"}, "country", "code")
	return mapRows(rows, err, func(r *sqlrepo.Row) domain.StreetType {
		country, _ := vocab.NewCountryCode(r.String("country"))
		return domain.StreetType{ID: r.UUID("id"), Country: country, Code: r.String("code"), Name: r.String("name"),
			Abbreviation: r.String("abbreviation"), Active: r.Bool("active")}
	})
}

func mapRows[T any](rows []*sqlrepo.Row, err error, fn func(*sqlrepo.Row) T) ([]T, error) {
	if err != nil {
		return nil, err
	}
	out := make([]T, 0, len(rows))
	for _, r := range rows {
		v := fn(r)
		if err := r.Err(); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// SeedCatalogs serves the catalogs of the embedded seed (in-memory backend).
type SeedCatalogs struct{}

func seed() *Seed {
	s, err := LoadSeed()
	if err != nil {
		panic(err) // the embedded seed is validated by the tests
	}
	return s
}

// BoundaryTypes implements domain.Catalogs.
func (SeedCatalogs) BoundaryTypes(context.Context) ([]domain.BoundaryType, error) {
	return slices.Clone(seed().BoundaryTypes), nil
}

// Currencies implements domain.Catalogs.
func (SeedCatalogs) Currencies(context.Context) ([]domain.Currency, error) {
	out := slices.Clone(seed().Currencies)
	slices.SortFunc(out, func(a, b domain.Currency) int { return strings.Compare(a.Code, b.Code) })
	return out, nil
}

// Languages implements domain.Catalogs.
func (SeedCatalogs) Languages(context.Context) ([]domain.Language, error) {
	return slices.Clone(seed().Languages), nil
}

// TimeZones implements domain.Catalogs.
func (SeedCatalogs) TimeZones(context.Context) ([]domain.TimeZone, error) {
	return slices.Clone(seed().TimeZones), nil
}

// StreetTypes implements domain.Catalogs.
func (SeedCatalogs) StreetTypes(context.Context) ([]domain.StreetType, error) {
	return slices.Clone(seed().StreetTypes), nil
}

// ---------------------------------------------------------------------------------------------
// Hot-swap factories
// ---------------------------------------------------------------------------------------------

func unsupported(b hotswap.Backend) error { return fmt.Errorf("geography: unsupported backend %T", b) }

// LoadMemory fills an in-memory store with the seed (the SQL backends get it from migration 2).
func LoadMemory(ctx context.Context, store *memory.Store) error {
	s, err := LoadSeed()
	if err != nil {
		return err
	}
	boundaries := memoryRepos.boundaries(store)
	postal := memoryRepos.postal(store)
	countries := memoryRepos.countries(store)
	// The decoded seed is shared by every store of the process, and saving marks the saved
	// instance as persisted: each store saves its own copies, so the seed can be loaded again.
	return store.Do(ctx, func(ctx context.Context) error {
		for _, b := range s.Boundaries {
			c := *b
			if err := boundaries.Save(ctx, &c); err != nil {
				return err
			}
		}
		for _, p := range s.PostalCodes {
			c := *p
			if err := postal.Save(ctx, &c); err != nil {
				return err
			}
		}
		for _, country := range s.Countries {
			c := *country
			if err := countries.Save(ctx, &c); err != nil {
				return err
			}
		}
		return nil
	})
}

// memoryRepos keeps one repository per store, so the loaded seed and the queries share it.
var memoryRepos = struct {
	boundaries func(*memory.Store) *memory.Repository[domain.BoundaryID, *domain.Boundary]
	postal     func(*memory.Store) *memory.Repository[domain.PostalCodeID, *domain.PostalCode]
	countries  func(*memory.Store) *memory.Repository[domain.CountryID, *domain.Country]
}{
	boundaries: perStore(func(s *memory.Store) *memory.Repository[domain.BoundaryID, *domain.Boundary] {
		return memory.NewRepository[domain.BoundaryID, *domain.Boundary](s)
	}),
	postal: perStore(func(s *memory.Store) *memory.Repository[domain.PostalCodeID, *domain.PostalCode] {
		return memory.NewRepository[domain.PostalCodeID, *domain.PostalCode](s)
	}),
	countries: perStore(func(s *memory.Store) *memory.Repository[domain.CountryID, *domain.Country] {
		return memory.NewRepository[domain.CountryID, *domain.Country](s)
	}),
}

// BoundaryRepositoryFactory builds the boundary repository.
func BoundaryRepositoryFactory(b hotswap.Backend) (domain.BoundaryRepository, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewRepository(db, BoundaryMapping())
	case *memory.Store:
		return memoryRepos.boundaries(db), nil
	}
	return nil, unsupported(b)
}

// PostalCodeRepositoryFactory builds the postal code repository.
func PostalCodeRepositoryFactory(b hotswap.Backend) (domain.PostalCodeRepository, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewRepository(db, PostalCodeMapping())
	case *memory.Store:
		return memoryRepos.postal(db), nil
	}
	return nil, unsupported(b)
}

// CountryRepositoryFactory builds the country repository.
func CountryRepositoryFactory(b hotswap.Backend) (domain.CountryRepository, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewRepository(db, CountryMapping())
	case *memory.Store:
		return memoryRepos.countries(db), nil
	}
	return nil, unsupported(b)
}

// CatalogsFor returns the catalog reader of a backend.
func CatalogsFor(b hotswap.Backend) (domain.Catalogs, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return SQLCatalogs{db: db}, nil
	case *memory.Store:
		return SeedCatalogs{}, nil
	}
	return nil, unsupported(b)
}

// perStore memoizes one value per in-memory store (memory repositories keep their rows).
func perStore[T any](build func(*memory.Store) T) func(*memory.Store) T {
	var mu sync.Mutex
	cache := map[*memory.Store]T{}
	return func(s *memory.Store) T {
		mu.Lock()
		defer mu.Unlock()
		if v, ok := cache[s]; ok {
			return v
		}
		v := build(s)
		cache[s] = v
		return v
	}
}
